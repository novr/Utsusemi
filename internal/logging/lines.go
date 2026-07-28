package logging

import (
	"bytes"
	"io"
	"log/slog"
	"os"
)

const subprocessPrefix = "  | "

var subprocessLogger *slog.Logger

// IsTerminalWriter reports whether w is an interactive character device.
func IsTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return isTerminal(f)
}

// SubprocessWriter formats nested command output for the current log mode.
func SubprocessWriter(dst io.Writer) io.Writer {
	if IsTerminalWriter(dst) {
		return &linePrefixWriter{dst: dst, prefix: []byte(subprocessPrefix)}
	}
	if subprocessLogger != nil {
		return &slogLineWriter{logger: subprocessLogger.With("component", "subprocess")}
	}
	return &linePrefixWriter{dst: dst, prefix: []byte(subprocessPrefix)}
}

type linePrefixWriter struct {
	dst    io.Writer
	prefix []byte
	buf    []byte
}

func (w *linePrefixWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := w.buf[:i]
		w.buf = w.buf[i+1:]
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		if _, err := w.writeLine(line); err != nil {
			return 0, err
		}
	}
	return len(p), nil
}

func (w *linePrefixWriter) Flush() error {
	trimmed := bytes.TrimSpace(w.buf)
	if len(trimmed) == 0 {
		w.buf = nil
		return nil
	}
	_, err := w.writeLine(trimmed)
	w.buf = nil
	return err
}

func (w *linePrefixWriter) writeLine(line []byte) (int, error) {
	if _, err := w.dst.Write(w.prefix); err != nil {
		return 0, err
	}
	if _, err := w.dst.Write(line); err != nil {
		return 0, err
	}
	return w.dst.Write([]byte{'\n'})
}

type slogLineWriter struct {
	logger *slog.Logger
	buf    []byte
}

func (w *slogLineWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for {
		i := bytes.IndexByte(w.buf, '\n')
		if i < 0 {
			break
		}
		line := bytes.TrimSpace(w.buf[:i])
		w.buf = w.buf[i+1:]
		if len(line) == 0 {
			continue
		}
		w.logger.Info(string(line))
	}
	return len(p), nil
}

func (w *slogLineWriter) Flush() error {
	line := bytes.TrimSpace(w.buf)
	w.buf = nil
	if len(line) == 0 {
		return nil
	}
	w.logger.Info(string(line))
	return nil
}

// FlushWriter flushes buffered subprocess output.
func FlushWriter(w io.Writer) {
	if flusher, ok := w.(interface{ Flush() error }); ok {
		_ = flusher.Flush()
	}
}
