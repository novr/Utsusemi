package provider

import (
	"context"
	"os"
	"path/filepath"
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
	p := NewTartProvider(exec, true)
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
	p := NewTartProvider(exec, false)
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
