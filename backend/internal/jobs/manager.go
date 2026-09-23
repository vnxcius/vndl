package jobs

import (
	"sync"
	"time"

	"vndl/internal/config"
)

type Manager struct {
	cfg  config.Config
	mu   sync.Mutex
	jobs map[string]*Job
}

func NewManager(cfg config.Config) *Manager {
	return &Manager{cfg: cfg, jobs: make(map[string]*Job)}
}

func (m *Manager) Create(url, formatID string, audioOnly bool, title, ext, container string) *Job {
	j := newJob(url, formatID, audioOnly, title, ext, container)
	m.mu.Lock()
	m.jobs[j.ID] = j
	m.mu.Unlock()
	// Bounds jobs whose /file is never requested; a second ExpireAfter call
	// from file() is harmless (deleting an already-missing key is a no-op).
	m.ExpireAfter(j.ID, m.cfg.JobTTL)
	return j
}

func (m *Manager) Get(id string) (*Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[id]
	return j, ok
}

func (m *Manager) ExpireAfter(id string, d time.Duration) {
	time.AfterFunc(d, func() {
		m.mu.Lock()
		delete(m.jobs, id)
		m.mu.Unlock()
	})
}

func (m *Manager) TTL() time.Duration {
	return m.cfg.JobTTL
}
