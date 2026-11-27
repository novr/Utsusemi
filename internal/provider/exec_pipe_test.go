package provider

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"reflect"
	"testing"
	"time"
)

func TestOpenExecPipeBinaryRoundTrip(t *testing.T) {
	p, err := openExecPipe(context.Background(), "cat", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()

	payload := make([]byte, 1<<20)
	if _, err := rand.Read(payload); err != nil {
		t.Fatal(err)
	}
	go func() { _, _ = p.Write(payload) }()

	got := make([]byte, len(payload))
	if _, err := io.ReadFull(p, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("round-trip mismatch")
	}
}

func TestOpenExecPipeCloseTerminatesProcess(t *testing.T) {
	p, err := openExecPipe(context.Background(), "sleep", []string{"30"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		_ = p.Close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not terminate the child")
	}
	if err := p.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestOpenExecPipeStderrCaptured(t *testing.T) {
	p, err := openExecPipe(context.Background(), "sh", []string{"-c", "echo boom >&2"})
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	_, _ = io.Copy(io.Discard, p)
	_ = p.Close()
	if got := p.Stderr(); got != "boom" {
		t.Fatalf("stderr = %q", got)
	}
}

func TestOpenExecPipeCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := openExecPipe(ctx, "cat", nil); err == nil {
		t.Fatal("expected error for canceled context")
	}
}

type pipeExecutor struct {
	*FakeExecutor
	name string
	args []string
}

func (e *pipeExecutor) OpenPipe(_ context.Context, name string, args []string) (ExecPipe, error) {
	e.name, e.args = name, args
	return nil, io.EOF
}

func TestTartOpenExecPipeArgs(t *testing.T) {
	ex := &pipeExecutor{FakeExecutor: NewFakeExecutor()}
	p := NewTartProvider(ex, false, nil)
	if _, err := p.OpenExecPipe(context.Background(), "vm1", "bash", []string{"-c", "echo hi", "_", "a"}); err != io.EOF {
		t.Fatalf("err = %v", err)
	}
	if ex.name != "tart" {
		t.Fatalf("name = %q", ex.name)
	}
	want := []string{"exec", "-i", "vm1", "bash", "-c", "echo hi", "_", "a"}
	if !reflect.DeepEqual(ex.args, want) {
		t.Fatalf("args = %v, want %v", ex.args, want)
	}
}

func TestTartOpenExecPipeUnsupportedExecutor(t *testing.T) {
	p := NewTartProvider(NewFakeExecutor(), false, nil)
	if _, err := p.OpenExecPipe(context.Background(), "vm1", "bash", nil); err == nil {
		t.Fatal("expected error when the executor cannot open pipes")
	}
}
