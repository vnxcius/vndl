// Package config loads runtime configuration from environment variables.
package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port               string
	AllowedOrigins     []string
	RateLimitRPS       float64
	RateLimitBurst     int
	MaxConcurrentYtDlp int
	ConcurrencyMaxWait time.Duration
	JobTTL             time.Duration
	YtDlpPath          string
	FfmpegPath         string
	LogFormat          string
	ProbeCacheTTL      time.Duration
	LogDir             string
	LogRetentionDays   int
}

func Load() Config {
	return Config{
		Port:               getEnv("PORT", "8080"),
		AllowedOrigins:     splitCSV(getEnv("ALLOWED_ORIGINS", "http://localhost:5173")),
		RateLimitRPS:       getEnvFloat("RATE_LIMIT_RPS", 1),
		RateLimitBurst:     getEnvInt("RATE_LIMIT_BURST", 5),
		MaxConcurrentYtDlp: getEnvInt("MAX_CONCURRENT_YTDLP", 6), // server-wide, on top of per-IP limiting
		ConcurrencyMaxWait: getEnvDuration("CONCURRENCY_MAX_WAIT", 15*time.Second),
		JobTTL:             getEnvDuration("JOB_TTL", 5*time.Minute),
		YtDlpPath:          getEnv("YTDLP_PATH", "yt-dlp"),
		FfmpegPath:         getEnv("FFMPEG_PATH", ""), // empty = let yt-dlp find it on PATH
		LogFormat:          getEnv("LOG_FORMAT", "text"),
		ProbeCacheTTL:      getEnvDuration("PROBE_CACHE_TTL", 10*time.Minute),
		LogDir:             getEnv("LOG_DIR", ""), // empty disables file logging (stdout only)
		LogRetentionDays:   getEnvInt("LOG_RETENTION_DAYS", 30),
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getEnvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if i, err := strconv.Atoi(v); err == nil {
			return i
		}
	}
	return fallback
}

func getEnvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
