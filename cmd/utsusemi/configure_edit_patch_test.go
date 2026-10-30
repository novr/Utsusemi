package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/novr/utsusemi/internal/config"
)

func TestConfigEditPatchPoolSizeHostedApp(t *testing.T) {
	path := writeEditPatchFixture(t, config.ModeHostedApp)
	cmd, opts := editPatchCmd()
	if err := cmd.ParseFlags([]string{"--pool-size", "2"}); err != nil {
		t.Fatal(err)
	}
	var latestCalls int
	err := runConfigEditPatch(cmd, path, *opts, true, func(context.Context) (string, error) {
		latestCalls++
		return "9.9.9", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if latestCalls != 0 {
		t.Fatalf("pool-size alone must not resolve latest; calls=%d", latestCalls)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.PoolSize != 2 {
		t.Fatalf("pool_size=%d", loaded.PoolSize)
	}
	if loaded.RunnerVersion != "2.336.0" {
		t.Fatalf("runner_version=%q", loaded.RunnerVersion)
	}
}

func TestConfigEditPatchRunnerVersionLatestPAT(t *testing.T) {
	path := writeEditPatchFixture(t, config.ModeGitHubPAT)
	cmd, opts := editPatchCmd()
	if err := cmd.ParseFlags([]string{"--runner-version", "latest"}); err != nil {
		t.Fatal(err)
	}
	err := runConfigEditPatch(cmd, path, *opts, true, func(context.Context) (string, error) {
		return "2.340.0", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RunnerVersion != "2.340.0" {
		t.Fatalf("runner_version=%q", loaded.RunnerVersion)
	}
	if loaded.Registration.Mode != config.ModeGitHubPAT {
		t.Fatalf("mode=%q", loaded.Registration.Mode)
	}
}

func TestConfigEditPatchDoesNotAppendDefaults(t *testing.T) {
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
	cmd, opts := editPatchCmd()
	if err := cmd.ParseFlags([]string{"--pool-size", "2"}); err != nil {
		t.Fatal(err)
	}
	if err := runConfigEditPatch(cmd, path, *opts, true, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	want := strings.Replace(original, "pool_size: 1", "pool_size: 2", 1)
	if text != want {
		t.Fatalf("patch rewrote more than pool_size:\n%s\nwant:\n%s", text, want)
	}
	for _, keep := range []string{
		"# keep me",
		`runner_version: "2.336.0"`,
		"reclaim_policy: grace",
		"pool_check_interval: 30s",
		"min_free_disk_gb: 50",
		"vm_name_prefix: utsusemi-",
		"state_dir: /tmp/utsusemi-state",
		"softnet: false",
		"broker_url: https://utsusemi.duckdns.org",
		"pool_size: 2",
	} {
		if !strings.Contains(text, keep) {
			t.Fatalf("missing %q in\n%s", keep, text)
		}
	}
	for _, absent := range []string{
		"credential_keychain_service",
		"reconciliation_interval",
		"spawn_timeout",
		"job_timeout",
		"provider:",
	} {
		if strings.Contains(text, absent) {
			t.Fatalf("appended %q in\n%s", absent, text)
		}
	}
	after, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.PoolSize != 2 {
		t.Fatalf("pool_size=%d", after.PoolSize)
	}
	if after.RunnerVersion != before.RunnerVersion || after.ReclaimPolicy != before.ReclaimPolicy ||
		after.StateDir != before.StateDir || after.VMNamePrefix != before.VMNamePrefix ||
		after.MinFreeDiskGB != before.MinFreeDiskGB || after.Softnet != before.Softnet ||
		after.PoolCheckInterval != before.PoolCheckInterval || after.Registration.BrokerURL != before.Registration.BrokerURL {
		t.Fatalf("effective config changed beyond pool_size")
	}
}

func TestConfigEditPatchWritesExplicitFalseAndClearsMounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := strings.Replace(sparseEditYAML(1), "softnet: false\n", "mounts:\n  - /tmp/cache\n", 1)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, opts := editPatchCmd()
	if err := cmd.ParseFlags([]string{"--softnet=false", "--mounts="}); err != nil {
		t.Fatal(err)
	}
	if err := runConfigEditPatch(cmd, path, *opts, true, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	want := strings.Replace(body, "mounts:\n  - /tmp/cache\n", "", 1) + "softnet: false\n"
	if text != want {
		t.Fatalf("patch rewrote more than mounts and softnet:\n%s\nwant:\n%s", text, want)
	}
}

func TestConfigEditPatchSingleValueSequences(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := strings.Replace(sparseEditYAML(1), "labels: [self-hosted]\n", "labels: [self-hosted] # groups\n", 1)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, opts := editPatchCmd()
	if err := cmd.ParseFlags([]string{"--labels", "self-hosted", "--mounts", "/tmp/cache"}); err != nil {
		t.Fatal(err)
	}
	if err := runConfigEditPatch(cmd, path, *opts, true, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "labels: [self-hosted] # groups\n", "labels: # groups\n  - self-hosted\n", 1) +
		"mounts:\n  - /tmp/cache\n"
	if string(raw) != want {
		t.Fatalf("patch rewrote more than labels and mounts:\n%s\nwant:\n%s", raw, want)
	}
	loaded, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Labels) != 1 || loaded.Labels[0] != "self-hosted" {
		t.Fatalf("labels=%v", loaded.Labels)
	}
	if len(loaded.Mounts) != 1 || loaded.Mounts[0] != "/tmp/cache" {
		t.Fatalf("mounts=%v", loaded.Mounts)
	}
}

func TestConfigEditPatchClearMountsKeepsNeighbors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	original := strings.Replace(sparseEditYAML(1), "softnet: false\nnotes: kept\n",
		"softnet: false\n\n# cache\nmounts:\n  - /tmp/cache\nnotes: kept\n", 1)
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd, opts := editPatchCmd()
	if err := cmd.ParseFlags([]string{"--mounts="}); err != nil {
		t.Fatal(err)
	}
	if err := runConfigEditPatch(cmd, path, *opts, true, nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(original, "# cache\nmounts:\n  - /tmp/cache\n", "", 1)
	if string(raw) != want {
		t.Fatalf("clear mounts rewrote neighbors:\n%s\nwant:\n%s", raw, want)
	}
}

func TestConfigEditPatchRequiresExistingConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.yaml")
	cmd, opts := editPatchCmd()
	if err := cmd.ParseFlags([]string{"--pool-size", "2"}); err != nil {
		t.Fatal(err)
	}
	err := runConfigEditPatch(cmd, path, *opts, true, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "configure app") {
		t.Fatalf("err=%v", err)
	}
}

func TestConfigEditPatchForceSkipsPrompt(t *testing.T) {
	path := writeEditPatchFixture(t, config.ModeHostedApp)
	cmd, opts := editPatchCmd()
	cmd.SetIn(strings.NewReader(""))
	if err := cmd.ParseFlags([]string{"--pool-size", "2"}); err != nil {
		t.Fatal(err)
	}
	if err := runConfigEditPatch(cmd, path, *opts, false, nil); err == nil {
		t.Fatal("expected --force required for non-TTY")
	}
	if err := runConfigEditPatch(cmd, path, *opts, true, nil); err != nil {
		t.Fatal(err)
	}
}

func TestConfigEditPatchAbortsBeforeLatestResolve(t *testing.T) {
	path := writeEditPatchFixture(t, config.ModeHostedApp)
	cmd, opts := editPatchCmd()
	cmd.SetIn(strings.NewReader(""))
	if err := cmd.ParseFlags([]string{"--runner-version", "latest"}); err != nil {
		t.Fatal(err)
	}
	var latestCalls int
	err := runConfigEditPatch(cmd, path, *opts, false, func(context.Context) (string, error) {
		latestCalls++
		return "2.340.0", nil
	})
	if err == nil {
		t.Fatal("expected abort without --force")
	}
	if latestCalls != 0 {
		t.Fatalf("confirm must run before latest resolve; calls=%d", latestCalls)
	}
}

func TestSharedConfigFlagsChangedIgnoresForce(t *testing.T) {
	cmd := newConfigureEditCmd()
	if err := cmd.ParseFlags([]string{"--force"}); err != nil {
		t.Fatal(err)
	}
	if sharedConfigFlagsChanged(cmd) {
		t.Fatal("--force alone must stay in $EDITOR mode")
	}
	cmd2 := newConfigureEditCmd()
	if err := cmd2.ParseFlags([]string{"--pool-size", "2"}); err != nil {
		t.Fatal(err)
	}
	if !sharedConfigFlagsChanged(cmd2) {
		t.Fatal("--pool-size must select patch mode")
	}
}

func TestSharedConfigFlagNamesCoverRegisteredFlags(t *testing.T) {
	opts := &sharedConfigOptions{}
	cmd := &cobra.Command{}
	addSharedConfigFlags(cmd, opts)
	registered := map[string]bool{}
	cmd.Flags().VisitAll(func(f *pflag.Flag) {
		registered[f.Name] = true
	})
	for _, name := range sharedConfigFlagNames {
		if !registered[name] {
			t.Fatalf("sharedConfigFlagNames has %q but addSharedConfigFlags did not register it", name)
		}
		delete(registered, name)
	}
	if len(registered) != 0 {
		t.Fatalf("addSharedConfigFlags registered extras not in sharedConfigFlagNames: %v", registered)
	}
}

func editPatchCmd() (*cobra.Command, *sharedConfigOptions) {
	opts := &sharedConfigOptions{}
	cmd := &cobra.Command{}
	addSharedConfigFlags(cmd, opts)
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	return cmd, opts
}

func writeEditPatchFixture(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := &config.Config{
		Labels:        []string{"self-hosted"},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example:latest",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	switch mode {
	case config.ModeGitHubPAT:
		cfg.Target = config.TargetYAML("", "owner/repo", 1)
		cfg.Registration = config.Registration{Mode: config.ModeGitHubPAT}
	default:
		cfg.Target = config.TargetYAML("my-org", "", 1)
		cfg.Registration = config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: config.DefaultHostedAppBrokerURL,
		}
	}
	if err := writeConfig(path, cfg); err != nil {
		t.Fatal(err)
	}
	return path
}
