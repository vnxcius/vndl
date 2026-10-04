package jobs

import (
	"sync"
	"time"

	"vndl/internal/config"
)

type Manager struct {
	cfg    config.Config
	mu     sync.Mutex
	jobs   map[string]*Job
	timers map[string]*time.Timer
}

func NewManager(cfg config.Config) *Manager {
	return &Manager{cfg: cfg, jobs: make(map[string]*Job), timers: make(map[string]*time.Timer)}
}

func (m *Manager) Create(url, formatID string, audioOnly bool, title, ext, container string) *Job {
	j := newJob(url, formatID, audioOnly, title, ext, container)
	m.mu.Lock()
	m.jobs[j.ID] = j
	m.mu.Unlock()
	// Bounds jobs that never start; /file calls Hold while streaming, then
	// ExpireAfter again once it ends.
	m.ExpireAfter(j.ID, m.cfg.JobTTL)
	return j
}

func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

// ExpireAfter replaces any earlier expiry for id.
func (m *Manager) ExpireAfter(id string, d time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[id]; !ok {
		return
	}
	if t := m.timers[id]; t != nil {
		t.Stop()
	}
	// t is read by expire only under m.mu, which is held until it's set.
	var t *time.Timer
	t = time.AfterFunc(d, func() { m.expire(id, &t) })
	m.timers[id] = t
}

// Hold cancels id's pending expiry, for a job still running past its TTL.
func (m *Manager) Hold(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if t := m.timers[id]; t != nil {
		t.Stop()
		delete(m.timers, id)
	}
}

// expire ignores a timer that was replaced or held after it already fired.
func (m *Manager) expire(id string, t **time.Timer) {
	m.mu.Lock()
	if m.timers[id] != *t {
		m.mu.Unlock()
		return
	}
	delete(m.jobs, id)
	delete(m.timers, id)
	m.mu.Unlock()
}

func (m *Manager) TTL() time.Duration {
	return m.cfg.JobTTL
}
