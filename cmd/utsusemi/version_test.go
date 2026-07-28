package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/version"
)

func TestRootVersionFlag(t *testing.T) {
	testVersionOutput(t, []string{"--version"})
}

func TestVersionSubcommand(t *testing.T) {
	testVersionOutput(t, []string{"version"})
}

func testVersionOutput(t *testing.T, args []string) {
	t.Helper()

	old := version.Version
	version.Version = "1.2.3-test"
	t.Cleanup(func() { version.Version = old })

	var out bytes.Buffer
	oldRootVersion := rootCmd.Version
	oldVersionTemplate := rootCmd.VersionTemplate()
	rootCmd.Version = version.String()
	rootCmd.SetVersionTemplate(version.Line() + "\n")
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&bytes.Buffer{})
	rootCmd.SetArgs(args)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
		rootCmd.Version = oldRootVersion
		rootCmd.SetVersionTemplate(oldVersionTemplate)
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	want := version.Line()
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
