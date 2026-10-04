package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"vndl/internal/downloader"
	"vndl/internal/jobs"
	"vndl/internal/logging"
	"vndl/internal/middleware"
)

// file streams the download to the browser while it's being produced, so
// the browser's own download manager owns it from the first click: it keeps
// going with the tab in the background or closed, and bytes flow within
// seconds, well inside a proxy's idle timeout. Nothing touches disk except
// an mp3's source audio. Headers wait for the first byte, so a failure
// before then is a real error status (a failed download, never an error
// body saved as the file); a failure after aborts the connection, which
// the browser also reports as failed.
func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	ev := logging.New().
		Set("endpoint", "file").
		Set("client_ip", middleware.ClientIP(r))
	defer ev.Emit(r.Context(), slog.Default())

	id := r.PathValue("id")
	ev.Set("job_id", id)

	job, ok := s.mgr.Get(id)
	if !ok {
		ev.Set("status", "error").Set("error", "job not found")
		writeError(w, http.StatusNotFound, "this download has expired — start it again")
		return
	}
	ev.Set("platform", hostOf(job.URL)).
		Set("format_id", job.FormatID).
		Set("audio_only", job.AudioOnly)

	if !job.Claim() {
		ev.Set("status", "error").Set("error", "already streaming")
		writeError(w, http.StatusConflict, "this download has already been started")
		return
	}
	defer job.Unclaim()
	// A stream can outlast JOB_TTL; the job must stay reachable meanwhile.
	s.mgr.Hold(job.ID)
	defer s.mgr.ExpireAfter(job.ID, s.mgr.TTL())

	// Failed rather than just rejected, so an open page shows why.
	reject := func(code int, msg, reason string) {
		job.Fail(msg)
		ev.Set("status", "error").Set("error", reason)
		writeError(w, code, msg)
	}

	releaseIP, ok := s.perIP.Acquire(middleware.ClientKey(r))
	if !ok {
		reject(http.StatusTooManyRequests, "too many downloads in progress from your network — wait for one to finish", "per-ip job limit")
		return
	}
	defer releaseIP()

	// Held for the whole stream: yt-dlp and ffmpeg run until the last byte.
	releaseSlot, err := s.cl.Acquire(r.Context())
	if err != nil {
		reject(http.StatusServiceUnavailable, "server is at capacity, try again shortly", "at capacity")
		return
	}
	defer releaseSlot()

	ctx, cancel := context.WithCancel(r.Context())
	if s.cfg.MaxJobDuration > 0 {
		ctx, cancel = context.WithTimeout(r.Context(), s.cfg.MaxJobDuration)
	}
	defer cancel()
	job.SetCancelFunc(cancel)
	if job.IsCanceled() {
		ev.Set("status", "canceled")
		writeError(w, statusClientClosedRequest, "download was canceled")
		return
	}

	source, format, direct := job.URL, job.FormatID, false
	if downloader.IsFxFormat(format) && downloader.IsTwitterURL(source) {
		ev.Set("fallback", "fxtwitter")
		mediaURL, err := downloader.ResolveFxURL(ctx, source, format, slog.With("endpoint", "file", "job_id", job.ID))
		if err != nil {
			reject(http.StatusUnprocessableEntity, "could not resolve this tweet's video — try fetching it again", err.Error())
			return
		}
		source, format, direct = mediaURL, "best", true
	}
	if format == "" {
		format = "bestvideo+bestaudio/best"
	}

	ext := job.Ext
	if job.AudioOnly {
		ext = "mp3"
	}
	out := &streamOut{w: w, rc: http.NewResponseController(w), limit: maxOutput(s.cfg.MaxFilesize), header: func(h http.Header) {
		h.Set("Content-Type", downloader.ContentType(ext))
		h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, job.Title, ext))
		h.Set("Cache-Control", "no-store")
	}}

	job.SetStatus(jobs.StatusDownloading)
	job.Publish(downloader.ProgressEvent{Status: "downloading"})

	video, audio, merge := downloader.SplitMergeFormat(format)
	switch {
	case job.AudioOnly:
		err = s.streamMP3(ctx, job, source, direct, out)
	case merge && !direct:
		ev.Set("merge", true)
		err = s.streamMerged(ctx, job, source, video, audio, out)
	default:
		err = s.streamSingle(ctx, job, source, format, direct, out)
	}
	if err == nil && out.written == 0 {
		err = &streamErr{msg: "download finished but produced no file", detail: "no output"}
	}
	ev.Set("bytes_streamed", out.written)

	if err == nil {
		job.Done()
		ev.Set("status", "ok")
		return
	}

	var se *streamErr
	switch {
	case job.IsCanceled():
		ev.Set("status", "canceled")
		if !out.started {
			writeError(w, statusClientClosedRequest, "download was canceled")
			return
		}
	case r.Context().Err() != nil:
		// The browser went away, e.g. the download was canceled from its
		// download manager. A retry from there can claim the job again.
		job.Fail("the download was interrupted — start it again")
		ev.Set("status", "error").Set("error", "client disconnected")
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		reject(http.StatusUnprocessableEntity, "this download took too long and was stopped — try a lower quality", "job exceeded max duration")
	case errors.Is(err, errOverLimit):
		reject(http.StatusUnprocessableEntity, tooLargeMsg(s.cfg.MaxFilesize), "exceeds max filesize")
	case errors.As(err, &se):
		reject(http.StatusUnprocessableEntity, se.msg, se.detail) // 422, not 5xx: see probe()
	default:
		reject(http.StatusInternalServerError, "the download failed — try again", err.Error())
	}
	if out.started {
		// Skips the final chunk, so the browser marks the download failed
		// instead of keeping a truncated file. reject's error body above went
		// nowhere: the response was already under way.
		panic(http.ErrAbortHandler)
	}
}

