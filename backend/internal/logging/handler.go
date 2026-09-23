package logging

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
)

// Gated on NO_COLOR (https://no-color.org), not isatty — stdout here is a
// pipe to the container runtime, not a TTY, so isatty would always disable it.
var colorEnabled = os.Getenv("NO_COLOR") == ""

const (
	ansiReset  = "\033[0m"
	ansiDim    = "\033[2m"
	ansiBold   = "\033[1m"
	ansiRed    = "\033[31m"
	ansiGreen  = "\033[32m"
	ansiYellow = "\033[33m"
	ansiCyan   = "\033[36m"
	ansiGray   = "\033[90m"
)

func color(code, s string) string {
	if !colorEnabled || s == "" {
		return s
	}
	return code + s + ansiReset
}

// TextHandler renders each record as one human-readable, color-accented
// line. Set LOG_FORMAT=json for slog's plain structured output instead.
type TextHandler struct {
	mu    *sync.Mutex
	w     io.Writer
	attrs []slog.Attr
	group string
}

func NewTextHandler(w io.Writer) *TextHandler {
	return &TextHandler{mu: &sync.Mutex{}, w: w}
}

func (h *TextHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *TextHandler) Handle(_ context.Context, r slog.Record) error {
	var buf bytes.Buffer

	buf.WriteString(color(ansiGray, r.Time.Format("15:04:05.000")))
	buf.WriteByte(' ')
	buf.WriteString(levelTag(r.Level))
	buf.WriteByte(' ')
	buf.WriteString(color(messageColor(r.Level), r.Message))

	fields := make(map[string]string, r.NumAttrs()+len(h.attrs))
	keys := make([]string, 0, r.NumAttrs()+len(h.attrs))
	add := func(a slog.Attr) bool {
		key := a.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		if _, exists := fields[key]; !exists {
			keys = append(keys, key)
		}
		fields[key] = formatValue(key, a.Value.Resolve())
		return true
	}
	for _, a := range h.attrs {
		add(a)
	}
	r.Attrs(add)

	sort.Strings(keys)
	for _, k := range keys {
		buf.WriteByte(' ')
		buf.WriteString(color(ansiDim, k+"="))
		buf.WriteString(fields[k])
	}
	buf.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.w.Write(buf.Bytes())
	return err
}

func (h *TextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	na := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	na = append(na, h.attrs...)
	na = append(na, attrs...)
	return &TextHandler{mu: h.mu, w: h.w, attrs: na, group: h.group}
}

func (h *TextHandler) WithGroup(name string) slog.Handler {
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &TextHandler{mu: h.mu, w: h.w, attrs: h.attrs, group: g}
}

func levelTag(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return color(ansiBold+ansiRed, "ERR")
	case l >= slog.LevelWarn:
		return color(ansiYellow, "WRN")
	case l >= slog.LevelInfo:
		return color(ansiCyan, "INF")
	default:
		return color(ansiGray, "DBG")
	}
}

func messageColor(l slog.Level) string {
	switch {
	case l >= slog.LevelError:
		return ansiBold + ansiRed
	case l >= slog.LevelWarn:
		return ansiBold + ansiYellow
	default:
		return ansiBold
	}
}

func formatValue(key string, v slog.Value) string {
	s := v.String()
	switch key {
	case "status":
		switch s {
		case "ok":
			return color(ansiGreen, s)
		case "error":
			return color(ansiRed, s)
		case "canceled":
			return color(ansiYellow, s)
		}
	case "error", "err":
		return color(ansiRed, s)
	case "client_disconnected":
		if s == "true" {
			return color(ansiYellow, s)
		}
	}
	if strings.ContainsAny(s, " \t\n") {
		return `"` + s + `"`
	}
	return s
}
