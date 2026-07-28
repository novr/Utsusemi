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
	cmd := *rootCmd
	cmd.Version = version.String()
	cmd.SetVersionTemplate(version.Line() + "\n")
	cmd.SetOut(&out)
	cmd.SetErr(&bytes.Buffer{})
	cmd.AddCommand(newVersionCmd())
	cmd.SetArgs(args)

	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	got := strings.TrimSpace(out.String())
	want := version.Line()
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}
