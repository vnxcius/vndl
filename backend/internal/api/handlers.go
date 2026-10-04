package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"vndl/internal/config"
	"vndl/internal/downloader"
	"vndl/internal/jobs"
	"vndl/internal/logging"
	"vndl/internal/middleware"
)

const statusClientClosedRequest = 499 // mirrors nginx's 499; no standard status exists for this

const maxRequestBody = 16 << 10 // caps json.Decoder's buffering of the request body

type Server struct {
	cfg    config.Config
	mgr    *jobs.Manager
	rl     *middleware.RateLimiter
	cl     *middleware.ConcurrencyLimiter
	probes *downloader.ProbeCache

	perIP *middleware.InFlightLimiter // concurrent /file per client
	sse   chan struct{}               // server-wide SSE connection slots; nil = unlimited
}

func NewServer(cfg config.Config, mgr *jobs.Manager, rl *middleware.RateLimiter, cl *middleware.ConcurrencyLimiter, probes *downloader.ProbeCache) *Server {
	s := &Server{cfg: cfg, mgr: mgr, rl: rl, cl: cl, probes: probes,
		perIP: middleware.NewInFlightLimiter(cfg.MaxJobsPerIP)}
	if cfg.MaxSSEConnections > 0 {
		s.sse = make(chan struct{}, cfg.MaxSSEConnections)
	}
	return s
}

// expensive applies both the per-IP rate limiter and the server-wide
// concurrency cap to a handler that spawns yt-dlp.
func (s *Server) expensive(h http.HandlerFunc) http.Handler {
	return s.rl.Middleware(s.cl.Middleware(h))
}

func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.Handle("POST /api/probe", s.expensive(s.probe))
	mux.Handle("POST /api/downloads", s.rl.Middleware(http.HandlerFunc(s.createDownload)))
	mux.Handle("GET /api/downloads/{id}", s.rl.Middleware(http.HandlerFunc(s.status)))
	mux.Handle("GET /api/downloads/{id}/events", s.rl.Middleware(http.HandlerFunc(s.events)))
	mux.Handle("GET /api/downloads/{id}/file", s.rl.Middleware(http.HandlerFunc(s.file)))
	mux.Handle("POST /api/downloads/{id}/cancel", s.rl.Middleware(http.HandlerFunc(s.cancel)))
	return mux
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type probeRequest struct {
	URL string `json:"url"`
}

func (s *Server) probe(w http.ResponseWriter, r *http.Request) {
	ev := logging.New().
		Set("endpoint", "probe").
		Set("client_ip", middleware.ClientIP(r))
	defer ev.Emit(r.Context(), slog.Default())

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req probeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ev.Set("status", "error").Set("error", "invalid request body")
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	// Privacy: only the host is ever logged, never the url or title.
	ev.Set("platform", hostOf(req.URL))

	if !downloader.IsAllowedURL(req.URL) {
		ev.Set("status", "error").Set("error", "unsupported url")
		writeError(w, http.StatusUnprocessableEntity, "unsupported url — only YouTube, Twitter/X, TikTok and Instagram links are supported")
		return
	}
	cleanURL := downloader.StripFragment(req.URL)

	meta, cached := s.probes.Get(cleanURL)
	if cached {
		ev.Set("cache", "hit")
	} else {
		ev.Set("cache", "miss")
		var err error
		meta, err = downloader.Probe(r.Context(), s.cfg, cleanURL)
		if downloader.NeedsFxFallback(cleanURL, err) {
			ev.Set("fallback", "fxtwitter")
			if fxMeta, fxErr := downloader.ProbeFx(r.Context(), cleanURL, slog.With("endpoint", "probe")); fxErr == nil {
				meta, err = fxMeta, nil
			} else {
				ev.Set("fallback_error", fxErr.Error())
			}
		}
		if err != nil {
			ev.Set("status", "error").Set("error", downloader.ScrubError(err.Error()))
			// 422, not 5xx: Cloudflare replaces 5xx bodies with its own error page.
			writeError(w, http.StatusUnprocessableEntity, downloader.FriendlyError(err.Error()))
			return
		}
		s.probes.Set(cleanURL, meta)
	}

	ev.Set("status", "ok").
		Set("duration_s", meta.Duration).
		Set("formats_available", len(meta.Formats))
	writeJSON(w, http.StatusOK, meta)
}

type createDownloadRequest struct {
	URL       string `json:"url"`
	FormatID  string `json:"format_id"`
	AudioOnly bool   `json:"audio_only"`
	Title     string `json:"title"`
	Container string `json:"container"`
}

