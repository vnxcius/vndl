package downloader

import (
	"testing"
	"time"
)

func TestProbeCacheHitAndMiss(t *testing.T) {
	c := NewProbeCache(time.Minute)

	if _, ok := c.Get("https://youtube.com/watch?v=x"); ok {
		t.Fatal("expected a miss before anything is cached")
	}

	meta := &Metadata{Title: "hello"}
	c.Set("https://youtube.com/watch?v=x", meta)

	got, ok := c.Get("https://youtube.com/watch?v=x")
	if !ok {
		t.Fatal("expected a hit for a URL just cached")
	}
	if got != meta {
		t.Fatalf("expected the exact cached Metadata pointer back, got %+v", got)
	}

	if _, ok := c.Get("https://youtube.com/watch?v=y"); ok {
		t.Fatal("a different URL must not hit another URL's cache entry")
	}
}

func TestProbeCacheExpiresAfterTTL(t *testing.T) {
	c := NewProbeCache(20 * time.Millisecond)
	c.Set("https://youtube.com/watch?v=x", &Metadata{Title: "hello"})

	if _, ok := c.Get("https://youtube.com/watch?v=x"); !ok {
		t.Fatal("expected a hit immediately after caching")
	}

	time.Sleep(50 * time.Millisecond)

	if _, ok := c.Get("https://youtube.com/watch?v=x"); ok {
		t.Fatal("expected the entry to have expired after its TTL")
	}
}

// A ttl of zero must disable caching entirely — Set becoming a no-op, Get
// always missing — rather than every caller needing to special-case it.
func TestProbeCacheDisabledWhenTTLIsZero(t *testing.T) {
	c := NewProbeCache(0)
	c.Set("https://youtube.com/watch?v=x", &Metadata{Title: "hello"})

	if _, ok := c.Get("https://youtube.com/watch?v=x"); ok {
		t.Fatal("caching must be a no-op when ttl is 0")
	}
}