// streamSingle pipes one format straight from yt-dlp.
func (s *Server) streamSingle(ctx context.Context, job *jobs.Job, source, format string, direct bool, out *streamOut) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	log := newYtDlpLog(job)
	cmd := downloader.BuildStreamCmd(ctx, s.cfg, source, format, direct)
	cmd.Stderr = log
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return &streamErr{msg: "failed to start download", detail: err.Error()}
	}

	_, copyErr := io.Copy(out, stdout)
	if copyErr != nil {
		cancel()
	}
	waitErr := cmd.Wait()

	switch {
	case copyErr != nil:
		return copyErr
	case waitErr != nil:
		return log.failure(waitErr)
	case log.tooLarge && out.written == 0:
		return &streamErr{msg: tooLargeMsg(s.cfg.MaxFilesize), detail: "exceeds max filesize"}
	}
	return nil
}

// streamMerged pipes the video and audio streams from two yt-dlp processes
// into ffmpeg, which interleaves them into one container as they arrive.
func (s *Server) streamMerged(ctx context.Context, job *jobs.Job, source, video, audio string, out *streamOut) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	vr, vw, err := os.Pipe()
	if err != nil {
		return err
	}
	ar, aw, err := os.Pipe()
	if err != nil {
		vr.Close()
		vw.Close()
		return err
	}
	// The children hold their own copies once started; ours must close so
	// ffmpeg sees EOF when yt-dlp finishes.
	pipes := []*os.File{vr, vw, ar, aw}
	closePipes := sync.OnceFunc(func() {
		for _, f := range pipes {
			f.Close()
		}
	})
	defer closePipes()

	vlog, alog := newYtDlpLog(job), newYtDlpLog(nil)
	vcmd := downloader.BuildStreamCmd(ctx, s.cfg, source, video, false)
	vcmd.Stdout, vcmd.Stderr = vw, vlog
	acmd := downloader.BuildStreamCmd(ctx, s.cfg, source, audio, false)
	acmd.Stdout, acmd.Stderr = aw, alog

	var muxStderr bytes.Buffer
	mux := downloader.BuildMuxCmd(ctx, s.cfg, job.Container)
	mux.ExtraFiles = []*os.File{vr, ar}
	mux.Stderr = &muxStderr
	stdout, err := mux.StdoutPipe()
	if err != nil {
		return err
	}

	var started []*exec.Cmd
	for _, c := range []*exec.Cmd{mux, vcmd, acmd} {
		if err := c.Start(); err != nil {
			cancel()
			for _, c := range started {
				_ = c.Wait()
			}
			return &streamErr{msg: "failed to start download", detail: err.Error()}
		}
		started = append(started, c)
	}
	closePipes()

	_, copyErr := io.Copy(out, stdout)
	muxErr := mux.Wait()
	if copyErr != nil || muxErr != nil {
		cancel() // unblocks yt-dlp if ffmpeg stopped reading
	}
	vErr, aErr := vcmd.Wait(), acmd.Wait()

	// ffmpeg treats a yt-dlp that died as a stream that ended, and may
	// still exit cleanly with a truncated file, so yt-dlp's result wins.
	switch {
	case copyErr != nil:
		return copyErr
	case vErr != nil:
		return vlog.failure(vErr)
	case aErr != nil:
		return alog.failure(aErr)
	case vlog.tooLarge || alog.tooLarge:
		return &streamErr{msg: tooLargeMsg(s.cfg.MaxFilesize), detail: "exceeds max filesize"}
	case muxErr != nil:
		return &streamErr{
			msg:    "couldn't combine this video's video and audio — try another format",
			detail: downloader.ScrubError(fmt.Sprintf("ffmpeg: %v: %s", muxErr, strings.TrimSpace(muxStderr.String()))),
		}
	}
	return nil
}