func (s *Server) createDownload(w http.ResponseWriter, r *http.Request) {
	ev := logging.New().
		Set("endpoint", "create_download").
		Set("client_ip", middleware.ClientIP(r))
	defer ev.Emit(r.Context(), slog.Default())

	r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
	var req createDownloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		ev.Set("status", "error").Set("error", "invalid request body")
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	ev.Set("platform", hostOf(req.URL)).
		Set("format_id", req.FormatID).
		Set("audio_only", req.AudioOnly)

	if !downloader.IsAllowedURL(req.URL) {
		ev.Set("status", "error").Set("error", "unsupported url")
		writeError(w, http.StatusUnprocessableEntity, "unsupported url")
		return
	}
	cleanURL := downloader.StripFragment(req.URL)
	if req.FormatID != "" && !validFormatID.MatchString(req.FormatID) {
		ev.Set("status", "error").Set("error", "invalid format_id")
		writeError(w, http.StatusBadRequest, "invalid format_id")
		return
	}
	if !req.AudioOnly && !validContainer[req.Container] {
		ev.Set("status", "error").Set("error", "invalid container")
		writeError(w, http.StatusBadRequest, "invalid container")
		return
	}

	title := sanitizeFilename(req.Title)
	if title == "" {
		title = "download"
	}
	// The client's "ext" is ignored — /file names the result after the file
	// yt-dlp actually wrote.
	ext, container := "mp3", ""
	if !req.AudioOnly {
		container = req.Container
		if container == "" {
			container = "mkv"
		}
		ext = container
	}

	releaseIP, ok := s.perIP.Acquire(middleware.ClientKey(r))
	if !ok {
		ev.Set("status", "error").Set("error", "per-ip job limit")
		writeError(w, http.StatusTooManyRequests, "too many downloads in progress from your network — wait for one to finish")
		return
	}
	releaseSlot, err := s.cl.Acquire(r.Context())
	if err != nil {
		releaseIP()
		ev.Set("status", "error").Set("error", "at capacity")
		writeError(w, http.StatusServiceUnavailable, "server is at capacity, try again shortly")
		return
	}

	job := s.mgr.Create(cleanURL, req.FormatID, req.AudioOnly, title, ext, container)
	s.mgr.Hold(job.ID)
	go s.run(job, releaseIP, releaseSlot)

	ev.Set("status", "ok").Set("job_id", job.ID)
	writeJSON(w, http.StatusCreated, map[string]string{
		"job_id":   job.ID,
		"filename": fmt.Sprintf("%s.%s", title, ext),
	})
}

func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	job, ok := s.mgr.Get(id)
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}

	if s.sse != nil {
		select {
		case s.sse <- struct{}{}:
			defer func() { <-s.sse }()
		default:
			writeError(w, http.StatusServiceUnavailable, "server is at capacity, try again shortly")
			return
		}
	}

	ch, cancel, ok := job.Subscribe()
	if !ok {
		writeError(w, http.StatusTooManyRequests, "too many active connections for this download")
		return
	}
	defer cancel()

	// A job whose /file never starts publishes nothing; don't hold the
	// connection past its TTL, nor any stream past the longest possible job.
	pendingTimeout := time.After(s.mgr.TTL())
	var hardDeadline <-chan time.Time
	if s.cfg.MaxJobDuration > 0 {
		hardDeadline = time.After(s.cfg.MaxJobDuration + s.mgr.TTL())
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if status, _ := job.Status(); status == jobs.StatusCompleted || status == jobs.StatusError || status == jobs.StatusCanceled {
		writeSSE(w, job.Event())
		flusher.Flush()
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case <-hardDeadline:
			return
		case <-pendingTimeout:
			if status, _ := job.Status(); status == jobs.StatusPending {
				return
			}
			pendingTimeout = nil
		case ev, open := <-ch:
			if !open {
				return
			}
			writeSSE(w, ev)
			flusher.Flush()
			if ev.Status == "done" || ev.Status == "error" || ev.Status == "canceled" {
				return
			}
		}
	}
}

// status lets a client whose progress stream dropped (e.g. a backgrounded
// mobile tab) catch up on a job that ended meanwhile.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	job, ok := s.mgr.Get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job.Event())
}

