package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"vndl/internal/api"
	"vndl/internal/config"
	"vndl/internal/downloader"
	"vndl/internal/jobs"
	"vndl/internal/logging"
	"vndl/internal/middleware"
)

const banner = `
░█░█░█▀█░█▀▄░█░░░░░█▀█░█▀█░▀█▀
░▀▄▀░█░█░█░█░█░░░░░█▀█░█▀▀░░█░
░░▀░░▀░▀░▀▀░░▀▀▀░░░▀░▀░▀░░░▀▀▀`

func main() {
	fmt.Println(banner)

	cfg := config.Load()

	var logHandler slog.Handler
	if cfg.LogFormat == "json" {
		logHandler = slog.NewJSONHandler(os.Stdout, nil)
	} else {
		logHandler = logging.NewTextHandler(os.Stdout)
	}
	slog.SetDefault(slog.New(logHandler))

	if cfg.LogDir != "" {
		rf := logging.NewRotatingFile(cfg.LogDir, cfg.LogRetentionDays)
		if err := rf.EnsureWritable(); err != nil {
			slog.Warn("file logging unavailable, continuing with stdout only",
				"log_dir", cfg.LogDir, "error", err.Error(),
				"hint", "if LOG_DIR is a bind mount (see docker-compose.yml), its host directory must be writable by this container's user")
		}
		fileHandler := slog.NewJSONHandler(rf, nil) // plain JSON — meant for grepping, not a terminal
		slog.SetDefault(slog.New(logging.NewMultiHandler(logHandler, fileHandler)))
	}

	checkDependencies(cfg)
	removeStaleScratchDirs()

	mgr := jobs.NewManager(cfg)
	rl := middleware.NewRateLimiter(cfg.RateLimitRPS, cfg.RateLimitBurst)
	cl := middleware.NewConcurrencyLimiter(cfg.MaxConcurrentYtDlp, cfg.ConcurrencyMaxWait)
	probes := downloader.NewProbeCache(cfg.ProbeCacheTTL)
	srv := api.NewServer(cfg, mgr, rl, cl, probes)

	handler := middleware.CORS(cfg.AllowedOrigins)(srv.Routes())

	// No WriteTimeout — /file legitimately holds the response open for as
	// long as the download takes.
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	slog.Info("vndl backend listening", "port", cfg.Port)
	if err := server.ListenAndServe(); err != nil {
		slog.Error("server stopped", "err", err)
		os.Exit(1)
	}
}

// removeStaleScratchDirs clears job dirs a crash or kill left behind; the
// per-request cleanup never ran for them.
func removeStaleScratchDirs() {
	dirs, _ := filepath.Glob(filepath.Join(os.TempDir(), "vndl-job-*"))
	for _, d := range dirs {
		_ = os.RemoveAll(d)
	}
	if len(dirs) > 0 {
		slog.Info("removed stale job dirs", "count", len(dirs))
	}
}

// checkDependencies fails fast and loud if yt-dlp/ffmpeg aren't reachable.
func checkDependencies(cfg config.Config) {
	ytDlpPath, err := exec.LookPath(cfg.YtDlpPath)
	if err != nil {
		slog.Error("yt-dlp not found — the backend cannot download anything without it",
			"looked_for", cfg.YtDlpPath,
			"hint", "run via `docker compose up` (yt-dlp is bundled in the image), or install it locally and ensure it's on PATH")
		os.Exit(1)
	}

	ffmpegBin := cfg.FfmpegPath
	if ffmpegBin == "" {
		ffmpegBin = "ffmpeg"
	}
	if _, err := exec.LookPath(ffmpegBin); err != nil {
		slog.Error("ffmpeg not found — audio extraction and video+audio merging will fail",
			"looked_for", ffmpegBin,
			"hint", "run via `docker compose up` (ffmpeg is bundled in the image), or install it locally and ensure it's on PATH")
		os.Exit(1)
	}

	slog.Info("dependency check ok", "yt_dlp", ytDlpPath, "ffmpeg", ffmpegBin)
}
