package provider

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestWithStdoutTapTeesStreamingOutput(t *testing.T) {
	var tap bytes.Buffer
	ctx := WithStdoutTap(context.Background(), &tap)
	if err := runCommandStreaming(ctx, "bash", []string{"-c", "echo hello-tap"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tap.String(), "hello-tap") {
		t.Fatalf("tap=%q", tap.String())
	}
}

func TestWithStdoutTapNilIsNoop(t *testing.T) {
	ctx := WithStdoutTap(context.Background(), nil)
	if stdoutTapFrom(ctx) != nil {
		t.Fatal("expected nil tap")
	}
}

func TestTeeFlushWriterFlushesTap(t *testing.T) {
	var primary, tap bytes.Buffer
	w := &teeFlushWriter{primary: &primary, tap: &flushBuffer{buf: &tap}}
	if _, err := w.Write([]byte("partial")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	if tap.String() != "flushed" {
		t.Fatalf("tap flush marker=%q", tap.String())
	}
}

type flushBuffer struct {
	buf *bytes.Buffer
}

func (f *flushBuffer) Write(p []byte) (int, error) { return f.buf.Write(p) }

func (f *flushBuffer) Flush() error {
	f.buf.Reset()
	f.buf.WriteString("flushed")
	return nil
}
