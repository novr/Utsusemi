package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/novr/utsusemi/internal/provider"
)

func setupRuntimeLogTest(t *testing.T) (cfgPath string, stateDir string, exec *provider.FakeExecutor) {
	root := t.TempDir()
	bin := filepath.Join(root, "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	tart := filepath.Join(bin, "tart")
	if err := os.WriteFile(tart, []byte("#!/bin/sh\necho 2.34.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	stateDir = filepath.Join(root, "state")
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgPath = filepath.Join(root, "config.yaml")
	content := fmt.Sprintf(`target:
  org: my-org
  runner_group_id: 1
labels: [self-hosted, macOS]
registration:
  mode: github_pat
provider: tart
base_image: ghcr.io/example/image:1
runner_version: "2.336.0"
pool_size: 1
state_dir: %s
`, stateDir)
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	exec = provider.NewFakeExecutor()
	exec.SetVersionOutput("2.34.0\n")
	return cfgPath, stateDir, exec
}

func TestLoadValidatedDefersAgentLogUntilCredentialValid(t *testing.T) {
	cfgPath, stateDir, exec := setupRuntimeLogTest(t)
	logPath := filepath.Join(stateDir, "agent.log")

	_, err := LoadValidated(context.Background(), LoadOptions{
		ConfigPath:      cfgPath,
		Exec:            exec,
		ResolveAgentLog: true,
	})
	if err == nil {
		t.Fatal("expected credential validation to fail")
	}
	if _, statErr := os.Stat(logPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no agent log before successful validation, stat err=%v", statErr)
	}
}

func TestLoadSetsLogFilePath(t *testing.T) {
	cfgPath, stateDir, exec := setupRuntimeLogTest(t)
	logPath := filepath.Join(stateDir, "agent.log")

	rt, err := Load(context.Background(), LoadOptions{
		ConfigPath:      cfgPath,
		Exec:            exec,
		ResolveAgentLog: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if rt.LogFilePath != logPath {
		t.Fatalf("got log path %q, want %q", rt.LogFilePath, logPath)
	}
}
