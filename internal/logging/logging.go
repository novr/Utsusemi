package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const agentLogFile = "agent.log"

var sensitivePatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization:\s*bearer\s+)[^\s]+`),
	regexp.MustCompile(`(?i)(ghp_[A-Za-z0-9_]+)`),
	regexp.MustCompile(`(?i)(gho_[A-Za-z0-9_]+)`),
	regexp.MustCompile(`(?i)(github_pat_[A-Za-z0-9_]+)`),
	regexp.MustCompile(`(?i)(eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+)`),
	regexp.MustCompile(`(?i)("refresh_token"\s*:\s*")[^"]+`),
	regexp.MustCompile(`(?i)("host_jwt"\s*:\s*")[^"]+`),
}

type Options struct {
	LogFile string
}

type redactingHandler struct {
	inner slog.Handler
}

type fanoutHandler struct {
	handlers []slog.Handler
}

func New(opts Options) (*slog.Logger, error) {
	logPath := strings.TrimSpace(opts.LogFile)
	fileLogEnabled = logPath != ""
	handlerOpts := &slog.HandlerOptions{Level: slog.LevelInfo}

	handlers := make([]slog.Handler, 0, 2)
	if isTerminal(os.Stdout) {
		handlers = append(handlers, newHumanHandler(os.Stdout, slog.LevelInfo))
	} else {
		handlers = append(handlers, slog.NewJSONHandler(os.Stdout, handlerOpts))
	}

	if fileLogEnabled {
		file, err := openLogFile(logPath)
		if err != nil {
			fileLogEnabled = false
			return nil, fmt.Errorf("open log file %s: %w", logPath, err)
		}
		handlers = append(handlers, slog.NewJSONHandler(file, handlerOpts))
	}

	inner := fanoutHandler{handlers: handlers}
	logger := slog.New(redactingHandler{inner: inner})
	subprocessLogger = logger
	slog.SetDefault(logger)
	return logger, nil
}

func ResolveLogFile(flagValue, stateDir string) (string, error) {
	flagValue = strings.TrimSpace(flagValue)
	if flagValue != "" && flagValue != "-" {
		return flagValue, nil
	}
	path := AgentLogPath(stateDir)
	if path == "" {
		return "", fmt.Errorf("state_dir is empty; set state_dir in config or pass --log /path/to.log")
	}
	return path, nil
}

func AgentLogPath(stateDir string) string {
	if strings.TrimSpace(stateDir) == "" {
		return ""
	}
	return filepath.Join(stateDir, agentLogFile)
}

func openLogFile(path string) (io.Writer, error) {
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	if info, err := os.Stat(path); err == nil && info.IsDir() {
		return nil, fmt.Errorf("%s is a directory", path)
	}
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

func (h fanoutHandler) Enabled(ctx context.Context, level slog.Level) bool {
	for _, handler := range h.handlers {
		if handler.Enabled(ctx, level) {
			return true
		}
	}
	return false
}

func (h fanoutHandler) Handle(ctx context.Context, record slog.Record) error {
	for _, handler := range h.handlers {
		if err := handler.Handle(ctx, record.Clone()); err != nil {
			return err
		}
	}
	return nil
}

func (h fanoutHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	out := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		out[i] = handler.WithAttrs(attrs)
	}
	return fanoutHandler{handlers: out}
}

func (h fanoutHandler) WithGroup(name string) slog.Handler {
	out := make([]slog.Handler, len(h.handlers))
	for i, handler := range h.handlers {
		out[i] = handler.WithGroup(name)
	}
	return fanoutHandler{handlers: out}
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
