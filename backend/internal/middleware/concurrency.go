package middleware

import (
	"context"
	"net/http"
	"time"
)

// ConcurrencyLimiter bounds how many requests are inside the wrapped
// handler at once, server-wide, independent of per-IP rate limiting. A
// request over the cap waits briefly for a slot rather than failing
// instantly, so a small legitimate burst smooths out instead of erroring.
type ConcurrencyLimiter struct {
	slots   chan struct{}
	maxWait time.Duration
}

func NewConcurrencyLimiter(max int, maxWait time.Duration) *ConcurrencyLimiter {
	return &ConcurrencyLimiter{slots: make(chan struct{}, max), maxWait: maxWait}
}

// Acquire's release func should be called as soon as the expensive work is
// done, not after any subsequent response streaming.
func (c *ConcurrencyLimiter) Acquire(ctx context.Context) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, c.maxWait)
	defer cancel()

	select {
	case c.slots <- struct{}{}:
		return func() { <-c.slots }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *ConcurrencyLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release, err := c.Acquire(r.Context())
		if err != nil {
			http.Error(w, "server is at capacity, try again shortly", http.StatusServiceUnavailable)
			return
		}
		defer release()
		next.ServeHTTP(w, r)
	})
}
