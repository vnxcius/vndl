package downloader

import (
	"math"
	"testing"
)

// Regression for VNDL-007: URL fragments (yt-dlp's "smuggling" vector,
// GHSA-3ch3-jhc6-5r8x) must never reach yt-dlp's argv.
func TestStripFragmentRemovesSmuggledData(t *testing.T) {
	cases := []struct{ in, want string }{
		{
			"https://youtube.com/watch?v=x#__youtubedl_smuggle={\"http_headers\":{\"Ytdl-request-proxy\":\"http://evil\"}}",
			"https://youtube.com/watch?v=x",
		},
		{"https://x.com/user/status/1", "https://x.com/user/status/1"},
		{"https://youtube.com/watch?v=x#", "https://youtube.com/watch?v=x"},
	}
	for _, c := range cases {
		if got := StripFragment(c.in); got != c.want {
			t.Errorf("StripFragment(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsAllowedURLIgnoresFragmentContent(t *testing.T) {
	// A malicious fragment must not affect the allow-list decision either way.
	if !IsAllowedURL("https://youtube.com/watch?v=x#__youtubedl_smuggle={}") {
		t.Fatal("host is allowed regardless of fragment contents")
	}
}

func TestParseETASeconds(t *testing.T) {
	cases := []struct {
		in   string
		want float64
		ok   bool
	}{
		{"00:12", 12, true},
		{"01:23", 83, true},
		{"01:23:45", 5025, true},
		{"Unknown", 0, false},
		{"", 0, false},
		{"12", 0, false},
	}
	for _, c := range cases {
		got, ok := parseETASeconds(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseETASeconds(%q) = (%v, %v), want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFormatETASeconds(t *testing.T) {
	cases := []struct {
		in   float64
		want string
	}{
		{12, "00:12"},
		{83, "01:23"},
		{5025, "1:23:45"},
		{-5, "00:00"},
	}
	for _, c := range cases {
		if got := formatETASeconds(c.in); got != c.want {
			t.Errorf("formatETASeconds(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Regression for the reported ETA jitter (3 -> 4 -> 5 -> 3 while
// downloading): a single wildly different sample must not fully propagate
// into the displayed value, and passthrough must apply to unparsable
// values like "Unknown" without disturbing the running average.
func TestETASmootherDampensJitter(t *testing.T) {
	var s ETASmoother

	first := s.Smooth("00:10")
	if first != "00:10" {
		t.Fatalf("first sample should pass through unsmoothed, got %q", first)
	}

	// A single noisy spike to 30s shouldn't make the displayed value jump
	// anywhere near 30 — it should move only partway there.
	smoothedRaw, _ := parseETASeconds(s.Smooth("00:30"))
	if smoothedRaw >= 30 || smoothedRaw <= 10 {
		t.Fatalf("expected damped value strictly between 10 and 30, got %v", smoothedRaw)
	}

	// "Unknown" must pass through without corrupting the running average.
	if got := s.Smooth("Unknown"); got != "Unknown" {
		t.Fatalf("unparsable sample should pass through unchanged, got %q", got)
	}

	// A sustained new trend should still be reflected within a handful of
	// samples, not suppressed forever.
	var last float64
	for i := 0; i < 20; i++ {
		last, _ = parseETASeconds(s.Smooth("01:00"))
	}
	if math.Abs(last-60) > 1 {
		t.Fatalf("expected convergence to sustained trend (~60s), got %v", last)
	}
}

// Regression: this is the exact yt-dlp stderr text from the reported bug
// (an age-gated/sensitive tweet yt-dlp can't see without cookies).
func TestFriendlyErrorMatchesKnownPatterns(t *testing.T) {
	cases := []struct {
		raw      string
		wantSame bool // true: FriendlyError should return the generic fallback
	}{
		{"ERROR: [twitter] 2102468391982539088: No video could be found in this tweet", false},
		{"ERROR: [youtube] abc123: Sign in to confirm your age. This video may be inappropriate for some users.", false},
		{"ERROR: [youtube] abc123: Private video. Sign in if you've been granted access to this video", false},
		{"ERROR: [youtube] abc123: Video unavailable", false},
		{"ERROR: [youtube] abc123: The uploader has not made this video available in your country", false},
		{"ERROR: unable to download video data: HTTP Error 410: Gone", false},
		{"ERROR: [download] Got error: HTTP Error 410: Gone. Giving up after 10 retries\nERROR: fragment 1 not found, unable to continue", false},
		{"exit status 1", true},
		{"", true},
	}
	for _, c := range cases {
		got := FriendlyError(c.raw)
		isGeneric := got == genericProbeFailure
		if isGeneric != c.wantSame {
			t.Errorf("FriendlyError(%q) = %q (generic=%v), want generic=%v", c.raw, got, isGeneric, c.wantSame)
		}
	}
}
