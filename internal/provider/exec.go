package provider

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
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

func parseTartList(output []byte, prefix string) []VM {
	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	var vms []VM
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		name := fields[0]
		if prefix != "" && !strings.HasPrefix(name, prefix) {
			continue
		}
		running := len(fields) > 1 && strings.EqualFold(fields[1], "running")
		vms = append(vms, VM{Name: name, Running: running})
	}
	return vms
}

func parseTartListJSON(output []byte, prefix string) []VM {
	// tart list may output plain text; fallback parser above is primary.
	_ = strconv.Itoa
	return parseTartList(output, prefix)
}
