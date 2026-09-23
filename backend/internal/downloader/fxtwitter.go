package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// fxtwitter resolves tweets yt-dlp can't see logged out (sensitive media).
// Only the tweet ID is sent, server-side — never the visitor's IP.

const fxFormatPrefix = "fx-"

var (
	fxAPIBase    = "https://api.fxtwitter.com"
	fxRetryDelay = time.Second
	fxHTTPClient = &http.Client{Timeout: 10 * time.Second}
	tweetIDRe    = regexp.MustCompile(`/status(?:es)?/(\d+)`)
	videoIndexRe = regexp.MustCompile(`/video/(\d+)`)
	dimensionsRe = regexp.MustCompile(`/(\d+)x(\d+)/`)
	errNoFxVideo = errors.New("fxtwitter: tweet has no video")
	fxMediaHosts = []string{"video.twimg.com"}
)

type fxVariant struct {
	URL         string `json:"url"`
	Bitrate     int    `json:"bitrate"`
	ContentType string `json:"content_type"`
}

type fxVideo struct {
	ThumbnailURL string      `json:"thumbnail_url"`
	Duration     float64     `json:"duration"`
	Variants     []fxVariant `json:"variants"`
}

type fxTweet struct {
	ID     string `json:"id"`
	Text   string `json:"text"`
	Author struct {
		Name string `json:"name"`
	} `json:"author"`
	Media struct {
		Videos []fxVideo `json:"videos"`
	} `json:"media"`
}

func IsTwitterURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && hostAllowed(strings.ToLower(u.Hostname()), []string{"twitter.com", "x.com"})
}

func IsFxFormat(formatID string) bool {
	return strings.HasPrefix(formatID, fxFormatPrefix)
}

// NeedsFxFallback reports whether a failed yt-dlp probe is the logged-out
// sensitive-media case.
func NeedsFxFallback(raw string, probeErr error) bool {
	return probeErr != nil && IsTwitterURL(raw) &&
		strings.Contains(strings.ToLower(probeErr.Error()), "no video could be found")
}

// ProbeFx and ResolveFxURL log only counts, statuses and timings — never
// the tweet ID or any URL.
func ProbeFx(ctx context.Context, raw string, log *slog.Logger) (*Metadata, error) {
	log = log.With("op", "probe")
	tweet, video, err := fetchFxVideo(ctx, raw, log)
	if err != nil {
		return nil, err
	}

	meta := &Metadata{
		Title:    fxTitle(tweet),
		Thumb:    video.ThumbnailURL,
		Duration: video.Duration,
		Uploader: tweet.Author.Name,
	}
	for _, v := range mp4Variants(video) {
		f := Format{
			ID:     fxFormatPrefix + strconv.Itoa(v.Bitrate),
			Ext:    "mp4",
			VCodec: "avc1",
			ACodec: "mp4a",
		}
		if m := dimensionsRe.FindStringSubmatch(v.URL); m != nil {
			f.Resolution = m[1] + "x" + m[2]
			f.Height, _ = strconv.Atoi(m[2])
		}
		meta.Formats = append(meta.Formats, f)
	}
	if len(meta.Formats) == 0 {
		log.Warn("fxtwitter.probe", "status", "error", "videos", len(tweet.Media.Videos), "error", errNoFxVideo.Error())
		return nil, errNoFxVideo
	}
	log.Info("fxtwitter.probe", "status", "ok",
		"videos", len(tweet.Media.Videos), "formats", len(meta.Formats),
		"top_height", meta.Formats[0].Height, "duration_s", video.Duration)
	return meta, nil
}

// ResolveFxURL returns the direct media URL for an fx- format, falling back
// to the highest bitrate if that exact variant is gone.
func ResolveFxURL(ctx context.Context, raw, formatID string, log *slog.Logger) (string, error) {
	log = log.With("op", "resolve", "requested", formatID)
	_, video, err := fetchFxVideo(ctx, raw, log)
	if err != nil {
		return "", err
	}
	variants := mp4Variants(video)
	if len(variants) == 0 {
		log.Warn("fxtwitter.resolve", "status", "error", "error", errNoFxVideo.Error())
		return "", errNoFxVideo
	}
	pick, exact := variants[0], false
	want := strings.TrimPrefix(formatID, fxFormatPrefix)
	for _, v := range variants {
		if strconv.Itoa(v.Bitrate) == want {
			pick, exact = v, true
			break
		}
	}
	u, err := url.Parse(pick.URL)
	if err != nil || u.Scheme != "https" || !hostAllowed(strings.ToLower(u.Hostname()), fxMediaHosts) {
		err = errors.New("fxtwitter: media url is not on an allowed host")
		log.Warn("fxtwitter.resolve", "status", "error", "error", err.Error())
		return "", err
	}
	log.Info("fxtwitter.resolve", "status", "ok",
		"variants", len(variants), "picked", fxFormatPrefix+strconv.Itoa(pick.Bitrate),
		"exact_match", exact, "media_host", u.Hostname())
	return pick.URL, nil
}

