package provider

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTartHomeUsesEnv(t *testing.T) {
	t.Setenv("TART_HOME", "/tmp/custom-tart")
	if got := tartHome(); got != "/tmp/custom-tart" {
		t.Fatalf("tartHome() = %q", got)
	}
}

func TestTartHomeDefaultsToUserDir(t *testing.T) {
	t.Setenv("TART_HOME", "")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".tart")
	if got := tartHome(); got != want {
		t.Fatalf("tartHome() = %q, want %q", got, want)
	}
}

func TestStartUsesSoftnetFlag(t *testing.T) {
	exec := NewFakeExecutor()
	p := NewTartProvider(exec, true, nil)
	if err := p.Start(context.Background(), "vm-1"); err != nil {
		t.Fatal(err)
	}
	if len(exec.Calls) != 1 {
		t.Fatalf("calls = %d", len(exec.Calls))
	}
	want := []string{"run", "vm-1", "--no-graphics", "--net-softnet"}
	got := exec.Calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestStartWithoutSoftnet(t *testing.T) {
	exec := NewFakeExecutor()
	p := NewTartProvider(exec, false, nil)
	if err := p.Start(context.Background(), "vm-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"run", "vm-1", "--no-graphics"}
	got := exec.Calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestStartWithMounts(t *testing.T) {
	exec := NewFakeExecutor()
	p := NewTartProvider(exec, false, []string{"/host/cache:ro", "/host/tools"})
	if err := p.Start(context.Background(), "vm-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"run", "vm-1", "--no-graphics",
		"--dir=/host/cache:ro",
		"--dir=/host/tools",
	}
	got := exec.Calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("args = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestDeleteOmitsForceFlag(t *testing.T) {
	exec := NewFakeExecutor()
	p := NewTartProvider(exec, false, nil)
	if err := p.Delete(context.Background(), "vm-1"); err != nil {
		t.Fatal(err)
	}
	want := []string{"delete", "vm-1"}
	got := exec.Calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestExecStdinOmitsSeparator(t *testing.T) {
	exec := NewFakeExecutor()
	p := NewTartProvider(exec, false, nil)
	if err := p.ExecStdin(context.Background(), "vm-1", "bash", []string{"-c", "true"}, nil, nil); err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "vm-1", "bash", "-c", "true"}
	got := exec.Calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestExecStdinAttachesInputAndForwardsEnv(t *testing.T) {
	exec := NewFakeExecutor()
	p := NewTartProvider(exec, false, nil)
	env := map[string]string{"RUNNER_VERSION": "2.336.0"}
	if err := p.ExecStdin(context.Background(), "vm-1", "bash", []string{"-c", "true"}, []byte("jit"), env); err != nil {
		t.Fatal(err)
	}
	want := []string{"exec", "-i", "vm-1", "env", "RUNNER_VERSION=2.336.0", "bash", "-c", "true"}
	got := exec.Calls[0].Args
	if len(got) != len(want) {
		t.Fatalf("args = %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("args = %v, want %v", got, want)
		}
	}
}

func TestRequireTartVersionAcceptsMinimum(t *testing.T) {
	if err := requireTartVersion("2.34.0"); err != nil {
		t.Fatalf("unexpected error for 2.34.0: %v", err)
	}
}

func TestRequireTartVersionAcceptsNewer(t *testing.T) {
	for _, v := range []string{"2.34.1", "2.35.0", "3.0.0"} {
		if err := requireTartVersion(v); err != nil {
			t.Fatalf("unexpected error for %s: %v", v, err)
		}
	}
}

func TestRequireTartVersionRejectsOlder(t *testing.T) {
	for _, v := range []string{"2.32.1", "2.33.9", "1.99.99"} {
		err := requireTartVersion(v)
		if err == nil {
			t.Fatalf("expected error for %s, got nil", v)
		}
		if !strings.Contains(err.Error(), "openai/tools") {
			t.Fatalf("error for %s missing upgrade hint: %v", v, err)
		}
	}
}

func TestVersionAtLeast(t *testing.T) {
	tests := []struct {
		got  string
		min  string
		want bool
	}{
		{"2.34.0", "2.34.0", true},
		{"2.34.1", "2.34.0", true},
		{"2.34", "2.34.0", false},
		{"2.33.9", "2.34.0", false},
		{"2.33.0", "2.34.0", false},
		{"", "2.34.0", false},
		{"v2.34.0", "2.34.0", false},
		{"2.34.0-dev", "2.34.0", true},
	}
	for _, tt := range tests {
		if got := versionAtLeast(tt.got, tt.min); got != tt.want {
			t.Errorf("versionAtLeast(%q, %q) = %v, want %v", tt.got, tt.min, got, tt.want)
		}
	}
}

func TestTartProviderAvailableChecksVersion(t *testing.T) {
	installFakeTart(t)
	exec := NewFakeExecutor()
	exec.SetVersionOutput("2.34.0\n")
	p := NewTartProvider(exec, false, nil)
	if err := p.Available(); err != nil {
		t.Fatalf("Available: %v", err)
	}
	if len(exec.Calls) != 1 || exec.Calls[0].Name != "tart" || len(exec.Calls[0].Args) != 1 || exec.Calls[0].Args[0] != "--version" {
		t.Fatalf("calls = %+v", exec.Calls)
	}
}

func TestTartProviderAvailableRejectsOldVersion(t *testing.T) {
	installFakeTart(t)
	exec := NewFakeExecutor()
	exec.SetVersionOutput("2.32.1")
	p := NewTartProvider(exec, false, nil)
	err := p.Available()
	if err == nil || !strings.Contains(err.Error(), "openai/tools") {
		t.Fatalf("Available: %v", err)
	}
}

func installFakeTart(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	tartPath := filepath.Join(dir, "tart")
	if err := os.WriteFile(tartPath, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

func TestFreeDiskGBWalksMissingPath(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist", "nested")
	free, err := freeDiskGB(missing)
	if err != nil {
		t.Fatal(err)
	}
	if free <= 0 {
		t.Fatalf("free = %v", free)
	}
}
