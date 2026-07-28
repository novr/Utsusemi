package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
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
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s %v: %w", name, args, err)
	}
	return nil
}

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
	go func() {
		_ = cmd.Wait()
	}()
	return nil
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
