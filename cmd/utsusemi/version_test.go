package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/version"
)

func TestRootVersionFlag(t *testing.T) {
	var out bytes.Buffer
	cmd := *rootCmd
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.SetArgs([]string{"--version"})

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	want := "utsusemi version " + version.String()
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
