// Package downloader wraps the yt-dlp binary for probing metadata/formats
// and building the streaming download command.
package downloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"vndl/internal/config"
)

var AllowedHosts = []string{
	"youtube.com", "youtu.be", "m.youtube.com",
	"twitter.com", "x.com", "mobile.twitter.com",
	"tiktok.com", "vm.tiktok.com", "vt.tiktok.com",
	"instagram.com",
}

// NSFWHosts are gated client-side only; the backend has no state to enforce
// the toggle with, so it accepts both lists unconditionally.
var NSFWHosts = []string{
	"pornhub.com",
	"xvideos.com",
	"xnxx.com",
	"xhamster.com",
	"redtube.com",
}

func IsAllowedURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return hostAllowed(host, AllowedHosts) || hostAllowed(host, NSFWHosts)
}

func hostAllowed(host string, list []string) bool {
	for _, allowed := range list {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return true
		}
	}
	return false
}

// StripFragment closes off yt-dlp's URL-fragment "smuggling" vector
// (GHSA-3ch3-jhc6-5r8x) — none of the allow-listed platforms need a
// fragment to identify content.
func StripFragment(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.Fragment = ""
	u.RawFragment = ""
	return u.String()
}

type Format struct {
	ID         string `json:"format_id"`
	Ext        string `json:"ext"`
	Resolution string `json:"resolution"`
	Note       string `json:"format_note"`
	VCodec     string `json:"vcodec"`
	ACodec     string `json:"acodec"`
	Filesize   int64  `json:"filesize"`
	// Primary quality-ranking signal — some platforms report filesize as
	// null for every format.
	Height int `json:"height"`
}

type Metadata struct {
	Title    string   `json:"title"`
	Thumb    string   `json:"thumbnail"`
	Duration float64  `json:"duration"`
	Uploader string   `json:"uploader"`
	Formats  []Format `json:"formats"`
}

// Only site-specific extractors: the generic one follows whatever a page
// links to, which would bypass the host allow-list.
const (
	siteExtractors   = "default,-generic"
	directExtractors = "generic"
)

// baseArgs applies to every yt-dlp run: no config files, a single item even
// for playlist/channel URLs, and a bounded socket timeout.
func baseArgs(extractors string) []string {
	return []string{
		"--ignore-config",
		"--use-extractors", extractors,
		"--no-playlist",
		"--playlist-items", "1",
		"--socket-timeout", "30",
		"--no-warnings",
	}
}

func Probe(ctx context.Context, cfg config.Config, rawURL string) (*Metadata, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	args := append(baseArgs(siteExtractors), "-j", "--skip-download", "--", rawURL)
	cmd := exec.CommandContext(ctx, cfg.YtDlpPath, args...)
	killGroupOnCancel(cmd)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("probe failed: %w: %s", err, strings.TrimSpace(stderr.String()))
	}

	var meta Metadata
	if err := json.Unmarshal(stdout.Bytes(), &meta); err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}
	return &meta, nil
}

// BuildDownloadCmd writes to outDir as "output.<ext>" rather than piping to
// stdout — yt-dlp/ffmpeg silently skip merging/transcoding when writing to
// a pipe. Caller deletes outDir after serving the result. container only
// applies when merging separate streams; ignored for progressive formats
// and audioOnly. direct marks a server-resolved media URL (fxtwitter),
// the only case allowed through the generic extractor.
func BuildDownloadCmd(ctx context.Context, cfg config.Config, rawURL, formatID string, audioOnly, direct bool, container, outDir string) *exec.Cmd {
	extractors := siteExtractors
	if direct {
		extractors = directExtractors
	}
	args := append(baseArgs(extractors),
		"--newline",
		"--abort-on-unavailable-fragments", // default skips dead fragments, stalling then yielding a corrupt file
	)
	if cfg.MaxFilesize != "" {
		args = append(args, "--max-filesize", cfg.MaxFilesize)
	}
	if cfg.FfmpegPath != "" {
		args = append(args, "--ffmpeg-location", cfg.FfmpegPath)
	}

	if audioOnly {
		args = append(args, "-f", "bestaudio/best", "-x", "--audio-format", "mp3")
	} else {
		f := formatID
		if f == "" {
			f = "bestvideo+bestaudio/best"
		}
		if container == "" {
			container = "mkv"
		}
		args = append(args, "-f", f, "--merge-output-format", container)
	}

	args = append(args, "-o", filepath.Join(outDir, "output.%(ext)s"), "--", rawURL)

	cmd := exec.CommandContext(ctx, cfg.YtDlpPath, args...)
	killGroupOnCancel(cmd)
	return cmd
}

// IsMaxFilesizeLine reports yt-dlp's notice for a download it skipped
// because of --max-filesize (it exits 0 without writing a file).
func IsMaxFilesizeLine(line string) bool {
	return strings.Contains(line, "larger than max-filesize")
}

