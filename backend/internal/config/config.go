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
	MaxJobDuration     time.Duration
	MaxFilesize        string
	MaxJobsPerIP       int
	MaxSSEConnections  int
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
		MaxJobDuration:     getEnvDuration("MAX_JOB_DURATION", 30*time.Minute), // 0 = no limit
		MaxFilesize:        getEnv("MAX_FILESIZE", "2G"),                       // yt-dlp --max-filesize syntax
		MaxJobsPerIP:       getEnvInt("MAX_JOBS_PER_IP", 2),                    // concurrent /file per client; 0 = no limit
		MaxSSEConnections:  getEnvInt("MAX_SSE_CONNECTIONS", 512),              // server-wide; 0 = no limit
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

// LogArgs lists the effective settings under their env var names, for the
// startup log. Nothing here is secret.
func (c Config) LogArgs() []any {
	return []any{
		"PORT", c.Port,
		"ALLOWED_ORIGINS", strings.Join(c.AllowedOrigins, ","),
		"RATE_LIMIT_RPS", c.RateLimitRPS,
		"RATE_LIMIT_BURST", c.RateLimitBurst,
		"MAX_CONCURRENT_YTDLP", c.MaxConcurrentYtDlp,
		"CONCURRENCY_MAX_WAIT", c.ConcurrencyMaxWait.String(),
		"MAX_JOBS_PER_IP", c.MaxJobsPerIP,
		"MAX_JOB_DURATION", c.MaxJobDuration.String(),
		"MAX_FILESIZE", c.MaxFilesize,
		"MAX_SSE_CONNECTIONS", c.MaxSSEConnections,
		"JOB_TTL", c.JobTTL.String(),
		"PROBE_CACHE_TTL", c.ProbeCacheTTL.String(),
		"YTDLP_PATH", c.YtDlpPath,
		"FFMPEG_PATH", c.FfmpegPath,
		"LOG_FORMAT", c.LogFormat,
		"LOG_DIR", c.LogDir,
		"LOG_RETENTION_DAYS", c.LogRetentionDays,
	}
}
