package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

func startDetached(ctx context.Context, name string, args []string, env map[string]string) error {
	cmd := exec.CommandContext(ctx, name, args...)
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
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return 0, err
	}
	free := float64(stat.Bavail) * float64(stat.Bsize)
	return free / (1024 * 1024 * 1024), nil
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
