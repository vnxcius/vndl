package downloader

import (
	"sync"
	"time"
)

// ProbeCache avoids re-running yt-dlp for repeated identical probes.
// In-memory only, self-expiring. Only successful probes are cached — a
// failure might be transient, and caching it would repeat it for minutes.
type ProbeCache struct {
	ttl time.Duration

	mu    sync.Mutex
	items map[string]probeCacheEntry
}

type probeCacheEntry struct {
	meta     *Metadata
	cachedAt time.Time
}

// NewProbeCache with ttl <= 0 disables caching (Get always misses).
func NewProbeCache(ttl time.Duration) *ProbeCache {
	c := &ProbeCache{ttl: ttl, items: make(map[string]probeCacheEntry)}
	if ttl > 0 {
		go c.sweepLoop()
	}
	return c
}

func (c *ProbeCache) sweepLoop() {
	ticker := time.NewTicker(time.Minute)
	for range ticker.C {
		cutoff := time.Now().Add(-c.ttl)
		c.mu.Lock()
		for url, e := range c.items {
			if e.cachedAt.Before(cutoff) {
				delete(c.items, url)
			}
		}
		c.mu.Unlock()
	}
}

// Get's returned Metadata is shared and must not be mutated.
func (c *ProbeCache) Get(url string) (*Metadata, bool) {
	if c.ttl <= 0 {
		return nil, false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[url]
	if !ok || time.Since(e.cachedAt) > c.ttl {
		return nil, false
	}
	return e.meta, true
}

func (c *ProbeCache) Set(url string, meta *Metadata) {
	if c.ttl <= 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.items[url] = probeCacheEntry{meta: meta, cachedAt: time.Now()}
}
