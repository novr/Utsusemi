package main

import (
	"testing"
)

func TestRunLogFlagNoOptUsesDefaultSentinel(t *testing.T) {
	cmd := newRunCmd()
	if err := cmd.ParseFlags([]string{"--log"}); err != nil {
		t.Fatal(err)
	}
	got, err := cmd.Flags().GetString("log")
	if err != nil {
		t.Fatal(err)
	}
	if got != "-" {
		t.Fatalf("got %q, want -", got)
	}
	if !cmd.Flags().Changed("log") {
		t.Fatal("expected --log to be marked changed")
	}
}

func TestRunLogFlagExplicitPath(t *testing.T) {
	cmd := newRunCmd()
	if err := cmd.ParseFlags([]string{"--log=/tmp/custom.log"}); err != nil {
		t.Fatal(err)
	}
	got, err := cmd.Flags().GetString("log")
	if err != nil {
		t.Fatal(err)
	}
	if got != "/tmp/custom.log" {
		t.Fatalf("got %q", got)
	}
}
