package provider

import (
	"context"
	"io"

	"github.com/novr/utsusemi/internal/logging"
)

type stdoutTapKey struct{}

// WithStdoutTap tees streaming command stdout/stderr to w for the duration of
// ctx. Each call site should pass its own writer; do not share across concurrent
// ExecStdin/RunStreaming invocations.
func WithStdoutTap(ctx context.Context, w io.Writer) context.Context {
	if w == nil {
		return ctx
	}
	return context.WithValue(ctx, stdoutTapKey{}, w)
}

func stdoutTapFrom(ctx context.Context) io.Writer {
	w, _ := ctx.Value(stdoutTapKey{}).(io.Writer)
	return w
}

// StdoutTap returns the writer installed by WithStdoutTap, if any.
func StdoutTap(ctx context.Context) io.Writer {
	return stdoutTapFrom(ctx)
}

// teeFlushWriter writes to primary and tap, and flushes both when possible.
type teeFlushWriter struct {
	primary io.Writer
	tap     io.Writer
}

func (t *teeFlushWriter) Write(p []byte) (int, error) {
	n, err := t.primary.Write(p)
	if n > 0 && t.tap != nil {
		_, _ = t.tap.Write(p[:n])
	}
	return n, err
}

func (t *teeFlushWriter) Flush() error {
	logging.FlushWriter(t.primary)
	if f, ok := t.tap.(interface{ Flush() error }); ok {
		return f.Flush()
	}
	return nil
}

func streamWriter(dst io.Writer, tap io.Writer) io.Writer {
	primary := logging.SubprocessWriter(dst)
	if tap == nil {
		return primary
	}
	return &teeFlushWriter{primary: primary, tap: tap}
}
