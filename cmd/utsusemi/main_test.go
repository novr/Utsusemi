package main

import (
	"path/filepath"
	"testing"
)

func TestDefaultConfigPath(t *testing.T) {
	t.Setenv("UTSUSEMI_CONFIG", "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := filepath.Join(home, ".config", "utsusemi", "config.yaml")

	if got := defaultConfigPath(); got != want {
		t.Fatalf("defaultConfigPath() = %q, want %q", got, want)
	}
}

func TestDefaultConfigPathFromEnvironment(t *testing.T) {
	want := "/custom/config.yaml"
	t.Setenv("UTSUSEMI_CONFIG", want)

	if got := defaultConfigPath(); got != want {
		t.Fatalf("defaultConfigPath() = %q, want %q", got, want)
	}
}
