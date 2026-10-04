package downloader

import (
	"context"
	"os/exec"
	"strconv"
	"strings"

	"vndl/internal/config"
)

// SplitMergeFormat splits a selector that combines a video and an audio
// stream, e.g. "137+bestaudio[ext=m4a]/137+bestaudio/best", into one
// selector per stream ("137", "bestaudio[ext=m4a]/bestaudio"), so each can
// be piped into ffmpeg separately. Alternatives pairing a different video,
// or none, are dropped. ok is false if the first alternative isn't a merge.
func SplitMergeFormat(format string) (video, audio string, ok bool) {
	alts := strings.Split(format, "/")
	video, first, ok := strings.Cut(alts[0], "+")
	if !ok || video == "" || first == "" {
		return "", "", false
	}
	audios := []string{first}
	for _, alt := range alts[1:] {
		if v, a, ok := strings.Cut(alt, "+"); ok && v == video && a != "" {
			audios = append(audios, a)
		}
	}
	return video, strings.Join(audios, "/"), true
}

func ffmpegBin(cfg config.Config) string {
	if cfg.FfmpegPath != "" {
		return cfg.FfmpegPath
	}
	return "ffmpeg"
}

// BuildMuxCmd copies video from fd 3 and audio from fd 4 (the caller sets
// ExtraFiles) into one container on stdout, without re-encoding. The output
// is laid out for a pipe: MP4 as fragmented MP4, since a regular one needs
// to seek back and write its index once it's done.
func BuildMuxCmd(ctx context.Context, cfg config.Config, container string) *exec.Cmd {
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-i", "pipe:3", "-i", "pipe:4",
		"-map", "0:v:0", "-map", "1:a:0", "-c", "copy",
	}
	switch container {
	case "mp4":
		args = append(args, "-f", "mp4", "-movflags", "frag_keyframe+empty_moov+default_base_moof")
	case "webm":
		args = append(args, "-f", "webm")
	default:
		args = append(args, "-f", "matroska")
	}
	cmd := exec.CommandContext(ctx, ffmpegBin(cfg), append(args, "pipe:1")...)
	killGroupOnCancel(cmd)
	return cmd
}

// BuildMP3Cmd encodes input as mp3 on stdout, at the quality yt-dlp's
// --audio-format mp3 used by default.
func BuildMP3Cmd(ctx context.Context, cfg config.Config, input string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, ffmpegBin(cfg),
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-i", input, "-vn", "-c:a", "libmp3lame", "-q:a", "5", "-f", "mp3", "pipe:1")
	killGroupOnCancel(cmd)
	return cmd
}

// ParseSize reads a yt-dlp size like "2G", "500M" or "1.5g" (binary units)
// as bytes; 0 if empty or unparseable.
func ParseSize(s string) int64 {
	s = strings.TrimSpace(strings.ToUpper(strings.TrimSuffix(strings.TrimSuffix(s, "B"), "b")))
	mult := 1.0
	if n := len(s); n > 0 {
		switch s[n-1] {
		case 'K':
			mult = 1 << 10
		case 'M':
			mult = 1 << 20
		case 'G':
			mult = 1 << 30
		case 'T':
			mult = 1 << 40
		}
		if mult != 1 {
			s = s[:n-1]
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0
	}
	return int64(f * mult)
}