var (
	extractorIDRe = regexp.MustCompile(`\[([\w:-]+)\] [^\s:]+:`)
	urlRe         = regexp.MustCompile(`https?://\S+`)
)

// ScrubError strips content IDs and URLs from yt-dlp output before it's
// logged, e.g. "ERROR: [youtube] <id>: Video unavailable".
func ScrubError(s string) string {
	s = urlRe.ReplaceAllString(s, "<url>")
	return extractorIDRe.ReplaceAllString(s, "[$1] <id>:")
}

func ContentType(ext string) string {
	switch strings.ToLower(ext) {
	case "mp3":
		return "audio/mpeg"
	case "m4a":
		return "audio/mp4"
	case "opus":
		return "audio/opus"
	case "mp4":
		return "video/mp4"
	case "mkv":
		return "video/x-matroska"
	case "webm":
		return "video/webm"
	default:
		return "application/octet-stream"
	}
}

// progressLine matches yt-dlp's default `--newline` progress output, e.g.:
// "[download]  42.5% of   69.42MiB at    3.11MiB/s ETA 00:12"
var progressLine = regexp.MustCompile(
	`\[download\]\s+(\d+(?:\.\d+)?)% of\s+~?\s*([\d.]+\w+)(?:\s+at\s+([\d.]+\w+/s|Unknown speed))?(?:\s+ETA\s+(\S+))?`,
)

type ProgressEvent struct {
	Status  string  `json:"status"`
	Percent float64 `json:"percent,omitempty"`
	Total   string  `json:"total,omitempty"`
	Speed   string  `json:"speed,omitempty"`
	ETA     string  `json:"eta,omitempty"`
}

func ParseProgressLine(line string) (ProgressEvent, bool) {
	if m := progressLine.FindStringSubmatch(line); m != nil {
		pct, _ := strconv.ParseFloat(m[1], 64)
		return ProgressEvent{
			Status:  "downloading",
			Percent: pct,
			Total:   m[2],
			Speed:   m[3],
			ETA:     m[4],
		}, true
	}
	if strings.Contains(line, "[Merger]") || strings.Contains(line, "[ExtractAudio]") ||
		strings.Contains(line, "[VideoConvertor]") {
		return ProgressEvent{Status: "processing"}, true
	}
	return ProgressEvent{}, false
}

// ETASmoother damps jitter in yt-dlp's own ETA, which it recomputes from
// instantaneous speed on every line. Not safe for concurrent use — one per
// download.
type ETASmoother struct {
	smoothed    float64
	initialized bool
}

const etaSmoothingFactor = 0.25 // lower = smoother, slower to react

// Smooth passes unparseable values (e.g. "Unknown") through unchanged.
func (s *ETASmoother) Smooth(raw string) string {
	secs, ok := parseETASeconds(raw)
	if !ok {
		return raw
	}
	if !s.initialized {
		s.smoothed = secs
		s.initialized = true
	} else {
		s.smoothed = etaSmoothingFactor*secs + (1-etaSmoothingFactor)*s.smoothed
	}
	return formatETASeconds(s.smoothed)
}

func parseETASeconds(raw string) (float64, bool) {
	parts := strings.Split(raw, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	vals := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, false
		}
		vals[i] = n
	}
	if len(vals) == 2 {
		return float64(vals[0]*60 + vals[1]), true
	}
	return float64(vals[0]*3600 + vals[1]*60 + vals[2]), true
}

const genericProbeFailure = "could not read that link — it may be private, deleted, or unsupported"

// FriendlyError maps a handful of yt-dlp's known error phrases to a more
// specific message. Best-effort — exact wording can shift between
// yt-dlp versions; the raw text is always still logged separately.
func FriendlyError(raw string) string {
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "sign in to confirm your age"), strings.Contains(lower, "age-restricted"):
		return "this video is age-restricted and requires a signed-in account — not currently supported"
	case strings.Contains(lower, "private video"):
		return "this video is private"
	case strings.Contains(lower, "video unavailable"), strings.Contains(lower, "this video is not available"):
		return "this video is unavailable — it may have been removed"
	case strings.Contains(lower, "not made this video available in your country"):
		return "this video isn't available from the server's region"
	case strings.Contains(lower, "no video could be found"):
		return "no video found at that link — if you're sure it has one, it may be age-restricted or region-locked content, which isn't supported yet"
	case strings.Contains(lower, "http error 410"), strings.Contains(lower, "http error 403"):
		return "the site refused to serve this video's file — downloads from it may be temporarily broken"
	}
	return genericProbeFailure
}

func formatETASeconds(secs float64) string {
	s := int(secs + 0.5)
	if s < 0 {
		s = 0
	}
	h, m, sec := s/3600, (s%3600)/60, s%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%02d:%02d", m, sec)
}
