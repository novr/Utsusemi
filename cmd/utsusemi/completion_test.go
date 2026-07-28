package main

import (
	"bytes"
	"slices"
	"strings"
	"testing"
)

func shellCompletions(t *testing.T, args ...string) string {
	t.Helper()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(append([]string{"__complete"}, args...))
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		rootCmd.SetArgs(nil)
	})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("completion %v: %v", args, err)
	}
	return buf.String()
}

func completionNames(t *testing.T, output string) []string {
	t.Helper()

	var names []string
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		if strings.HasPrefix(line, ":") || strings.HasPrefix(line, "Completion ended") {
			continue
		}
		name, _, _ := strings.Cut(line, "\t")
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func assertCompletionContains(t *testing.T, output string, want ...string) {
	t.Helper()

	names := completionNames(t, output)
	for _, item := range want {
		if !slices.Contains(names, item) {
			t.Fatalf("completion missing %q in %v:\n%s", item, names, output)
		}
	}
}

func TestShellCompletionTopLevel(t *testing.T) {
	out := shellCompletions(t, "")
	assertCompletionContains(t, out,
		"run", "validate", "status", "list", "configure", "clean", "version",
	)
}

func TestShellCompletionTopLevelPrefix(t *testing.T) {
	out := shellCompletions(t, "l")
	assertCompletionContains(t, out, "list")
}

func TestShellCompletionConfigureSubcommands(t *testing.T) {
	out := shellCompletions(t, "configure", "")
	assertCompletionContains(t, out, "app", "token")
}

func TestShellCompletionListTargets(t *testing.T) {
	out := shellCompletions(t, "list", "")
	assertCompletionContains(t, out, "vms", "runners")
}

func TestShellCompletionListTargetPrefix(t *testing.T) {
	out := shellCompletions(t, "list", "v")
	assertCompletionContains(t, out, "vms")
}
