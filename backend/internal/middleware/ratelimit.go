package middleware

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// bucket is a token-bucket per client IP; the map self-prunes idle entries.
type bucket struct {
	tokens   float64
	lastSeen time.Time
}

type RateLimiter struct {
	rps   float64
	burst int

	mu      sync.Mutex
	buckets map[string]*bucket
}

func NewRateLimiter(rps float64, burst int) *RateLimiter {
	rl := &RateLimiter{rps: rps, burst: burst, buckets: make(map[string]*bucket)}
	go rl.sweepLoop()
	return rl
}

func (rl *RateLimiter) sweepLoop() {
	ticker := time.NewTicker(10 * time.Minute)
	for range ticker.C {
		cutoff := time.Now().Add(-10 * time.Minute)
		rl.mu.Lock()
		for ip, b := range rl.buckets {
			if b.lastSeen.Before(cutoff) {
				delete(rl.buckets, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	b, ok := rl.buckets[ip]
	now := time.Now()
	if !ok {
		b = &bucket{tokens: float64(rl.burst - 1), lastSeen: now}
		rl.buckets[ip] = b
		return true
	}

	elapsed := now.Sub(b.lastSeen).Seconds()
	b.tokens += elapsed * rl.rps
	if b.tokens > float64(rl.burst) {
		b.tokens = float64(rl.burst)
	}
	b.lastSeen = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ClientIP(r)
		if !rl.allow(ip) {
			http.Error(w, "rate limit exceeded, slow down", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// CF-Connecting-IP (set by Cloudflare's edge) is trusted first since
// cloudflared sits in front of Caddy now; X-Forwarded-For's rightmost entry
// is the fallback for access that doesn't go through the tunnel. Both are
// only safe to trust because the backend port is never reachable directly.
func ClientIP(r *http.Request) string {
	if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
		return cfIP
	}
	if fwd := r.Header.Get("X-Forwarded-For"); fwd != "" {
		parts := strings.Split(fwd, ",")
		last := strings.TrimSpace(parts[len(parts)-1])
		if last != "" {
			return last
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
