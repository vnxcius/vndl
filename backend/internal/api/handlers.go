package api

import (
	"bytes"
	"context"
	"encoding/json"
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
}

func NewServer(cfg config.Config, mgr *jobs.Manager, rl *middleware.RateLimiter, cl *middleware.ConcurrencyLimiter, probes *downloader.ProbeCache) *Server {
	return &Server{cfg: cfg, mgr: mgr, rl: rl, cl: cl, probes: probes}
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
	mux.Handle("GET /api/downloads/{id}/events", s.rl.Middleware(http.HandlerFunc(s.events)))
	// Not wrapped in s.cl.Middleware — that would hold a concurrency slot for
	// the whole response, including streaming to a slow client, long after
	// yt-dlp has exited. file() acquires/releases the slot itself instead.
	mux.Handle("GET /api/downloads/{id}/file", s.rl.Middleware(http.HandlerFunc(s.file)))
	mux.HandleFunc("POST /api/downloads/{id}/cancel", s.cancel)
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
			if fxMeta, fxErr := downloader.ProbeFx(r.Context(), cleanURL); fxErr == nil {
				meta, err = fxMeta, nil
			} else {
				ev.Set("fallback_error", fxErr.Error())
			}
		}
		if err != nil {
			ev.Set("status", "error").Set("error", err.Error())
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
	Ext       string `json:"ext"`
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
	ext := req.Ext
	container := req.Container
	if req.AudioOnly {
		ext = "mp3"
		container = ""
	} else {
		if container == "" {
			container = "mkv"
		}
		if ext == "" {
			ext = container
		}
	}

	job := s.mgr.Create(cleanURL, req.FormatID, req.AudioOnly, title, ext, container)
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

	ch, cancel, ok := job.Subscribe()
	if !ok {
		writeError(w, http.StatusTooManyRequests, "too many active connections for this download")
		return
	}
	defer cancel()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	if status, _ := job.Status(); status == jobs.StatusCompleted || status == jobs.StatusError || status == jobs.StatusCanceled {
		writeSSE(w, downloader.ProgressEvent{Status: string(status)})
		flusher.Flush()
		return
	}

	for {
		select {
		case <-r.Context().Done():
			return
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
		writeError(w, http.StatusNotFound, "job not found")
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
	defer s.mgr.ExpireAfter(job.ID, s.mgr.TTL())

	if job.IsCanceled() {
		ev.Set("status", "canceled")
		writeError(w, statusClientClosedRequest, "download was canceled")
		return
	}

	// Deleted before the handler returns — nothing here is ever persisted.
	scratch, err := os.MkdirTemp("", "vndl-job-*")
	if err != nil {
		ev.Set("status", "error").Set("error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to start download")
		return
	}
	defer os.RemoveAll(scratch)

	source, formatID := job.URL, job.FormatID
	if downloader.IsFxFormat(formatID) && downloader.IsTwitterURL(source) {
		ev.Set("fallback", "fxtwitter")
		direct, err := downloader.ResolveFxURL(r.Context(), source, formatID)
		if err != nil {
			job.Fail(err.Error())
			ev.Set("status", "error").Set("error", err.Error())
			writeError(w, http.StatusUnprocessableEntity, "could not resolve this tweet's video — try fetching it again")
			return
		}
		source, formatID = direct, ""
	}

	// Released right after cmd.Wait() below, not deferred past file
	// streaming — a slow client shouldn't pin a concurrency slot.
	releaseSlot, err := s.cl.Acquire(r.Context())
	if err != nil {
		ev.Set("status", "error").Set("error", "at capacity")
		writeError(w, http.StatusServiceUnavailable, "server is at capacity, try again shortly")
		return
	}
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(releaseSlot) }
	defer release()

	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	job.SetCancelFunc(cancel)

	cmd := downloader.BuildDownloadCmd(ctx, s.cfg, source, formatID, job.AudioOnly, job.Container, scratch)

	// Captured so a failure carries more detail than a bare "exit status 1".
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		ev.Set("status", "error").Set("error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to start download")
		return
	}

	if err := cmd.Start(); err != nil {
		job.Fail("failed to start yt-dlp")
		ev.Set("status", "error").Set("error", err.Error())
		writeError(w, http.StatusBadGateway, "failed to start download")
		return
	}

	job.SetStatus(jobs.StatusDownloading)
	job.Publish(downloader.ProgressEvent{Status: "downloading"})

	// Must drain stdout to EOF before Wait(), per exec.Cmd's own docs —
	// otherwise Wait() can close the pipe before streamProgress reads it.
	stdoutDone := make(chan struct{})
	go func() {
		streamProgress(job, stdout)
		close(stdoutDone)
	}()
	<-stdoutDone

	waitErr := cmd.Wait()
	release()

	if waitErr != nil {
		if job.IsCanceled() {
			ev.Set("status", "canceled")
			writeError(w, statusClientClosedRequest, "download was canceled")
			return
		}
		detail := strings.TrimSpace(stderrBuf.String())
		fullErr := waitErr.Error()
		if detail != "" {
			fullErr += ": " + detail
		}
		job.Fail(fullErr)
		ev.Set("status", "error").Set("error", fullErr)
		writeError(w, http.StatusUnprocessableEntity, downloader.FriendlyError(detail)) // see probe()
		return
	}

	matches, _ := filepath.Glob(filepath.Join(scratch, "output.*"))
	if len(matches) != 1 {
		job.Fail("unexpected output from yt-dlp")
		ev.Set("status", "error").Set("error", fmt.Sprintf("expected 1 output file, found %d", len(matches)))
		writeError(w, http.StatusInternalServerError, "download finished but produced no file")
		return
	}
	resultPath := matches[0]
	ext := strings.TrimPrefix(filepath.Ext(resultPath), ".")

	f, err := os.Open(resultPath)
	if err != nil {
		job.Fail(err.Error())
		ev.Set("status", "error").Set("error", err.Error())
		writeError(w, http.StatusInternalServerError, "failed to read finished download")
		return
	}
	defer f.Close()

	info, statErr := f.Stat()

	job.SetStatus(jobs.StatusCompleted)
	job.Publish(downloader.ProgressEvent{Status: "done"})

	w.Header().Set("Content-Type", downloader.ContentType(ext))
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.%s"`, job.Title, ext))
	if statErr == nil {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", info.Size()))
	}
	w.WriteHeader(http.StatusOK)

	written, copyErr := io.Copy(w, f)
	ev.Set("bytes_streamed", written).Set("status", "ok")
	if copyErr != nil {
		ev.Set("client_disconnected", true)
	}
}

const progressLogInterval = 5 * time.Second // plus always on phase change

func streamProgress(job *jobs.Job, r io.Reader) {
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
			return
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
