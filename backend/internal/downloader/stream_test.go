package downloader

import "testing"

func TestSplitMergeFormat(t *testing.T) {
	cases := []struct {
		in, video, audio string
		ok               bool
	}{
		{"137+bestaudio[ext=m4a]/137+bestaudio/best", "137", "bestaudio[ext=m4a]/bestaudio", true},
		{"248+bestaudio/best", "248", "bestaudio", true},
		{"bestvideo+bestaudio/best", "bestvideo", "bestaudio", true},
		{"18", "", "", false},
		{"best/137+140", "", "", false},
		{"+140", "", "", false},
	}
	for _, c := range cases {
		v, a, ok := SplitMergeFormat(c.in)
		if v != c.video || a != c.audio || ok != c.ok {
			t.Errorf("SplitMergeFormat(%q) = %q, %q, %v; want %q, %q, %v", c.in, v, a, ok, c.video, c.audio, c.ok)
		}
	}
}

func TestParseSize(t *testing.T) {
	cases := map[string]int64{"2G": 2 << 30, "500M": 500 << 20, "1.5k": 1536, "100": 100, "": 0, "abc": 0, "2GiB": 0}
	for in, want := range cases {
		if got := ParseSize(in); got != want {
			t.Errorf("ParseSize(%q) = %d, want %d", in, got, want)
		}
	}
}
