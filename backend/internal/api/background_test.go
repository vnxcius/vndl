package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"vndl/internal/config"
	"vndl/internal/downloader"
	"vndl/internal/jobs"
)

// writesOutput is a fake yt-dlp body that writes "hello world" as its mp4 output.
const writesOutput = `while [ $# -gt 0 ]; do [ "$1" = "-o" ] && out="$2"; shift; done
printf 'hello world' > "$(dirname "$out")/output.mp4"`

func completedJob(t *testing.T, cfg config.Config) (*Server, http.Handler, *jobs.Job) {
	t.Helper()
	cfg.YtDlpPath = fakeYtDlp(t, writesOutput)
	s, h := serverWith(cfg)
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "", false, "clip", "mp4", "mp4")
	s.run(job, noop, noop)
	if status, msg := job.Status(); status != jobs.StatusCompleted {
		t.Fatalf("job ended %s %q, want completed", status, msg)
	}
	return s, h, job
}

func TestFileServesCompletedJobRepeatedlyWithRanges(t *testing.T) {
	_, h, job := completedJob(t, config.Config{FileTTL: time.Minute})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/downloads/"+job.ID+"/file", nil))
	if w.Code != http.StatusOK || w.Body.String() != "hello world" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="clip.mp4"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}

	// A resumed transfer from the browser's download manager.
	req := httptest.NewRequest("GET", "/api/downloads/"+job.ID+"/file", nil)
	req.Header.Set("Range", "bytes=6-")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusPartialContent || w.Body.String() != "world" {
		t.Errorf("range request got %d %q", w.Code, w.Body.String())
	}
}

func TestFileRejectsJobNotYetReady(t *testing.T) {
	s, h := serverWith(config.Config{})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "", false, "t", "mp4", "mp4")

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/downloads/"+job.ID+"/file", nil))
	if w.Code != http.StatusConflict {
		t.Errorf("got %d, want 409", w.Code)
	}
}

func TestCompletedJobFileDeletedOnExpiry(t *testing.T) {
	s, h, job := completedJob(t, config.Config{FileTTL: 100 * time.Millisecond})
	path, _, _ := job.Result()

	time.Sleep(300 * time.Millisecond)

	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("finished file should be deleted once the job expires, stat err = %v", err)
	}
	if _, ok := s.mgr.Get(job.ID); ok {
		t.Error("job should be gone after FILE_TTL")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/downloads/"+job.ID+"/file", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want 404", w.Code)
	}
}

// A job must outlive JOB_TTL while it is still running.
func TestRunningJobNotExpiredByJobTTL(t *testing.T) {
	bin := fakeYtDlp(t, "sleep 0.4\n"+writesOutput)
	s, _ := serverWith(config.Config{YtDlpPath: bin, JobTTL: 100 * time.Millisecond, FileTTL: time.Minute})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "", false, "t", "mp4", "mp4")
	s.mgr.Hold(job.ID)
	s.run(job, noop, noop)

	if _, ok := s.mgr.Get(job.ID); !ok {
		t.Fatal("job expired while it was still running")
	}
	if status, _ := job.Status(); status != jobs.StatusCompleted {
		t.Errorf("got %s, want completed", status)
	}
}

func TestStatusReportsFinishedJob(t *testing.T) {
	_, h, job := completedJob(t, config.Config{FileTTL: time.Minute})

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/downloads/"+job.ID, nil))
	var ev downloader.ProgressEvent
	if err := json.Unmarshal(w.Body.Bytes(), &ev); err != nil || ev.Status != "done" {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

func TestCreateDownloadEnforcesPerIPLimit(t *testing.T) {
	bin := fakeYtDlp(t, "sleep 5")
	_, h := serverWith(config.Config{YtDlpPath: bin, MaxJobsPerIP: 1, MaxJobDuration: 2 * time.Second})
	body := `{"url":"https://youtube.com/watch?v=x","container":"mp4"}`

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/downloads", strings.NewReader(body)))
	if w.Code != http.StatusCreated {
		t.Fatalf("first download got %d %s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/downloads", strings.NewReader(body)))
	if w.Code != http.StatusTooManyRequests {
		t.Errorf("second concurrent download got %d, want 429", w.Code)
	}
}
