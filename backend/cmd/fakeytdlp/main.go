// fakeytdlp stands in for the real yt-dlp binary during load testing,
// speaking just enough of its CLI surface to exercise the backend without
// any real network traffic.
package main

import (
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func main() {
	args := os.Args[1:]

	if hasFlag(args, "-j") {
		runProbe(args)
		return
	}
	runDownload(args)
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func flagValue(args []string, name string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func stepDelay() time.Duration {
	ms := envInt("FAKE_YTDLP_DELAY_MS", 40)
	return time.Duration(ms) * time.Millisecond
}

func fileSizeBytes() int64 {
	return int64(envInt("FAKE_YTDLP_FILE_BYTES", 2_000_000))
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func runProbe(args []string) {
	url := args[len(args)-1]
	title := fmt.Sprintf("fake video for %s", shortHash(url))
	fmt.Printf(`{"title":%q,"thumbnail":"https://example.invalid/thumb.jpg","duration":123.4,"uploader":"loadtest","formats":[`+
		`{"format_id":"137","ext":"mp4","resolution":"1920x1080","format_note":"1080p","vcodec":"avc1.640028","acodec":"none","filesize":52428800,"height":1080},`+
		`{"format_id":"140","ext":"m4a","resolution":"audio only","format_note":"","vcodec":"none","acodec":"mp4a.40.2","filesize":3355443,"height":0},`+
		`{"format_id":"18","ext":"mp4","resolution":"640x360","format_note":"360p","vcodec":"avc1.42001E","acodec":"mp4a.40.2","filesize":10485760,"height":360}`+
		`]}`+"\n", title)
}

func runDownload(args []string) {
	outTemplate := flagValue(args, "-o")
	audioOnly := hasFlag(args, "-x")
	container := flagValue(args, "--merge-output-format")
	formatID := flagValue(args, "-f")

	ext := "mp4"
	switch {
	case audioOnly:
		ext = "mp3"
	case container != "":
		ext = container
	}

	delay := stepDelay()
	total := fileSizeBytes()

	fmt.Println("[download] Destination: " + outTemplate)
	steps := []float64{0.0, 0.2, 0.7, 1.4, 2.9, 5.7, 11.5, 22.9, 45.8, 68.3, 89.8, 100.0}
	for _, pct := range steps {
		speed := 5 + rand.Intn(80)
		fmt.Printf("[download] %5.1f%% of %6.2fMiB at %3dMiB/s ETA 00:0%d\n",
			pct, float64(total)/1024/1024, speed, rand.Intn(9))
		time.Sleep(delay)
	}
	fmt.Printf("[download] 100%% of %.2fMiB in 00:00:01\n", float64(total)/1024/1024)

	if strings.Contains(formatID, "+") {
		fmt.Println("[Merger] Merging formats into fake output")
		time.Sleep(delay)
	} else if audioOnly {
		fmt.Println("[ExtractAudio] Destination: fake output")
		time.Sleep(delay)
	}

	outPath := strings.Replace(outTemplate, "%(ext)s", ext, 1)
	if err := os.MkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "fakeytdlp: mkdir:", err)
		os.Exit(1)
	}
	if err := writeFakeFile(outPath, total); err != nil {
		fmt.Fprintln(os.Stderr, "fakeytdlp: write:", err)
		os.Exit(1)
	}
}

func writeFakeFile(path string, size int64) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	buf := make([]byte, 64*1024)
	var written int64
	for written < size {
		n := len(buf)
		if remaining := size - written; remaining < int64(n) {
			n = int(remaining)
		}
		if _, err := f.Write(buf[:n]); err != nil {
			return err
		}
		written += int64(n)
	}
	return nil
}

func shortHash(s string) string {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return fmt.Sprintf("%08x", h)
}