// streamMP3 fetches the source audio first — some sources can't be decoded
// from a pipe, and audio is small enough to arrive quickly — then streams
// the mp3 as ffmpeg encodes it.
func (s *Server) streamMP3(ctx context.Context, job *jobs.Job, source string, direct bool, out *streamOut) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	scratch, err := os.MkdirTemp("", "vndl-job-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(scratch)

	log := newYtDlpLog(job)
	fetch := downloader.BuildDownloadCmd(ctx, s.cfg, source, "bestaudio/best", direct, scratch)
	fetch.Stdout, fetch.Stderr = log, log
	if err := fetch.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return log.failure(err)
	}
	matches, _ := filepath.Glob(filepath.Join(scratch, "input.*"))
	if len(matches) != 1 {
		if log.tooLarge {
			return &streamErr{msg: tooLargeMsg(s.cfg.MaxFilesize), detail: "exceeds max filesize"}
		}
		return &streamErr{msg: "download finished but produced no file", detail: fmt.Sprintf("expected 1 input file, found %d", len(matches))}
	}

	job.SetStatus(jobs.StatusProcessing)
	job.Publish(downloader.ProgressEvent{Status: "processing"})

	var stderr bytes.Buffer
	conv := downloader.BuildMP3Cmd(ctx, s.cfg, matches[0])
	conv.Stderr = &stderr
	stdout, err := conv.StdoutPipe()
	if err != nil {
		return err
	}
	if err := conv.Start(); err != nil {
		return &streamErr{msg: "failed to convert to mp3", detail: err.Error()}
	}
	_, copyErr := io.Copy(out, stdout)
	if copyErr != nil {
		cancel()
	}
	waitErr := conv.Wait()
	switch {
	case copyErr != nil:
		return copyErr
	case waitErr != nil:
		return &streamErr{msg: "failed to convert to mp3", detail: downloader.ScrubError(fmt.Sprintf("ffmpeg: %v: %s", waitErr, strings.TrimSpace(stderr.String())))}
	}
	return nil
}

// streamErr is a failure with a message fit for the user; detail is logged.
type streamErr struct{ msg, detail string }

func (e *streamErr) Error() string { return e.detail }

var errOverLimit = errors.New("output exceeds size limit")

func tooLargeMsg(max string) string {
	return "this file is larger than the server allows (" + max + ") — try a lower quality"
}

