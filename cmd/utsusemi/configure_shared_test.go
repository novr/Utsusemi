package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/keychain"
)

func TestMergeConfigureConfigPreservesMounts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	existing := &config.Config{
		Target: config.TargetYAML("my-org", "", 1),
		Registration: config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: config.DefaultHostedAppBrokerURL,
		},
		Labels:        []string{"self-hosted"},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/old:latest",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
		Mounts:        []string{"/tmp/shared"},
	}
	if err := writeConfig(path, existing); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	opts := runnerOptions{}
	addRunnerFlags(cmd, &opts)
	if err := cmd.ParseFlags([]string{"--pool-size", "2"}); err != nil {
		t.Fatal(err)
	}

	merge, err := mergeConfigureConfig(cmd, configureMergeInput{
		Mode:       configureModeApp,
		OutputPath: path,
		Target:     configureTargetInput{Org: "my-org", RunnerGroup: 1},
		App:        configureAppInput{BrokerURL: config.DefaultHostedAppBrokerURL},
		Opts:       opts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if merge.Config.PoolSize != 2 {
		t.Fatalf("pool_size=%d", merge.Config.PoolSize)
	}
	if len(merge.Config.Mounts) != 1 || merge.Config.Mounts[0] != "/tmp/shared" {
		t.Fatalf("mounts=%v", merge.Config.Mounts)
	}
}

func TestMergeConfigureConfigRejectsModeChange(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	existing := &config.Config{
		Target: config.TargetYAML("my-org", "", 1),
		Registration: config.Registration{
			Mode: config.ModeHostedApp,
		},
		Provider: "tart",
	}
	if err := writeConfig(path, existing); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	opts := runnerOptions{}
	addRunnerFlags(cmd, &opts)
	if err := cmd.ParseFlags(nil); err != nil {
		t.Fatal(err)
	}

	_, err := mergeConfigureConfig(cmd, configureMergeInput{
		Mode:       configureModeToken,
		OutputPath: path,
		Target:     configureTargetInput{Org: "my-org", RunnerGroup: 1},
		Opts:       opts,
	})
	if err == nil {
		t.Fatal("expected mode change error")
	}
}

func TestNeedsAppDeviceFlowSkipsWhenCredentialPresent(t *testing.T) {
	store := keychain.NewMemoryStore()
	credentialStore = store
	defer func() { credentialStore = nil }()

	cfg := &config.Config{
		Target: config.TargetYAML("my-org", "", 1),
		Registration: config.Registration{
			Mode:                    config.ModeHostedApp,
			CredentialKeychainService: config.DefaultCredentialService,
		},
	}
	bundle, err := hostcredential.NewBundle("eyJhbGciOiJFUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTl9.c2ln", "refresh-1", "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(cfg.CredentialService(), cfg.CredentialAccount(), bundle); err != nil {
		t.Fatal(err)
	}

	if needsAppDeviceFlow(cfg, configureMergeResult{OrgAuthChanged: false, BrokerAuthChanged: false}) {
		t.Fatal("expected device flow skip")
	}
}

func TestNeedsAppDeviceFlowWhenOrgChanged(t *testing.T) {
	store := keychain.NewMemoryStore()
	credentialStore = store
	defer func() { credentialStore = nil }()

	cfg := &config.Config{
		Target: config.TargetYAML("my-org", "", 1),
		Registration: config.Registration{
			Mode:                    config.ModeHostedApp,
			CredentialKeychainService: config.DefaultCredentialService,
		},
	}
	bundle, err := hostcredential.NewBundle("eyJhbGciOiJFUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTl9.c2ln", "refresh-1", "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(cfg.CredentialService(), cfg.CredentialAccount(), bundle); err != nil {
		t.Fatal(err)
	}

	if !needsAppDeviceFlow(cfg, configureMergeResult{OrgAuthChanged: true, BrokerAuthChanged: false}) {
		t.Fatal("expected device flow when org changed")
	}
}

func TestNeedsAppDeviceFlowSkipsWhenOrgFlagUnchangedValue(t *testing.T) {
	store := keychain.NewMemoryStore()
	credentialStore = store
	defer func() { credentialStore = nil }()

	cfg := &config.Config{
		Target: config.TargetYAML("my-org", "", 1),
		Registration: config.Registration{
			Mode:                      config.ModeHostedApp,
			CredentialKeychainService: config.DefaultCredentialService,
		},
	}
	bundle, err := hostcredential.NewBundle("eyJhbGciOiJFUzI1NiJ9.eyJleHAiOjk5OTk5OTk5OTl9.c2ln", "refresh-1", "octocat")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Set(cfg.CredentialService(), cfg.CredentialAccount(), bundle); err != nil {
		t.Fatal(err)
	}

	if needsAppDeviceFlow(cfg, configureMergeResult{OrgAuthChanged: false, BrokerAuthChanged: false}) {
		t.Fatal("expected device flow skip for unchanged org value")
	}
}

func TestTryAppOAuthRefreshRequiresCredential(t *testing.T) {
	store := keychain.NewMemoryStore()
	credentialStore = store
	defer func() { credentialStore = nil }()

	cfg := &config.Config{
		Target: config.TargetYAML("my-org", "", 1),
		Registration: config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: config.DefaultHostedAppBrokerURL,
		},
	}
	config.ApplyDefaults(cfg)

	_, err := tryAppOAuthRefresh(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected error without credential")
	}
}

