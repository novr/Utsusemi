package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfirmConfigOverwriteMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := confirmConfigOverwrite(path, false, strings.NewReader(""), &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmConfigOverwriteForce(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := confirmConfigOverwrite(path, true, strings.NewReader(""), &strings.Builder{}); err != nil {
		t.Fatal(err)
	}
}

func TestConfirmConfigOverwriteRequiresForceNonInteractive(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := confirmConfigOverwrite(path, false, strings.NewReader("y\n"), &strings.Builder{})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "--force") {
		t.Fatalf("got %v, want --force hint", err)
	}
}
