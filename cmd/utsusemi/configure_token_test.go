package main

import (
	"os"
	"strings"
	"testing"
)

func TestResolveTokenPrefersFlag(t *testing.T) {
	got, err := resolveTokenOptional(strings.NewReader("stdin-token\n"), "flag-token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "flag-token" {
		t.Fatalf("got %q, want flag-token", got)
	}
}

func TestResolveTokenFromStdinPipe(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("stdin-token\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := resolveTokenOptional(r, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if got != "stdin-token" {
		t.Fatalf("got %q, want stdin-token", got)
	}
}

func TestResolveTokenOptionalEmpty(t *testing.T) {
	got, err := resolveTokenOptional(strings.NewReader(""), "")
	if err != nil {
		t.Fatal(err)
	}
	if got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}
