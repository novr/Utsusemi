package main

import (
	"strings"
	"testing"
)

func TestResolveTokenPrefersStdin(t *testing.T) {
	got, err := resolveToken(strings.NewReader("stdin-token\n"), "flag-token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "stdin-token" {
		t.Fatalf("got %q, want stdin-token", got)
	}
}

func TestResolveTokenFallsBackToFlag(t *testing.T) {
	got, err := resolveToken(strings.NewReader(""), "flag-token")
	if err != nil {
		t.Fatal(err)
	}
	if got != "flag-token" {
		t.Fatalf("got %q, want flag-token", got)
	}
}

func TestResolveTokenRequiresInput(t *testing.T) {
	_, err := resolveToken(strings.NewReader(""), "")
	if err == nil {
		t.Fatal("expected error")
	}
}
