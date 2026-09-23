package downloader

import (
	"context"
	"slices"
	"strings"
	"testing"

	"vndl/internal/config"
)

func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

// Audit #2 VNDL-001/002/004: every download is single-item, size-capped,
// config-free, restricted to site extractors, and the URL can't be an option.
func TestBuildDownloadCmdHardening(t *testing.T) {
	cfg := config.Config{YtDlpPath: "yt-dlp", MaxFilesize: "2G"}
	const url = "https://www.youtube.com/playlist?list=x"
	args := BuildDownloadCmd(context.Background(), cfg, url, "", false, false, "mkv", t.TempDir()).Args

	for flag, want := range map[string]string{
		"--use-extractors": siteExtractors,
		"--playlist-items": "1",
		"--max-filesize":   "2G",
	} {
		if got := argAfter(args, flag); got != want {
			t.Errorf("%s = %q, want %q", flag, got, want)
		}
	}
	if !slices.Contains(args, "--ignore-config") {
		t.Error("missing --ignore-config")
	}
	if n := len(args); args[n-2] != "--" || args[n-1] != url {
		t.Errorf("URL must follow \"--\" at the end of argv, got %q", args[n-2:])
	}

	direct := BuildDownloadCmd(context.Background(), cfg, "https://video.twimg.com/a.mp4", "", false, true, "mkv", t.TempDir()).Args
	if got := argAfter(direct, "--use-extractors"); got != directExtractors {
		t.Errorf("direct download extractors = %q, want %q", got, directExtractors)
	}
}

// Audit #2 VNDL-005: logged errors must not reveal what was downloaded.
func TestScrubErrorRemovesContentIDsAndURLs(t *testing.T) {
	cases := []string{
		"exit status 1: ERROR: [youtube] ABCDEFGHIJK: Video unavailable",
		"probe failed: exit status 1: ERROR: [twitter] 2090883552552591439: No video could be found in this tweet",
		"ERROR: unable to download video data: HTTP Error 403: Forbidden (https://rr1.example/videoplayback?id=ABCDEFGHIJK)",
	}
	for _, in := range cases {
		out := ScrubError(in)
		for _, leak := range []string{"ABCDEFGHIJK", "2090883552552591439", "https://"} {
			if strings.Contains(out, leak) {
				t.Errorf("ScrubError(%q) = %q, still contains %q", in, out, leak)
			}
		}
	}
	if got := ScrubError("ERROR: [youtube] X: Private video"); FriendlyError(got) != "this video is private" {
		t.Errorf("scrubbed text should still classify, got %q", got)
	}
}
