// Package jobs holds in-memory state for in-flight downloads: one Job per
// request, addressed by a random ID, fanning progress out to SSE subscribers.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"

	"vndl/internal/downloader"
)

type Status string

const (
	StatusPending     Status = "pending"
	StatusDownloading Status = "downloading"
	StatusProcessing  Status = "processing"
	StatusCompleted   Status = "completed"
	StatusError       Status = "error"
	StatusCanceled    Status = "canceled"
)

type Job struct {
	ID        string
	URL       string
	FormatID  string
	AudioOnly bool
	Title     string
	Ext       string
	Container string
	CreatedAt time.Time

	mu        sync.Mutex
	status    Status
	errMsg    string // user-facing
	streaming bool
	canceled  bool
	cancelFn  context.CancelFunc

	subMu       sync.Mutex
	subscribers map[chan downloader.ProgressEvent]struct{}
}

func newJob(url, formatID string, audioOnly bool, title, ext, container string) *Job {
	return &Job{
		ID:          newID(),
		URL:         url,
		FormatID:    formatID,
		AudioOnly:   audioOnly,
		Title:       title,
		Ext:         ext,
		Container:   container,
		CreatedAt:   time.Now(),
		status:      StatusPending,
		subscribers: make(map[chan downloader.ProgressEvent]struct{}),
	}
}

func newID() string {
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Claim ensures only one caller streams this job's bytes at a time. A
// stream that failed can be claimed again — the browser's retry button
// requests the same URL — but a finished or canceled one can't.
func (j *Job) Claim() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.streaming || j.canceled || j.status == StatusCompleted {
		return false
	}
	j.streaming = true
	return true
}

func (j *Job) Unclaim() {
	j.mu.Lock()
	j.streaming = false
	j.mu.Unlock()
}

func (j *Job) Status() (Status, string) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.status, j.errMsg
}

func (j *Job) SetStatus(s Status) {
	j.mu.Lock()
	j.status = s
	j.mu.Unlock()
}

// Fail's msg is shown to the user as-is, so it must not carry raw yt-dlp
// output.
func (j *Job) Fail(msg string) {
	j.mu.Lock()
	j.status = StatusError
	j.errMsg = msg
	j.mu.Unlock()
	j.Publish(downloader.ProgressEvent{Status: "error", Error: msg})
}

// Done marks every byte as sent.
func (j *Job) Done() {
	j.SetStatus(StatusCompleted)
	j.Publish(downloader.ProgressEvent{Status: "done"})
}

// Event is the job's current state as a progress event: terminal states
// as-is, anything else as just its phase.
func (j *Job) Event() downloader.ProgressEvent {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch j.status {
	case StatusCompleted:
		return downloader.ProgressEvent{Status: "done"}
	case StatusError:
		return downloader.ProgressEvent{Status: "error", Error: j.errMsg}
	case StatusPending:
		return downloader.ProgressEvent{Status: "downloading"}
	}
	return downloader.ProgressEvent{Status: string(j.status)}
}

// SetCancelFunc runs fn immediately if Cancel was already called before the
// process started, instead of storing it, so the process never runs.
func (j *Job) SetCancelFunc(fn context.CancelFunc) {
	j.mu.Lock()
	alreadyCanceled := j.canceled
	if !alreadyCanceled {
		j.cancelFn = fn
	}
	j.mu.Unlock()

	if alreadyCanceled {
		fn()
	}
}

func (j *Job) Cancel() {
	j.mu.Lock()
	if j.canceled || j.status == StatusCompleted || j.status == StatusError {
		j.mu.Unlock()
		return
	}
	j.canceled = true
	j.status = StatusCanceled
	fn := j.cancelFn
	j.mu.Unlock()

	if fn != nil {
		fn()
	}
	j.Publish(downloader.ProgressEvent{Status: "canceled"})
}

func (j *Job) IsCanceled() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.canceled
}

const maxSubscribersPerJob = 8 // caps concurrent SSE connections per job

// Subscribe returns an unsubscribe func the caller must defer. ok is false
// if the job already has maxSubscribersPerJob active subscribers.
func (j *Job) Subscribe() (ch chan downloader.ProgressEvent, cancel func(), ok bool) {
	j.subMu.Lock()
	if len(j.subscribers) >= maxSubscribersPerJob {
		j.subMu.Unlock()
		return nil, nil, false
	}
	ch = make(chan downloader.ProgressEvent, 16)
	j.subscribers[ch] = struct{}{}
	j.subMu.Unlock()

	cancel = func() {
		j.subMu.Lock()
		if _, ok := j.subscribers[ch]; ok {
			delete(j.subscribers, ch)
			close(ch)
		}
		j.subMu.Unlock()
	}
	return ch, cancel, true
}

func (j *Job) Publish(ev downloader.ProgressEvent) {
	j.subMu.Lock()
	defer j.subMu.Unlock()
	for ch := range j.subscribers {
		select {
		case ch <- ev:
		default:
			// slow subscriber — drop the event rather than block the download
		}
	}
}
