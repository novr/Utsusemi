package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
)

type humanHandler struct {
	w     io.Writer
	mu    sync.Mutex
	level slog.Level
	attrs []slog.Attr
}

func newHumanHandler(w io.Writer, level slog.Level) *humanHandler {
	return &humanHandler{w: w, level: level}
}

func (h *humanHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.level
}

func (h *humanHandler) Handle(_ context.Context, r slog.Record) error {
	var buf strings.Builder
	buf.WriteString(r.Time.Format("15:04:05"))
	buf.WriteByte(' ')
	buf.WriteString(levelLabel(r.Level))
	buf.WriteByte(' ')
	buf.WriteString(r.Message)

	attrs := append(append([]slog.Attr{}, h.attrs...), collectAttrs(r)...)
	for _, attr := range attrs {
		buf.WriteByte(' ')
		buf.WriteString(formatAttr(attr))
	}
	buf.WriteByte('\n')

	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := io.WriteString(h.w, buf.String())
	return err
}

func (h *humanHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	h2 := *h
	h2.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &h2
}

func (h *humanHandler) WithGroup(string) slog.Handler {
	return h
}

func collectAttrs(r slog.Record) []slog.Attr {
	attrs := make([]slog.Attr, 0, r.NumAttrs())
	r.Attrs(func(attr slog.Attr) bool {
		attrs = append(attrs, attr)
		return true
	})
	return attrs
}

func levelLabel(level slog.Level) string {
	switch {
	case level >= slog.LevelError:
		return "ERROR"
	case level >= slog.LevelWarn:
		return "WARN "
	case level >= slog.LevelInfo:
		return "INFO "
	default:
		return "DEBUG"
	}
}

func formatAttr(attr slog.Attr) string {
	if attr.Equal(slog.Attr{}) {
		return ""
	}
	attr.Value = attr.Value.Resolve()
	if attr.Value.Kind() == slog.KindString {
		s := attr.Value.String()
		if strings.ContainsAny(s, " \t[]") {
			return fmt.Sprintf("%s=%q", attr.Key, s)
		}
		return fmt.Sprintf("%s=%s", attr.Key, s)
	}
	if s := attr.Value.String(); s != "" {
		if strings.ContainsAny(s, " \t[]") {
			return fmt.Sprintf("%s=%q", attr.Key, s)
		}
		return fmt.Sprintf("%s=%s", attr.Key, s)
	}
	return fmt.Sprintf("%s=%v", attr.Key, attr.Value.Any())
}
