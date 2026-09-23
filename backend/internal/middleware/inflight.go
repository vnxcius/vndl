package middleware

import "sync"

// InFlightLimiter caps concurrent work per key (client). max <= 0 disables it.
type InFlightLimiter struct {
	max int

	mu sync.Mutex
	n  map[string]int
}

func NewInFlightLimiter(max int) *InFlightLimiter {
	return &InFlightLimiter{max: max, n: make(map[string]int)}
}

func (l *InFlightLimiter) Acquire(key string) (release func(), ok bool) {
	if l.max <= 0 {
		return func() {}, true
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.n[key] >= l.max {
		return nil, false
	}
	l.n[key]++
	var once sync.Once
	return func() {
		once.Do(func() {
			l.mu.Lock()
			defer l.mu.Unlock()
			if l.n[key]--; l.n[key] <= 0 {
				delete(l.n, key)
			}
		})
	}, true
}
