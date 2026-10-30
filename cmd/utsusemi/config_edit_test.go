package main

import (
	"bytes"
	"fmt"
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
	script := "#!/bin/sh\ntmp=\"$1.tmp\"\nsed 's/pool_size: 1/pool_size: 2/' \"$1\" > \"$tmp\" && mv \"$tmp\" \"$1\"\n"
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
	script := "#!/bin/sh\ntmp=\"$1.tmp\"\nsed 's/pool_size: 1/pool_size: 0/' \"$1\" > \"$tmp\" && mv \"$tmp\" \"$1\"\n"
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

func TestConfigEditNewFileFromTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	editor := filepath.Join(dir, "editor.sh")
	script := "#!/bin/sh\ntmp=\"$1.tmp\"\nsed 's/my-org/smoke-org/' \"$1\" > \"$tmp\" && mv \"$tmp\" \"$1\"\n"
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
	if loaded.Target.Org != "smoke-org" {
		t.Fatalf("org=%q", loaded.Target.Org)
	}
	if loaded.Registration.BrokerURL != config.DefaultHostedAppBrokerURL {
		t.Fatalf("broker_url=%q", loaded.Registration.BrokerURL)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "pool_check_interval") || strings.Contains(text, "\n  broker_url:") {
		t.Fatalf("edit wrote defaults into a new file:\n%s", text)
	}
	if !strings.Contains(text, "# reclaim_policy: grace") {
		t.Fatalf("template comment dropped:\n%s", text)
	}
}

func editCommand() *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd
}

func writeEditor(t *testing.T, dir, script string) error {
	t.Helper()
	editor := filepath.Join(dir, "editor.sh")
	if err := os.WriteFile(editor, []byte(script), 0o755); err != nil {
		return err
	}
	t.Setenv("EDITOR", editor)
	return nil
}

func TestConfigEditDoesNotAppendDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := sparseEditYAML(1)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\ntmp=\"$1.tmp\"\nsed 's/pool_size: 1/pool_size: 2/' \"$1\" > \"$tmp\" && mv \"$tmp\" \"$1\"\n"
	if err := writeEditor(t, dir, script); err != nil {
		t.Fatal(err)
	}
	cmd := editCommand()
	if err := runConfigEdit(cmd, path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got := string(raw)
	want := strings.Replace(original, "pool_size: 1", "pool_size: 2", 1)
	if got != want {
		t.Fatalf("edited file:\n%s\nwant:\n%s", got, want)
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.PoolSize != 2 || before.PoolSize != 1 {
		t.Fatalf("pool before=%d after=%d", before.PoolSize, after.PoolSize)
	}
	if after.ReclaimPolicy != before.ReclaimPolicy || after.StateDir != before.StateDir || after.Softnet != before.Softnet {
		t.Fatalf("effective config changed beyond pool_size: before=%+v after=%+v", before, after)
	}
}

func TestConfigShowPrintsStoredFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := sparseEditYAML(1)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := editCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	if err := runConfigShow(cmd, path); err != nil {
		t.Fatal(err)
	}
	if out.String() != original {
		t.Fatalf("show=\n%s\nwant=\n%s", out.String(), original)
	}
}

func TestConfigShowRejectsInvalidYAML(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(path, []byte("not: [valid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := editCommand()
	err := runConfigShow(cmd, path)
	if err == nil || !strings.Contains(err.Error(), "parse config") {
		t.Fatalf("err=%v", err)
	}
}

func sparseEditYAML(poolSize int) string {
	return fmt.Sprintf(`# keep me
target:
  org: my-org
  runner_group_id: 1

# runners
labels: [self-hosted]
registration:
  mode: hosted_app
  broker_url: https://utsusemi.duckdns.org
base_image: ghcr.io/example:latest
runner_version: "2.336.0"
pool_size: %d  # slots
reclaim_policy: grace
reclaim_grace: 15m
pool_check_interval: 30s
min_free_disk_gb: 50
vm_name_prefix: utsusemi-
state_dir: /tmp/utsusemi-state
softnet: false
notes: kept
`, poolSize)
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