// maxOutput caps the stream. yt-dlp's --max-filesize checks each source
// stream on its own and only when their size is known up front, so this
// backstops it with headroom for a merge of two streams near the limit.
func maxOutput(maxFilesize string) int64 {
	return downloader.ParseSize(maxFilesize) * 3 / 2
}

// streamOut sends response headers on the first write, and flushes every
// write so bytes keep flowing through the proxies in front.
type streamOut struct {
	w       http.ResponseWriter
	rc      *http.ResponseController
	header  func(http.Header)
	limit   int64 // 0 = none
	started bool
	written int64
}

func (o *streamOut) Write(p []byte) (int, error) {
	if o.limit > 0 && o.written+int64(len(p)) > o.limit {
		return 0, errOverLimit
	}
	if !o.started {
		o.header(o.w.Header())
		o.w.WriteHeader(http.StatusOK)
		o.started = true
	}
	n, err := o.w.Write(p)
	o.written += int64(n)
	if err == nil {
		err = o.rc.Flush()
	}
	return n, err
}

const (
	progressLogInterval = 5 * time.Second // plus always on phase change
	maxLogDetail        = 16 << 10
	maxLineLen          = 64 << 10
)

// ytDlpLog consumes yt-dlp's console output line by line: progress goes to
// the job's subscribers (if job is set), anything else is kept, bounded,
// to explain a failure.
type ytDlpLog struct {
	job      *jobs.Job
	partial  []byte
	other    bytes.Buffer
	tooLarge bool // yt-dlp skipped the download for exceeding --max-filesize

	smoother          downloader.ETASmoother
	lastLogged        time.Time
	lastStatus        string
	processingStarted time.Time
}

func newYtDlpLog(job *jobs.Job) *ytDlpLog { return &ytDlpLog{job: job} }

func (l *ytDlpLog) Write(p []byte) (int, error) {
	for _, c := range p {
		if c == '\r' || c == '\n' {
			l.line(string(l.partial))
			l.partial = l.partial[:0]
		} else if len(l.partial) < maxLineLen {
			l.partial = append(l.partial, c)
		}
	}
	return len(p), nil
}

func (l *ytDlpLog) line(line string) {
	if line == "" {
		return
	}
	if downloader.IsMaxFilesizeLine(line) {
		l.tooLarge = true
	}
	if ev, ok := downloader.ParseProgressLine(line); ok {
		if l.job != nil {
			if ev.Status == "downloading" {
				ev.ETA = l.smoother.Smooth(ev.ETA)
			}
			l.job.Publish(ev)
			l.logProgress(ev)
		}
		return
	}
	// "[extractor] ..." lines are routine chatter, which yt-dlp writing to
	// stdout moves onto stderr; ERROR/WARNING lines are what explain a failure.
	if !strings.HasPrefix(line, "[") && l.other.Len() < maxLogDetail {
		l.other.WriteString(line)
		l.other.WriteByte('\n')
	}
}

func (l *ytDlpLog) logProgress(ev downloader.ProgressEvent) {
	now := time.Now()
	if ev.Status == l.lastStatus && now.Sub(l.lastLogged) < progressLogInterval {
		return
	}
	l.lastLogged = now
	l.lastStatus = ev.Status

	fields := []any{"job_id", l.job.ID, "platform", hostOf(l.job.URL), "phase", ev.Status}
	switch ev.Status {
	case "downloading":
		fields = append(fields, "percent", ev.Percent, "eta", ev.ETA, "speed", ev.Speed)
	case "processing":
		if l.processingStarted.IsZero() {
			l.processingStarted = now
		}
		fields = append(fields, "elapsed_s", int(now.Sub(l.processingStarted).Seconds()))
	}
	slog.Info("download.progress", fields...)
}

// failure turns yt-dlp's exit into a user-facing message, logging the
// scrubbed detail.
func (l *ytDlpLog) failure(waitErr error) *streamErr {
	detail := strings.TrimSpace(l.other.String())
	full := waitErr.Error()
	if detail != "" {
		full += ": " + detail
	}
	return &streamErr{msg: downloader.FriendlyError(detail), detail: downloader.ScrubError(full)}
}