const (
	fxMaxAttempts = 3
	fxCacheTTL    = 30 * time.Minute
)

// fxCache lets a download reuse its probe's lookup: fxtwitter is flaky
// enough that a second call seconds later can 404 through every retry.
var fxCache = struct {
	sync.Mutex
	items map[string]fxCacheEntry
}{items: make(map[string]fxCacheEntry)}

type fxCacheEntry struct {
	tweet    *fxTweet
	cachedAt time.Time
}

func fxCacheGet(id string) (*fxTweet, bool) {
	fxCache.Lock()
	defer fxCache.Unlock()
	e, ok := fxCache.items[id]
	if !ok || time.Since(e.cachedAt) > fxCacheTTL {
		return nil, false
	}
	return e.tweet, true
}

func fxCacheSet(id string, tweet *fxTweet) {
	fxCache.Lock()
	defer fxCache.Unlock()
	now := time.Now()
	for k, e := range fxCache.items {
		if now.Sub(e.cachedAt) > fxCacheTTL {
			delete(fxCache.items, k)
		}
	}
	fxCache.items[id] = fxCacheEntry{tweet: tweet, cachedAt: now}
}

func fetchFxVideo(ctx context.Context, raw string, log *slog.Logger) (*fxTweet, *fxVideo, error) {
	m := tweetIDRe.FindStringSubmatch(raw)
	if m == nil {
		err := errors.New("fxtwitter: no tweet id in url")
		log.Warn("fxtwitter.request", "outcome", "failed", "error", err.Error())
		return nil, nil, err
	}
	tweetID := m[1]

	tweet, cached := fxCacheGet(tweetID)
	if cached {
		log.Info("fxtwitter.request", "outcome", "cache_hit")
		return pickFxVideo(raw, tweet)
	}

	endpoint := fxAPIBase + "/i/status/" + tweetID
	var err error
	// fxtwitter intermittently 404s a tweet it then serves on the next try.
	for attempt := 1; attempt <= fxMaxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(fxRetryDelay):
			}
		}
		start := time.Now()
		var retry bool
		var status int
		tweet, status, retry, err = fxRequest(ctx, endpoint)

		fields := []any{"attempt", attempt, "max_attempts", fxMaxAttempts,
			"http_status", status, "duration_ms", time.Since(start).Milliseconds()}
		switch {
		case err == nil:
			log.Info("fxtwitter.request", append(fields, "outcome", "ok")...)
		case retry && attempt < fxMaxAttempts:
			log.Warn("fxtwitter.request", append(fields, "outcome", "retry", "error", err.Error())...)
			continue
		default:
			log.Warn("fxtwitter.request", append(fields, "outcome", "failed", "error", err.Error())...)
		}
		break
	}
	if err != nil {
		return nil, nil, err
	}
	fxCacheSet(tweetID, tweet)
	return pickFxVideo(raw, tweet)
}

func pickFxVideo(raw string, tweet *fxTweet) (*fxTweet, *fxVideo, error) {
	videos := tweet.Media.Videos
	if len(videos) == 0 {
		return nil, nil, errNoFxVideo
	}
	idx := 0
	if vm := videoIndexRe.FindStringSubmatch(raw); vm != nil {
		if n, _ := strconv.Atoi(vm[1]); n >= 1 && n <= len(videos) {
			idx = n - 1
		}
	}
	return tweet, &videos[idx], nil
}

func fxRequest(ctx context.Context, endpoint string) (tweet *fxTweet, status int, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, 0, false, err
	}
	req.Header.Set("User-Agent", "vndl (+https://dl.vncius.dev)")

	resp, err := fxHTTPClient.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the request URL — it carries the tweet ID
		}
		return nil, 0, true, fmt.Errorf("fxtwitter: %w", err)
	}
	defer resp.Body.Close()

	status = resp.StatusCode
	if status != http.StatusOK {
		retry := status == http.StatusNotFound || status >= 500
		return nil, status, retry, fmt.Errorf("fxtwitter: http %d", status)
	}

	var body struct {
		Code  int     `json:"code"`
		Tweet fxTweet `json:"tweet"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, status, false, fmt.Errorf("fxtwitter: decode: %w", err)
	}
	if body.Code != http.StatusOK {
		return nil, status, body.Code == http.StatusNotFound, fmt.Errorf("fxtwitter: code %d", body.Code)
	}
	return &body.Tweet, status, false, nil
}

// mp4Variants returns progressive mp4 variants, highest bitrate first.
func mp4Variants(v *fxVideo) []fxVariant {
	var out []fxVariant
	for _, variant := range v.Variants {
		if variant.ContentType == "video/mp4" {
			out = append(out, variant)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Bitrate > out[j].Bitrate })
	return out
}

func fxTitle(t *fxTweet) string {
	text := strings.TrimSpace(strings.SplitN(t.Text, "\n", 2)[0])
	if r := []rune(text); len(r) > 80 {
		text = string(r[:80])
	}
	switch {
	case text != "" && t.Author.Name != "":
		return t.Author.Name + " - " + text
	case text != "":
		return text
	default:
		return "tweet " + t.ID
	}
}
