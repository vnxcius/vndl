package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"vndl/internal/config"
	"vndl/internal/jobs"
)

// parseArgs sets $f (format) and $o (output) from a fake binary's argv.
const parseArgs = `for a in "$@"; do case "$prev" in -f) f="$a";; -o) o="$a";; -i) i="$a";; esac; prev="$a"; done
`

// streamsFormat is a fake yt-dlp that streams "[<format>]" to stdout.
const streamsFormat = parseArgs + `echo '[download]  50.0% of 1.00MiB at 1.00MiB/s ETA 00:01' >&2
printf '[%s]' "$f"`

// catsInputs is a fake ffmpeg: a merge emits both piped inputs, an mp3
// conversion its input file.
const catsInputs = parseArgs + `if [ -n "$i" ] && [ "$i" != pipe:4 ]; then printf 'mp3:'; cat "$i"; else cat <&3; cat <&4; fi`

func get(h http.Handler, job *jobs.Job) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/downloads/"+job.ID+"/file", nil))
	return w
}

func TestFileStreamsSingleFormat(t *testing.T) {
	s, h := serverWith(config.Config{YtDlpPath: fakeYtDlp(t, streamsFormat)})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "18", false, "clip", "mp4", "mp4")

	w := get(h, job)
	if w.Code != http.StatusOK || w.Body.String() != "[18]" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="clip.mp4"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if status, _ := job.Status(); status != jobs.StatusCompleted {
		t.Errorf("job status %s, want completed", status)
	}
}

func TestFileStreamsMergedFormatThroughFfmpeg(t *testing.T) {
	s, h := serverWith(config.Config{YtDlpPath: fakeYtDlp(t, streamsFormat), FfmpegPath: fakeYtDlp(t, catsInputs)})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "137+bestaudio[ext=m4a]/137+bestaudio/best", false, "clip", "mp4", "mp4")

	w := get(h, job)
	if want := "[137][bestaudio[ext=m4a]/bestaudio]"; w.Code != http.StatusOK || w.Body.String() != want {
		t.Fatalf("got %d %q, want %q", w.Code, w.Body.String(), want)
	}
}

func TestFileStreamsMP3(t *testing.T) {
	writesInput := parseArgs + `printf audio > "$(dirname "$o")/input.m4a"`
	s, h := serverWith(config.Config{YtDlpPath: fakeYtDlp(t, writesInput), FfmpegPath: fakeYtDlp(t, catsInputs)})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "", true, "song", "mp3", "")

	w := get(h, job)
	if w.Code != http.StatusOK || w.Body.String() != "mp3:audio" {
		t.Fatalf("got %d %q", w.Code, w.Body.String())
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename="song.mp3"` {
		t.Errorf("Content-Disposition = %q", cd)
	}
}

// Once bytes are flowing, a failure must cut the connection rather than end
// the response cleanly, or the browser keeps a truncated file as complete.
func TestFileAbortsConnectionOnFailureMidStream(t *testing.T) {
	cases := map[string]config.Config{
		"single": {YtDlpPath: fakeYtDlp(t, "printf partial\nexit 1")},
		// ffmpeg exits cleanly on a yt-dlp that died; yt-dlp's exit must win.
		"merged": {YtDlpPath: fakeYtDlp(t, "printf partial\nexit 1"), FfmpegPath: fakeYtDlp(t, catsInputs)},
	}
	for name, cfg := range cases {
		t.Run(name, func(t *testing.T) {
			s, h := serverWith(cfg)
			format := "18"
			if name == "merged" {
				format = "137+140"
			}
			job := s.mgr.Create("https://www.youtube.com/watch?v=x", format, false, "t", "mp4", "mp4")
			srv := httptest.NewServer(h)
			defer srv.Close()

			resp, err := http.Get(srv.URL + "/api/downloads/" + job.ID + "/file")
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if _, err := io.ReadAll(resp.Body); err == nil {
				t.Error("response ended cleanly; want the connection aborted")
			}
			if status, _ := job.Status(); status != jobs.StatusError {
				t.Errorf("job status %s, want error", status)
			}
		})
	}
}

func TestFileRejectsOutputOverSizeLimit(t *testing.T) {
	bin := fakeYtDlp(t, "head -c 100 /dev/zero")
	s, h := serverWith(config.Config{YtDlpPath: bin, MaxFilesize: "10"})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "18", false, "t", "mp4", "mp4")

	if w := get(h, job); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "larger than") {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

func TestFileRejectsYtDlpSkippingOverMaxFilesize(t *testing.T) {
	bin := fakeYtDlp(t, "echo '[download] File is larger than max-filesize (100 bytes > 10 bytes). Aborting.' >&2")
	s, h := serverWith(config.Config{YtDlpPath: bin, MaxFilesize: "10"})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "18", false, "t", "mp4", "mp4")

	if w := get(h, job); w.Code != http.StatusUnprocessableEntity || !strings.Contains(w.Body.String(), "larger than") {
		t.Errorf("got %d %s", w.Code, w.Body.String())
	}
}

// The browser's retry button requests the same URL again.
func TestFileCanRetryAfterFailureButNotAfterSuccess(t *testing.T) {
	marker := t.TempDir() + "/ran"
	// Fails the first time, succeeds the second.
	bin := fakeYtDlp(t, `if [ -e `+marker+` ]; then printf ok; else touch `+marker+`; exit 1; fi`)
	s, h := serverWith(config.Config{YtDlpPath: bin})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "18", false, "t", "mp4", "mp4")

	if w := get(h, job); w.Code != http.StatusUnprocessableEntity {
		t.Fatalf("first attempt got %d, want 422", w.Code)
	}
	if w := get(h, job); w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("retry got %d %q", w.Code, w.Body.String())
	}
	if w := get(h, job); w.Code != http.StatusConflict {
		t.Errorf("request after success got %d, want 409", w.Code)
	}
}

func TestFileEnforcesPerIPLimit(t *testing.T) {
	s, h := serverWith(config.Config{YtDlpPath: fakeYtDlp(t, "sleep 1; printf ok"), MaxJobsPerIP: 1})
	first := s.mgr.Create("https://www.youtube.com/watch?v=x", "18", false, "t", "mp4", "mp4")
	second := s.mgr.Create("https://www.youtube.com/watch?v=y", "18", false, "t", "mp4", "mp4")

	done := make(chan struct{})
	go func() { get(h, first); close(done) }()
	time.Sleep(200 * time.Millisecond)

	if w := get(h, second); w.Code != http.StatusTooManyRequests {
		t.Errorf("concurrent download got %d, want 429", w.Code)
	}
	<-done
}

// A stream can run past JOB_TTL; the job must not expire under it.
func TestStreamingJobNotExpiredByJobTTL(t *testing.T) {
	s, h := serverWith(config.Config{YtDlpPath: fakeYtDlp(t, "sleep 0.4; printf ok"), JobTTL: 100 * time.Millisecond})
	job := s.mgr.Create("https://www.youtube.com/watch?v=x", "18", false, "t", "mp4", "mp4")

	if w := get(h, job); w.Code != http.StatusOK {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
	if _, ok := s.mgr.Get(job.ID); !ok {
		t.Error("job expired while it was streaming")
	}
}
