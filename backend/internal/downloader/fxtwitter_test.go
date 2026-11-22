package downloader

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

const fxSample = `{"code":200,"message":"OK","tweet":{"id":"2090883552552591439","text":"Anal Only \nhttps://onlyfans.com/x","author":{"name":"."},"media":{"videos":[{"thumbnail_url":"https://pbs.twimg.com/t.jpg","duration":24.729,"variants":[
{"url":"https://video.twimg.com/amplify_video/1/pl/a.m3u8?tag=14","bitrate":0,"content_type":"application/x-mpegURL"},
{"url":"https://video.twimg.com/amplify_video/1/vid/avc1/320x540/b.mp4?tag=14","bitrate":632000,"content_type":"video/mp4"},
{"url":"https://video.twimg.com/amplify_video/1/vid/avc1/480x812/c.mp4?tag=14","bitrate":950000,"content_type":"video/mp4"}]}]}}}`

const tweetURL = "https://x.com/tomiie_x/status/2090883552552591439"

var discard = slog.New(slog.NewTextHandler(io.Discard, nil))

func fakeFx(t *testing.T, body string, failFirst int) *atomic.Int32 {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		if r.URL.Path != "/i/status/2090883552552591439" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if int(n) <= failFirst {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)

	fxCache.Lock()
	fxCache.items = make(map[string]fxCacheEntry)
	fxCache.Unlock()

	oldBase, oldDelay := fxAPIBase, fxRetryDelay
	fxAPIBase, fxRetryDelay = srv.URL, 0
	t.Cleanup(func() { fxAPIBase, fxRetryDelay = oldBase, oldDelay })
	return &calls
}

func TestProbeFxRetriesTransient404(t *testing.T) {
	calls := fakeFx(t, fxSample, 1)

	var logs bytes.Buffer
	meta, err := ProbeFx(context.Background(), tweetURL, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("ProbeFx: %v", err)
	}

	out := logs.String()
	for _, want := range []string{`"outcome":"retry"`, `"http_status":404`, `"outcome":"ok"`, `"msg":"fxtwitter.probe"`, `"formats":2`} {
		if !strings.Contains(out, want) {
			t.Errorf("logs missing %s:\n%s", want, out)
		}
	}
	for _, leak := range []string{"2090883552552591439", "twimg.com", "http://", "https://"} {
		if strings.Contains(out, leak) {
			t.Errorf("logs leak %q:\n%s", leak, out)
		}
	}
	if calls.Load() != 2 {
		t.Errorf("calls = %d, want 2 (one 404, one retry)", calls.Load())
	}
	if meta.Title != ". - Anal Only" {
		t.Errorf("title = %q", meta.Title)
	}
	if len(meta.Formats) != 2 {
		t.Fatalf("formats = %+v, want only the 2 mp4 variants", meta.Formats)
	}
	top := meta.Formats[0]
	if top.ID != "fx-950000" || top.Height != 812 || top.Resolution != "480x812" || top.ACodec == "none" {
		t.Errorf("top format = %+v", top)
	}
}

func TestResolveFxURLPicksVariantAndChecksHost(t *testing.T) {
	fakeFx(t, fxSample, 0)

	got, err := ResolveFxURL(context.Background(), tweetURL, "fx-632000", discard)
	if err != nil || !strings.Contains(got, "320x540") {
		t.Errorf("fx-632000 -> %q, %v", got, err)
	}
	got, err = ResolveFxURL(context.Background(), tweetURL, "fx-1", discard)
	if err != nil || !strings.Contains(got, "480x812") {
		t.Errorf("unknown bitrate should fall back to highest, got %q, %v", got, err)
	}

	fakeFx(t, strings.ReplaceAll(fxSample, "video.twimg.com", "evil.example"), 0)
	if _, err := ResolveFxURL(context.Background(), tweetURL, "fx-950000", discard); err == nil {
		t.Error("expected a non-twimg media host to be rejected")
	}
}

// Regression: a download's fresh lookup 404'd through every retry seconds
// after its probe succeeded.
func TestResolveFxURLReusesProbeLookup(t *testing.T) {
	calls := fakeFx(t, fxSample, 0)

	if _, err := ProbeFx(context.Background(), tweetURL, discard); err != nil {
		t.Fatalf("ProbeFx: %v", err)
	}
	var logs bytes.Buffer
	if _, err := ResolveFxURL(context.Background(), tweetURL, "fx-950000", slog.New(slog.NewJSONHandler(&logs, nil))); err != nil {
		t.Fatalf("ResolveFxURL: %v", err)
	}
	if calls.Load() != 1 {
		t.Errorf("fxtwitter calls = %d, want 1 (resolve should reuse the probe's lookup)", calls.Load())
	}
	if !strings.Contains(logs.String(), `"outcome":"cache_hit"`) {
		t.Errorf("expected a cache_hit log line:\n%s", logs.String())
	}
}

func TestNeedsFxFallbackOnlyForSensitiveTweets(t *testing.T) {
	noVideo := errors.New("ERROR: [twitter] 1: No video could be found in this tweet")
	cases := []struct {
		url  string
		err  error
		want bool
	}{
		{tweetURL, noVideo, true},
		{"https://twitter.com/a/status/1", noVideo, true},
		{tweetURL + "/video/1", errors.New("ERROR: [twitter] 1: Video #1 is unavailable"), true},
		{tweetURL + "/photo/1", errors.New("ERROR: [twitter] 1: Media #1 is not a video"), false},
		{"https://www.youtube.com/watch?v=x", noVideo, false},
		{tweetURL, errors.New("ERROR: Private video"), false},
		{tweetURL, nil, false},
	}
	for _, c := range cases {
		if got := NeedsFxFallback(c.url, c.err); got != c.want {
			t.Errorf("NeedsFxFallback(%q, %v) = %v, want %v", c.url, c.err, got, c.want)
		}
	}
}

