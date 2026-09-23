package downloader

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
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

func ProbeFx(ctx context.Context, raw string) (*Metadata, error) {
	tweet, video, err := fetchFxVideo(ctx, raw)
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
		return nil, errNoFxVideo
	}
	return meta, nil
}

// ResolveFxURL returns the direct media URL for an fx- format, falling back
// to the highest bitrate if that exact variant is gone.
func ResolveFxURL(ctx context.Context, raw, formatID string) (string, error) {
	_, video, err := fetchFxVideo(ctx, raw)
	if err != nil {
		return "", err
	}
	variants := mp4Variants(video)
	if len(variants) == 0 {
		return "", errNoFxVideo
	}
	pick := variants[0]
	want := strings.TrimPrefix(formatID, fxFormatPrefix)
	for _, v := range variants {
		if strconv.Itoa(v.Bitrate) == want {
			pick = v
			break
		}
	}
	u, err := url.Parse(pick.URL)
	if err != nil || u.Scheme != "https" || !hostAllowed(strings.ToLower(u.Hostname()), fxMediaHosts) {
		return "", errors.New("fxtwitter: media url is not on an allowed host")
	}
	return pick.URL, nil
}

func fetchFxVideo(ctx context.Context, raw string) (*fxTweet, *fxVideo, error) {
	m := tweetIDRe.FindStringSubmatch(raw)
	if m == nil {
		return nil, nil, errors.New("fxtwitter: no tweet id in url")
	}
	endpoint := fxAPIBase + "/i/status/" + m[1]

	var tweet *fxTweet
	var err error
	// fxtwitter intermittently 404s a tweet it then serves on the next try.
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(fxRetryDelay):
			}
		}
		var retry bool
		tweet, retry, err = fxRequest(ctx, endpoint)
		if err == nil || !retry {
			break
		}
	}
	if err != nil {
		return nil, nil, err
	}

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

func fxRequest(ctx context.Context, endpoint string) (tweet *fxTweet, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("User-Agent", "vndl (+https://dl.vncius.dev)")

	resp, err := fxHTTPClient.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err // drop the request URL — it carries the tweet ID
		}
		return nil, true, fmt.Errorf("fxtwitter: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		retry := resp.StatusCode == http.StatusNotFound || resp.StatusCode >= 500
		return nil, retry, fmt.Errorf("fxtwitter: http %d", resp.StatusCode)
	}

	var body struct {
		Code  int     `json:"code"`
		Tweet fxTweet `json:"tweet"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return nil, false, fmt.Errorf("fxtwitter: decode: %w", err)
	}
	if body.Code != http.StatusOK {
		return nil, body.Code == http.StatusNotFound, fmt.Errorf("fxtwitter: code %d", body.Code)
	}
	return &body.Tweet, false, nil
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
