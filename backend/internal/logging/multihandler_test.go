package logging

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

var errBoom = errors.New("boom")

func TestMultiHandlerFansOutToAllHandlers(t *testing.T) {
	var buf1, buf2 bytes.Buffer
	h := NewMultiHandler(slog.NewJSONHandler(&buf1, nil), slog.NewJSONHandler(&buf2, nil))
	slog.New(h).Info("hello", "key", "value")

	if !strings.Contains(buf1.String(), "hello") || !strings.Contains(buf1.String(), "value") {
		t.Errorf("first handler didn't receive the record: %q", buf1.String())
	}
	if !strings.Contains(buf2.String(), "hello") || !strings.Contains(buf2.String(), "value") {
		t.Errorf("second handler didn't receive the record: %q", buf2.String())
	}
}

// A failing handler (e.g. a broken file sink) must never prevent the
// other handlers from receiving the record.
type erroringHandler struct{}

func (erroringHandler) Enabled(context.Context, slog.Level) bool  { return true }
func (erroringHandler) Handle(context.Context, slog.Record) error { return errBoom }
func (erroringHandler) WithAttrs([]slog.Attr) slog.Handler        { return erroringHandler{} }
func (erroringHandler) WithGroup(string) slog.Handler             { return erroringHandler{} }

func TestMultiHandlerToleratesOneHandlerFailing(t *testing.T) {
	var buf bytes.Buffer
	h := NewMultiHandler(erroringHandler{}, slog.NewJSONHandler(&buf, nil))
	slog.New(h).Info("still works")

	if !strings.Contains(buf.String(), "still works") {
		t.Errorf("working handler should still receive the record, got: %q", buf.String())
	}
}
