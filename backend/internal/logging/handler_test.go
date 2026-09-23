package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestTextHandlerColorsStatusByValue(t *testing.T) {
	cases := []struct {
		status string
		code   string
	}{
		{"ok", ansiGreen},
		{"error", ansiRed},
		{"canceled", ansiYellow},
	}
	for _, c := range cases {
		var buf bytes.Buffer
		h := NewTextHandler(&buf)
		l := slog.New(h)
		l.Info("request.completed", "status", c.status)

		out := buf.String()
		want := c.code + c.status + ansiReset
		if !strings.Contains(out, want) {
			t.Errorf("status=%s: expected colored %q in output, got: %q", c.status, want, out)
		}
	}
}

func TestTextHandlerColorsErrorField(t *testing.T) {
	var buf bytes.Buffer
	h := NewTextHandler(&buf)
	slog.New(h).Error("request.completed", "error", "boom")

	out := buf.String()
	if !strings.Contains(out, ansiRed+"boom"+ansiReset) {
		t.Errorf("expected error field colored red, got: %q", out)
	}
}

func TestTextHandlerQuotesValuesWithSpaces(t *testing.T) {
	var buf bytes.Buffer
	h := NewTextHandler(&buf)
	slog.New(h).Info("msg", "hint", "run via docker compose")

	if !strings.Contains(buf.String(), `hint=`+ansiReset+`"run via docker compose"`) {
		t.Errorf("expected quoted value for a field containing spaces, got: %q", buf.String())
	}
}

func TestTextHandlerRespectsNoColor(t *testing.T) {
	old := colorEnabled
	colorEnabled = false
	defer func() { colorEnabled = old }()

	var buf bytes.Buffer
	h := NewTextHandler(&buf)
	slog.New(h).Error("request.completed", "status", "error")

	out := buf.String()
	if strings.Contains(out, "\033[") {
		t.Errorf("expected no ANSI escape codes with colorEnabled=false, got: %q", out)
	}
	if !strings.Contains(out, "status=error") {
		t.Errorf("expected plain status=error text, got: %q", out)
	}
}

func TestTextHandlerWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := NewTextHandler(&buf).WithAttrs([]slog.Attr{slog.String("service", "vndl")})
	slog.New(h).Info("msg")

	if want := ansiDim + "service=" + ansiReset + "vndl"; !strings.Contains(buf.String(), want) {
		t.Errorf("expected inherited attr from WithAttrs, got: %q", buf.String())
	}
}

func TestTextHandlerWithGroupPrefixesRecordAttrs(t *testing.T) {
	var buf bytes.Buffer
	h := NewTextHandler(&buf).WithGroup("req")
	slog.New(h).Log(context.Background(), slog.LevelInfo, "msg", slog.String("id", "abc"))

	if want := ansiDim + "req.id=" + ansiReset + "abc"; !strings.Contains(buf.String(), want) {
		t.Errorf("expected group-prefixed key, got: %q", buf.String())
	}
}
