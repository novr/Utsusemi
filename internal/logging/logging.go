package logging

import (
	"context"
	"log/slog"
	"os"
	"regexp"
	"strings"
)

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[^\s]+`),
	regexp.MustCompile(`(?i)(ghp_[A-Za-z0-9_]+)`),
	regexp.MustCompile(`(?i)(gho_[A-Za-z0-9_]+)`),
	regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]+)`),
	regexp.MustCompile(`(?i)(eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)`),
	regexp.MustCompile(`(?i)("refresh_token"\s*:\s*")[^"]+`),
	regexp.MustCompile(`(?i)("host_jwt"\s*:\s*")[^"]+`),
}

type redactingHandler struct {
	inner slog.Handler
}

func New() *slog.Logger {
	opts := &slog.HandlerOptions{Level: slog.LevelInfo}
	var inner slog.Handler
	if isTerminal(os.Stdout) {
		inner = newHumanHandler(os.Stdout, slog.LevelInfo)
	} else {
		inner = slog.NewJSONHandler(os.Stdout, opts)
	}
	logger := slog.New(redactingHandler{inner: inner})
	subprocessLogger = logger
	return logger
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (h redactingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h redactingHandler) Handle(ctx context.Context, record slog.Record) error {
	record = record.Clone()
	record.Message = redact(record.Message)
	attrs := make([]slog.Attr, 0, record.NumAttrs())
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Value.Kind() == slog.KindString {
			attr.Value = slog.StringValue(redact(attr.Value.String()))
		}
		attrs = append(attrs, attr)
		return true
	})
	record = slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.AddAttrs(attrs...)
	return h.inner.Handle(ctx, record)
}

func (h redactingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return redactingHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h redactingHandler) WithGroup(name string) slog.Handler {
	return redactingHandler{inner: h.inner.WithGroup(name)}
}

func redact(value string) string {
	out := value
	for _, pattern := range sensitivePatterns {
		out = pattern.ReplaceAllString(out, "${1}[REDACTED]")
	}
	if strings.Contains(out, "ghp_") || strings.Contains(out, "github_pat_") || strings.Contains(out, "eyJ") || strings.Contains(out, "refresh_token") {
		return "[REDACTED]"
	}
	return out
}
