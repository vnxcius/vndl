package api

import (
	"bytes"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"vndl/internal/config"
	"vndl/internal/downloader"
	"vndl/internal/jobs"
	"vndl/internal/middleware"
)

// fakeYtDlp writes a shell script standing in for yt-dlp.
func fakeYtDlp(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "yt-dlp")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func serverWith(cfg config.Config) (*Server, http.Handler) {
	if cfg.JobTTL == 0 {
		cfg.JobTTL = time.Minute
	}
	s := NewServer(cfg, jobs.NewManager(cfg), middleware.NewRateLimiter(1000, 1000),
		middleware.NewConcurrencyLimiter(6, time.Second), downloader.NewProbeCache(0))
	return s, s.Routes()
}

// Audit #2 VNDL-001/007: a job past MAX_JOB_DURATION is stopped, and so is
// every process yt-dlp spawned.
func TestFileStopsJobAndChildrenAtMaxDuration(t *testing.T) {
	pidFile := filepath.Join(t.TempDir(), "child.pid")
	bin := fakeYtDlp(t, "sleep 30 &\necho $! > "+pidFile+"\nwait")
	s, h := serverWith(config.Config{YtDlpPath: bin, MaxJobDuration: 300 * time.Millisecond})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "", false, "t", "mkv", "mkv")

	start := time.Now()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/downloads/"+job.ID+"/file", nil))

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("/file took %v, want it stopped near the 300ms limit", elapsed)
	}
	if w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "too long") {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
	raw, err := os.ReadFile(pidFile)
	if err != nil {
		t.Fatal(err)
	}
	pid, _ := strconv.Atoi(strings.TrimSpace(string(raw)))
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(pid, 0); err == nil {
		_ = syscall.Kill(pid, syscall.SIGKILL)
		t.Error("yt-dlp's child process survived the job being stopped")
	}
}

// Audit #2 VNDL-005: yt-dlp's stderr carries the content ID; it must not
// reach the logs.
func TestFileErrorLogOmitsContentID(t *testing.T) {
	bin := fakeYtDlp(t, "echo 'ERROR: [youtube] ABCDEFGHIJK: Video unavailable' >&2\nexit 1")
	s, h := serverWith(config.Config{YtDlpPath: bin})
	job := s.mgr.Create("https://www.youtube.com/watch?v=ABCDEFGHIJK", "", false, "t", "mkv", "mkv")

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
	defer slog.SetDefault(prev)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/downloads/"+job.ID+"/file", nil))

	if !strings.Contains(logs.String(), "Video unavailable") {
		t.Fatalf("expected the error to be logged:\n%s", logs.String())
	}
	if strings.Contains(logs.String(), "ABCDEFGHIJK") {
		t.Errorf("logs leak the content ID:\n%s", logs.String())
	}
	if !strings.Contains(w.Body.String(), "unavailable") {
		t.Errorf("client should still get the friendly message, got %s", w.Body.String())
	}
}

// Audit #2 VNDL-006: an SSE stream for a job that never starts must end.
func TestEventsClosesForJobThatNeverStarts(t *testing.T) {
	s, h := serverWith(config.Config{JobTTL: 200 * time.Millisecond})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "", false, "t", "mkv", "mkv")
	srv := httptest.NewServer(h)
	defer srv.Close()

	done := make(chan struct{})
	go func() {
		defer close(done)
		resp, err := http.Get(srv.URL + "/api/downloads/" + job.ID + "/events")
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("SSE stream for a never-started job stayed open")
	}
}

func TestEventsRejectsOverServerWideCap(t *testing.T) {
	s, h := serverWith(config.Config{MaxSSEConnections: 1})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "", false, "t", "mkv", "mkv")
	s.sse <- struct{}{} // the one slot is taken

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/downloads/"+job.ID+"/events", nil))
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want 503", w.Code)
	}
}
