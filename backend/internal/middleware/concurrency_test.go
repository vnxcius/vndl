package middleware

import (
	"context"
	"testing"
	"time"
)

// Regression for VNDL-001: the slot must be releasable independently of any
// caller-side response streaming — i.e. Acquire/release must be a plain
// acquire/release pair, not something that only unblocks when an entire
// wrapped HTTP handler returns.
func TestAcquireReleaseIndependentOfCaller(t *testing.T) {
	cl := NewConcurrencyLimiter(1, 200*time.Millisecond)

	release, err := cl.Acquire(context.Background())
	if err != nil {
		t.Fatalf("first acquire should succeed: %v", err)
	}

	// A second, immediate acquire should time out — the slot is genuinely held.
	if _, err := cl.Acquire(context.Background()); err == nil {
		t.Fatal("second acquire should have blocked/timed out while the slot is held")
	}

	release() // simulates cmd.Wait() returning, well before any response streaming

	if _, err := cl.Acquire(context.Background()); err != nil {
		t.Fatalf("acquire after release should succeed immediately: %v", err)
	}
}
