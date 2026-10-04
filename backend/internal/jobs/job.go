// Package jobs holds in-memory state for in-flight downloads: one Job per
// request, addressed by a random ID, fanning progress out to SSE subscribers.
package jobs

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
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

	mu         sync.Mutex
	status     Status
	errMsg     string // user-facing
	canceled   bool
	cancelFn   context.CancelFunc
	scratch    string // owned by the job once Complete succeeds; removed on expiry
	resultPath string
	resultExt  string

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

// Complete hands scratch (holding path) over to the job, to be removed when
// it expires. It reports false, taking nothing over, if the job was canceled
// in the meantime.
func (j *Job) Complete(scratch, path, ext string) bool {
	j.mu.Lock()
	if j.canceled {
		j.mu.Unlock()
		return false
	}
	j.status = StatusCompleted
	j.scratch, j.resultPath, j.resultExt = scratch, path, ext
	j.mu.Unlock()
	j.Publish(downloader.ProgressEvent{Status: "done"})
	return true
}

// Result is the finished file, once the job has completed.
func (j *Job) Result() (path, ext string, ok bool) {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.resultPath, j.resultExt, j.status == StatusCompleted
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

func (j *Job) removeScratch() {
	j.mu.Lock()
	dir := j.scratch
	j.scratch = ""
	j.mu.Unlock()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
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
