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