func writeSSE(w http.ResponseWriter, ev downloader.ProgressEvent) {
	b, _ := json.Marshal(ev)
	fmt.Fprintf(w, "data: %s\n\n", b)
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	ev := logging.New().
		Set("endpoint", "cancel").
		Set("client_ip", middleware.ClientIP(r))
	defer ev.Emit(r.Context(), slog.Default())

	id := r.PathValue("id")
	ev.Set("job_id", id)

	job, ok := s.mgr.Get(id)
	if !ok {
		ev.Set("status", "error").Set("error", "job not found")
		writeError(w, http.StatusNotFound, "job not found")
		return
	}

	job.Cancel()
	ev.Set("status", "ok")
	writeJSON(w, http.StatusOK, map[string]string{"status": "canceled"})
}

// run produces job's file in the background, detached from any request, so
// a client that backgrounds or closes its tab doesn't stop the download.
// It owns both releases.
func (s *Server) run(job *jobs.Job, releaseIP, releaseSlot func()) {
	ev := logging.New().
		Set("endpoint", "download").
		Set("job_id", job.ID).
		Set("platform", hostOf(job.URL)).
		Set("format_id", job.FormatID).
		Set("audio_only", job.AudioOnly)
	defer ev.Emit(context.Background(), slog.Default())
	defer releaseIP()

	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(releaseSlot) }
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	if s.cfg.MaxJobDuration > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), s.cfg.MaxJobDuration)
	}
	defer cancel()
	job.SetCancelFunc(cancel)

	completed := false
	defer func() {
		ttl := s.mgr.TTL()
		if completed {
			ttl = s.cfg.FileTTL
		}
		s.mgr.ExpireAfter(job.ID, ttl)
	}()

	if job.IsCanceled() {
		ev.Set("status", "canceled")
		return
	}

	// Kept past run only if the job completes; the job deletes it on expiry.
	scratch, err := os.MkdirTemp("", "vndl-job-*")
	if err != nil {
		job.Fail("failed to start download")
		ev.Set("status", "error").Set("error", err.Error())
		return
	}
	defer func() {
		if !completed {
			os.RemoveAll(scratch)
		}
	}()

	source, formatID, direct := job.URL, job.FormatID, false
	if downloader.IsFxFormat(formatID) && downloader.IsTwitterURL(source) {
		ev.Set("fallback", "fxtwitter")
		mediaURL, err := downloader.ResolveFxURL(ctx, source, formatID, slog.With("endpoint", "download", "job_id", job.ID))
		if err != nil {
			job.Fail("could not resolve this tweet's video — try fetching it again")
			ev.Set("status", "error").Set("error", err.Error())
			return
		}
		source, formatID, direct = mediaURL, "", true
	}

	cmd := downloader.BuildDownloadCmd(ctx, s.cfg, source, formatID, job.AudioOnly, direct, job.Container, scratch)

	// Captured so a failure carries more detail than a bare "exit status 1".
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		job.Fail("failed to start download")
		ev.Set("status", "error").Set("error", err.Error())
		return
	}

	if err := cmd.Start(); err != nil {
		if job.IsCanceled() {
			ev.Set("status", "canceled")
			return
		}
		job.Fail("failed to start download")
		ev.Set("status", "error").Set("error", err.Error())
		return
	}

	job.SetStatus(jobs.StatusDownloading)
	job.Publish(downloader.ProgressEvent{Status: "downloading"})

	// Must drain stdout to EOF before Wait(), per exec.Cmd's own docs —
	// otherwise Wait() can close the pipe before streamProgress reads it.
	tooLarge := streamProgress(job, stdout)

	waitErr := cmd.Wait()
	release()

	if waitErr != nil {
		if job.IsCanceled() {
			ev.Set("status", "canceled")
			return
		}
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			job.Fail("this download took too long and was stopped — try a lower quality")
			ev.Set("status", "error").Set("error", "job exceeded max duration")
			return
		}
		detail := strings.TrimSpace(stderrBuf.String())
		fullErr := waitErr.Error()
		if detail != "" {
			fullErr += ": " + detail
		}
		job.Fail(downloader.FriendlyError(detail))
		ev.Set("status", "error").Set("error", downloader.ScrubError(fullErr))
		return
	}

	matches, _ := filepath.Glob(filepath.Join(scratch, "output.*"))
	if len(matches) == 0 && tooLarge {
		job.Fail("this file is larger than the server allows (" + s.cfg.MaxFilesize + ") — try a lower quality")
		ev.Set("status", "error").Set("error", "exceeds max filesize")
		return
	}
	if len(matches) != 1 {
		job.Fail("download finished but produced no file")
		ev.Set("status", "error").Set("error", fmt.Sprintf("expected 1 output file, found %d", len(matches)))
		return
	}
	resultPath := matches[0]
	if info, err := os.Stat(resultPath); err == nil {
		ev.Set("bytes", info.Size())
	}

	if !job.Complete(scratch, resultPath, strings.TrimPrefix(filepath.Ext(resultPath), ".")) {
		ev.Set("status", "canceled")
		return
	}
	completed = true
	ev.Set("status", "ok")
}

