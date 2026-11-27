package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/novr/utsusemi/internal/logging"
)

func runCommand(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, stderr.String())
	}
	return nil
}

func runCommandStreaming(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	tap := stdoutTapFrom(ctx)
	out := streamWriter(os.Stdout, tap)
	errOut := streamWriter(os.Stderr, tap)
	defer logging.FlushWriter(out)
	defer logging.FlushWriter(errOut)
	cmd.Stdout = out
	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

// How long to watch a detached process for an immediate failure (e.g. tart
// rejecting invalid --dir). Tunable for tests.
var startDetachedGrace = 750 * time.Millisecond

// The process outlives ctx, which only guards the start itself: a VM must stay
// up for the whole job and is torn down explicitly by Stop and Delete.
func startDetached(ctx context.Context, name string, args []string, env map[string]string) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	cmd := exec.Command(name, args...)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, stderr.String())
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()
	timer := time.NewTimer(startDetachedGrace)
	defer timer.Stop()
	select {
	case err := <-done:
		msg := strings.TrimSpace(stderr.String())
		if err != nil {
			if msg != "" {
				return fmt.Errorf("%s %v: %w: %s", name, args, err, msg)
			}
			return fmt.Errorf("%s %v: %w", name, args, err)
		}
		if msg != "" {
			return fmt.Errorf("%s %v: exited immediately: %s", name, args, msg)
		}
		return fmt.Errorf("%s %v: exited immediately", name, args)
	case <-timer.C:
		go func() { <-done }()
		return nil
	}
}

func outputCommand(ctx context.Context, name string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("%s %v: %w: %s", name, args, err, stderr.String())
	}
	return out, nil
}

type limitedStderr struct {
	mu  sync.Mutex
	buf bytes.Buffer
	max int
}

func (l *limitedStderr) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if rem := l.max - l.buf.Len(); rem > 0 {
		if len(p) > rem {
			_, _ = l.buf.Write(p[:rem])
		} else {
			_, _ = l.buf.Write(p)
		}
	}
	return len(p), nil
}

func (l *limitedStderr) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.TrimSpace(l.buf.String())
}

type cmdExecPipe struct {
	cmd       *exec.Cmd
	inW       *os.File
	outR      *os.File
	stderr    *limitedStderr
	done      chan struct{}
	closeOnce sync.Once
}

// openExecPipe starts name with dedicated OS pipes for binary stdin/stdout so
// cmd.Wait never races with stdout reads.
func openExecPipe(ctx context.Context, name string, args []string) (ExecPipe, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	inR, inW, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	outR, outW, err := os.Pipe()
	if err != nil {
		_ = inR.Close()
		_ = inW.Close()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	cmd.Stdin = inR
	cmd.Stdout = outW
	stderr := &limitedStderr{max: 4096}
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		_ = inR.Close()
		_ = inW.Close()
		_ = outR.Close()
		_ = outW.Close()
		return nil, fmt.Errorf("%s %v: %w", name, args, err)
	}
	_ = inR.Close()
	_ = outW.Close()

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	return &cmdExecPipe{
		cmd:    cmd,
		inW:    inW,
		outR:   outR,
		stderr: stderr,
		done:   done,
	}, nil
}

func (p *cmdExecPipe) Read(b []byte) (int, error)  { return p.outR.Read(b) }
func (p *cmdExecPipe) Write(b []byte) (int, error) { return p.inW.Write(b) }
func (p *cmdExecPipe) Stderr() string              { return p.stderr.String() }

func (p *cmdExecPipe) Close() error {
	p.closeOnce.Do(func() {
		_ = p.inW.Close()
		_ = p.outR.Close()
		select {
		case <-p.done:
			return
		case <-time.After(250 * time.Millisecond):
		}
		if p.cmd.Process != nil {
			_ = p.cmd.Process.Kill()
		}
		<-p.done
	})
	return nil
}

func freeDiskGB(path string) (float64, error) {
	current := path
	for {
		var stat syscall.Statfs_t
		if err := syscall.Statfs(current, &stat); err == nil {
			free := float64(stat.Bavail) * float64(stat.Bsize)
			return free / (1024 * 1024 * 1024), nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return 0, fmt.Errorf("statfs %s: path not found", path)
		}
		current = parent
	}
}

type tartVMRecord struct {
	Name  string `json:"Name"`
	State string `json:"State"`
}

func parseTartLocalList(output []byte, prefix string) ([]VM, error) {
	var records []tartVMRecord
	if err := json.Unmarshal(output, &records); err != nil {
		return nil, fmt.Errorf("parse tart list json: %w", err)
	}
	vms := make([]VM, 0, len(records))
	for _, record := range records {
		if prefix != "" && !hasPrefix(record.Name, prefix) {
			continue
		}
		vms = append(vms, VM{
			Name:    record.Name,
			Running: record.State == "running",
		})
	}
	return vms, nil
}

func hasPrefix(name, prefix string) bool {
	return len(name) >= len(prefix) && name[:len(prefix)] == prefix
}
