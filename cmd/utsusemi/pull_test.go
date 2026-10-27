package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestPullImageRequiresArg(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"pull", "image"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("err=%v", err)
	}
}

func TestPullRunnerRequiresArg(t *testing.T) {
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"pull", "runner"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	err := rootCmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "accepts 1 arg") {
		t.Fatalf("err=%v", err)
	}
}