func TestEnsureConfigureAppRefreshCompatibleRejectsPAT(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	existing := &config.Config{
		Target: config.TargetYAML("", "owner/repo", 1),
		Registration: config.Registration{Mode: config.ModeGitHubPAT},
		Provider:     "tart",
	}
	if err := writeConfig(path, existing); err != nil {
		t.Fatal(err)
	}
	if err := ensureConfigureAppRefreshCompatible(path); err == nil {
		t.Fatal("expected error for github_pat config")
	}
}

func TestConfigureOnlyCredentialRefresh(t *testing.T) {
	cmd := &cobra.Command{}
	var refresh bool
	cmd.Flags().BoolVar(&refresh, "refresh", false, "")
	opts := runnerOptions{}
	addRunnerFlags(cmd, &opts)
	if err := cmd.ParseFlags([]string{"--refresh"}); err != nil {
		t.Fatal(err)
	}
	if !configureOnlyCredentialRefresh(cmd, true) {
		t.Fatal("expected credential-only refresh")
	}
	if configureOnlyCredentialRefresh(cmd, false) {
		t.Fatal("expected false without --refresh flag")
	}
	if err := cmd.ParseFlags([]string{"--refresh", "--pool-size", "2"}); err != nil {
		t.Fatal(err)
	}
	if configureOnlyCredentialRefresh(cmd, true) {
		t.Fatal("expected false when config flags change")
	}
}

func TestConfigureOutputPathUsesGlobalConfig(t *testing.T) {
	cmd := &cobra.Command{}
	var output string
	cmd.Flags().StringVar(&output, "output", "/default/output.yaml", "config output path")

	prev := configPath
	t.Cleanup(func() { configPath = prev })
	configPath = "/global/config.yaml"
	if got := configureOutputPath(cmd, output); got != "/global/config.yaml" {
		t.Fatalf("got %q, want /global/config.yaml", got)
	}

	if err := cmd.ParseFlags([]string{"--output", "/explicit.yaml"}); err != nil {
		t.Fatal(err)
	}
	if got := configureOutputPath(cmd, output); got != "/explicit.yaml" {
		t.Fatalf("got %q, want /explicit.yaml", got)
	}
}

func TestWriteConfigBytesAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "config.yaml")
	if err := writeConfigBytes(path, []byte("key: value\n")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "key: value\n" {
		t.Fatalf("got %q", string(data))
	}
}