const fxPhotoSample = `{"code":200,"message":"OK","tweet":{"id":"2090883552552591439","text":"two pics","author":{"name":"A"},"media":{"photos":[
{"type":"photo","url":"https://pbs.twimg.com/media/One.jpg?name=orig","width":1920,"height":1080},
{"type":"photo","url":"https://pbs.twimg.com/media/Two.png?name=orig","width":800,"height":600}]}}}`

func TestProbeFxListsPhotos(t *testing.T) {
	fakeFx(t, fxPhotoSample, 0)

	meta, err := ProbeFx(context.Background(), tweetURL, discard)
	if err != nil {
		t.Fatalf("ProbeFx: %v", err)
	}
	if len(meta.Formats) != 2 {
		t.Fatalf("formats = %+v, want 2 photos", meta.Formats)
	}
	first, second := meta.Formats[0], meta.Formats[1]
	if first.ID != "fximg-1" || first.Ext != "jpg" || first.Resolution != "1920x1080" || first.VCodec != "none" || first.ACodec != "none" {
		t.Errorf("first photo = %+v", first)
	}
	if second.ID != "fximg-2" || second.Ext != "png" {
		t.Errorf("second photo = %+v", second)
	}
	if !strings.Contains(meta.Thumb, "One.jpg") || !strings.Contains(meta.Thumb, "name=small") {
		t.Errorf("thumb = %q", meta.Thumb)
	}
	if meta.Duration != 0 {
		t.Errorf("duration = %v", meta.Duration)
	}
}

func TestProbeFxListsVideoThenPhotos(t *testing.T) {
	mixed := strings.Replace(fxSample, `"media":{`, `"media":{"photos":[{"url":"https://pbs.twimg.com/media/P.jpg","width":10,"height":10}],`, 1)
	fakeFx(t, mixed, 0)

	meta, err := ProbeFx(context.Background(), tweetURL, discard)
	if err != nil {
		t.Fatalf("ProbeFx: %v", err)
	}
	if len(meta.Formats) != 3 || !IsFxFormat(meta.Formats[0].ID) || meta.Formats[2].ID != "fximg-1" {
		t.Fatalf("formats = %+v, want 2 videos then 1 photo", meta.Formats)
	}
	if meta.Thumb != "https://pbs.twimg.com/t.jpg" {
		t.Errorf("thumb = %q, want the video's", meta.Thumb)
	}
}

func TestProbeFxNoMedia(t *testing.T) {
	fakeFx(t, `{"code":200,"tweet":{"id":"1","text":"just text","media":{}}}`, 0)
	_, err := ProbeFx(context.Background(), tweetURL, discard)
	if !errors.Is(err, errNoFxMedia) {
		t.Fatalf("err = %v, want errNoFxMedia", err)
	}
	if FriendlyError(err.Error()) == genericProbeFailure {
		t.Errorf("no-media error should get a specific message")
	}
}

func TestResolveFxPhoto(t *testing.T) {
	fakeFx(t, fxPhotoSample, 0)

	got, ext, err := ResolveFxPhoto(context.Background(), tweetURL, "fximg-2", discard)
	if err != nil || got != "https://pbs.twimg.com/media/Two.png?name=orig" || ext != "png" {
		t.Errorf("fximg-2 -> %q, %q, %v", got, ext, err)
	}
	for _, bad := range []string{"fximg-0", "fximg-3", "fximg-x"} {
		if _, _, err := ResolveFxPhoto(context.Background(), tweetURL, bad, discard); err == nil {
			t.Errorf("%s: expected an error", bad)
		}
	}

	fakeFx(t, strings.ReplaceAll(fxPhotoSample, "pbs.twimg.com", "evil.example"), 0)
	if _, _, err := ResolveFxPhoto(context.Background(), tweetURL, "fximg-1", discard); err == nil {
		t.Error("expected a non-twimg photo host to be rejected")
	}
}

func TestIsTwitterPhotoURL(t *testing.T) {
	cases := map[string]bool{
		tweetURL + "/photo/1":                    true,
		"https://twitter.com/a/status/1/photo/4": true,
		tweetURL:                                 false,
		tweetURL + "/video/1":                    false,
		"https://youtube.com/status/1/photo/1":   false,
	}
	for u, want := range cases {
		if got := IsTwitterPhotoURL(u); got != want {
			t.Errorf("IsTwitterPhotoURL(%q) = %v, want %v", u, got, want)
		}
	}
}

func TestFetchPhotoChecksResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte("jpegbytes"))
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			_, _ = w.Write([]byte("<html>"))
		default:
			http.Redirect(w, r, "https://evil.example/", http.StatusFound)
		}
	}))
	defer srv.Close()

	var buf bytes.Buffer
	if n, err := FetchPhoto(context.Background(), srv.URL+"/ok.jpg", &buf); err != nil || n != 9 || buf.String() != "jpegbytes" {
		t.Errorf("ok: n=%d err=%v body=%q", n, err, buf.String())
	}
	if _, err := FetchPhoto(context.Background(), srv.URL+"/html", io.Discard); err == nil {
		t.Error("expected a non-image response to be rejected")
	}
	if _, err := FetchPhoto(context.Background(), srv.URL+"/redirect", io.Discard); err == nil {
		t.Error("expected a redirect not to be followed")
	}
}
