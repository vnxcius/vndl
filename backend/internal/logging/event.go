// Package logging implements canonical log lines: one structured event per
// request. Errors and slow requests are always kept; fast successful
// requests are tail-sampled. See https://loggingsucks.com.
package logging

import (
	"context"
	"log/slog"
	"math/rand"
	"sort"
	"time"
)

// SampleRate is the fraction of fast, successful requests kept on top of
// the always-kept errors and slow requests.
var SampleRate = 0.05

// SlowThreshold is how long a request has to take before it's always kept.
var SlowThreshold = 3 * time.Second

type Event struct {
	fields map[string]any
	start  time.Time
}

func New() *Event {
	return &Event{fields: make(map[string]any, 16), start: time.Now()}
}

func (e *Event) Set(key string, val any) *Event {
	e.fields[key] = val
	return e
}

// Emit writes one line for this event. Call it once, when the request is done.
func (e *Event) Emit(ctx context.Context, logger *slog.Logger) {
	duration := time.Since(e.start)
	e.fields["duration_ms"] = duration.Milliseconds()

	isError := e.fields["status"] == "error"
	isCanceled := e.fields["status"] == "canceled"
	isSlow := duration > SlowThreshold
	if !isError && !isCanceled && !isSlow && rand.Float64() > SampleRate {
		return
	}

	keys := make([]string, 0, len(e.fields))
	for k := range e.fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	args := make([]any, 0, len(keys)*2)
	for _, k := range keys {
		args = append(args, k, e.fields[k])
	}

	level := slog.LevelInfo
	if isError {
		level = slog.LevelError
	}
	logger.Log(ctx, level, "request.completed", args...)
}