// file serves a finished job's result. Range requests are supported, so the
// browser's own download manager can resume an interrupted transfer, and it
// can be fetched more than once until the job expires.
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
	ev.Set("platform", hostOf(job.URL))

	path, ext, ok := job.Result()
	if !ok {
		switch status, msg := job.Status(); status {
		case jobs.StatusError:
			ev.Set("status", "error").Set("error", "job failed")
			writeError(w, http.StatusUnprocessableEntity, msg)
		case jobs.StatusCanceled:
			ev.Set("status", "canceled")
			writeError(w, statusClientClosedRequest, "download was canceled")
		default:
			ev.Set("status", "error").Set("error", "not ready")
			writeError(w, http.StatusConflict, "this download isn't ready yet")
		}
		return
	}

	// An expiry can delete the file between Result and here; an already open
	// file stays readable after that, so a transfer in progress is unaffected.
	f, err := os.Open(path)
	if err != nil {
		ev.Set("status", "error").Set("error", "file gone")
		writeError(w, http.StatusNotFound, "this download has expired — start it again")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		ev.Set("status", "error").Set("error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to read finished download")
		return
	}

	w.Header().Set("Content-Type", downloader.ContentType(ext))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, job.Title, ext))
	http.ServeContent(w, r, "", info.ModTime(), f)
	ev.Set("status", "ok").Set("range", r.Header.Get("Range") != "")
}

const progressLogInterval = 5 * time.Second // plus always on phase change

// streamProgress reports whether yt-dlp skipped the download for exceeding
// --max-filesize.
func streamProgress(job *jobs.Job, r io.Reader) (tooLarge bool) {
	buf := make([]byte, 4096)
	var partial strings.Builder
	var smoother downloader.ETASmoother
	var lastLogged time.Time
	var lastStatus string
	var processingStarted time.Time

	logProgress := func(ev downloader.ProgressEvent) {
		now := time.Now()
		if ev.Status == lastStatus && now.Sub(lastLogged) < progressLogInterval {
			return
		}
		lastLogged = now
		lastStatus = ev.Status

		fields := []any{"job_id", job.ID, "platform", hostOf(job.URL), "phase", ev.Status}
		switch ev.Status {
		case "downloading":
			fields = append(fields, "percent", ev.Percent, "eta", ev.ETA, "speed", ev.Speed)
		case "processing":
			if processingStarted.IsZero() {
				processingStarted = now
			}
			fields = append(fields, "elapsed_s", int(now.Sub(processingStarted).Seconds()))
		}
		slog.Info("download.progress", fields...)
	}

	for {
		n, err := r.Read(buf)
		if n > 0 {
			chunk := string(buf[:n])
			for _, c := range chunk {
				if c == '\r' || c == '\n' {
					line := partial.String()
					partial.Reset()
					if line == "" {
						continue
					}
					if downloader.IsMaxFilesizeLine(line) {
						tooLarge = true
					}
					if ev, ok := downloader.ParseProgressLine(line); ok {
						if ev.Status == "downloading" {
							ev.ETA = smoother.Smooth(ev.ETA)
						}
						job.Publish(ev)
						logProgress(ev)
					}
				} else {
					partial.WriteRune(c)
				}
			}
		}
		if err != nil {
			return tooLarge
		}
	}
}

func hostOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// Allows yt-dlp's format-selector syntax (ids, /, +, [], (), comparisons)
// and nothing else.
var validFormatID = regexp.MustCompile(`^[a-zA-Z0-9_./+\[\]()<>=*-]{1,64}$`)

var validContainer = map[string]bool{"": true, "mp4": true, "mkv": true, "webm": true}

var unsafeFilename = regexp.MustCompile(`[^a-zA-Z0-9 _.-]`)

func sanitizeFilename(name string) string {
	name = strings.TrimSpace(name)
	name = unsafeFilename.ReplaceAllString(name, "")
	if len(name) > 80 {
		name = name[:80]
	}
	return strings.TrimSpace(name)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
