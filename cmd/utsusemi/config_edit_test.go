package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
)

func TestConfigEditWritesValidatedConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	initial := &config.Config{
		Target: config.TargetYAML("my-org", "", 1),
		Registration: config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: config.DefaultHostedAppBrokerURL,
		},
		Labels:        []string{"self-hosted"},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example:latest",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	if err := writeConfig(path, initial); err != nil {
		t.Fatal(err)
	}

	editor := filepath.Join(dir, "editor.sh")
	script := "#!/bin/sh\nsed -i '' 's/pool_size: 1/pool_size: 2/' \"$1\"\n"
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := runConfigEdit(cmd, path); err != nil {
		t.Fatal(err)
	}

	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PoolSize != 2 {
		t.Fatalf("pool_size=%d", loaded.PoolSize)
	}
}

func TestConfigEditKeepsTempOnValidationError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("target:\n  org: my-org\nregistration:\n  mode: hosted_app\n  broker_url: https://example.com\nprovider: tart\nbase_image: ghcr.io/example:latest\nrunner_version: \"2.336.0\"\nlabels: [self-hosted]\npool_size: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	editor := filepath.Join(dir, "editor.sh")
	script := "#!/bin/sh\nsed -i '' 's/pool_size: 1/pool_size: 0/' \"$1\"\n"
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runConfigEdit(cmd, path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), ".utsusemi-config-edit-") {
		t.Fatalf("expected temp path in error: %v", err)
	}
}

func TestConfigEditRejectsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("target:\n  org: my-org\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	editor := filepath.Join(dir, "editor.sh")
	script := "#!/bin/sh\necho 'not: [valid' > \"$1\"\n"
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("EDITOR", editor)

	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	err := runConfigEdit(cmd, path)
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "validate edited config") && !strings.Contains(err.Error(), "parse edited config") {
		t.Fatalf("unexpected error: %v", err)
	}
}
