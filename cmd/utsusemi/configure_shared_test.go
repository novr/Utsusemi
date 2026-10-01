package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/keychain"
)

func TestMergeConfigureConfigRunnerGroupAuthChanged(t *testing.T) {
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
	}
	if err := writeConfig(path, existing); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	opts := sharedConfigOptions{}
	addSharedConfigFlags(cmd, &opts)
	var runnerGroup int64
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "")
	if err := cmd.ParseFlags([]string{"--runner-group-id", "2"}); err != nil {
		t.Fatal(err)
	}

	merge, err := mergeConfigureConfig(cmd, configureMergeInput{
		Mode:       configureModeApp,
		OutputPath: path,
		Target:     configureTargetInput{Org: "my-org", RunnerGroup: 2},
		App:        configureAppInput{BrokerURL: config.DefaultHostedAppBrokerURL},
		Opts:       opts,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !merge.RunnerGroupAuthChanged {
		t.Fatal("expected RunnerGroupAuthChanged when group value changes")
	}
	if merge.Config.Target.RunnerGroupID != 2 {
		t.Fatalf("RunnerGroupID=%d", merge.Config.Target.RunnerGroupID)
	}

	cmdSame := &cobra.Command{}
	optsSame := sharedConfigOptions{}
	addSharedConfigFlags(cmdSame, &optsSame)
	var runnerGroupSame int64
	cmdSame.Flags().Int64Var(&runnerGroupSame, "runner-group-id", 1, "")
	if err := cmdSame.ParseFlags([]string{"--runner-group-id", "1"}); err != nil {
		t.Fatal(err)
	}
	mergeSame, err := mergeConfigureConfig(cmdSame, configureMergeInput{
		Mode:       configureModeApp,
		OutputPath: path,
		Target:     configureTargetInput{Org: "my-org", RunnerGroup: 1},
		App:        configureAppInput{BrokerURL: config.DefaultHostedAppBrokerURL},
		Opts:       optsSame,
	})
	if err != nil {
		t.Fatal(err)
	}
	if mergeSame.RunnerGroupAuthChanged {
		t.Fatal("expected no RunnerGroupAuthChanged when group value is unchanged")
	}
}

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
		Softnet:       true,
		ReclaimPolicy: config.ReclaimSoft,
		MinFreeDiskGB: 40,
	}
	if err := writeConfig(path, existing); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	opts := sharedConfigOptions{}
	addSharedConfigFlags(cmd, &opts)
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
	if !merge.Config.Softnet || merge.Config.ReclaimPolicy != config.ReclaimSoft || merge.Config.MinFreeDiskGB != 40 {
		t.Fatalf("ops fields changed without flags: softnet=%v reclaim=%q disk=%d", merge.Config.Softnet, merge.Config.ReclaimPolicy, merge.Config.MinFreeDiskGB)
	}
}

func TestMergeConfigureConfigMountsReplaceAndClear(t *testing.T) {
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
		Mounts:        []string{"/tmp/old"},
	}
	if err := writeConfig(path, existing); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	opts := sharedConfigOptions{}
	addSharedConfigFlags(cmd, &opts)
	if err := cmd.ParseFlags([]string{"--mounts", "/tmp/a", "--mounts", "/tmp/b"}); err != nil {
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
	if len(merge.Config.Mounts) != 2 || merge.Config.Mounts[0] != "/tmp/a" || merge.Config.Mounts[1] != "/tmp/b" {
		t.Fatalf("mounts=%v", merge.Config.Mounts)
	}

	cmdClear := &cobra.Command{}
	optsClear := sharedConfigOptions{}
	addSharedConfigFlags(cmdClear, &optsClear)
	if err := cmdClear.ParseFlags([]string{"--mounts="}); err != nil {
		t.Fatal(err)
	}
	cleared, err := mergeConfigureConfig(cmdClear, configureMergeInput{
		Mode:       configureModeApp,
		OutputPath: path,
		Target:     configureTargetInput{Org: "my-org", RunnerGroup: 1},
		App:        configureAppInput{BrokerURL: config.DefaultHostedAppBrokerURL},
		Opts:       optsClear,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cleared.Config.Mounts) != 0 {
		t.Fatalf("cleared mounts=%v", cleared.Config.Mounts)
	}
}

func TestMergeConfigureConfigSoftnetAndReclaim(t *testing.T) {
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
		Softnet:       true,
		ReclaimPolicy: config.ReclaimGrace,
		MinFreeDiskGB: 50,
	}
	if err := writeConfig(path, existing); err != nil {
		t.Fatal(err)
	}

	cmd := &cobra.Command{}
	opts := sharedConfigOptions{}
	addSharedConfigFlags(cmd, &opts)
	if err := cmd.ParseFlags([]string{
		"--softnet=false",
		"--reclaim-policy", "hard",
		"--reclaim-grace", "20m",
		"--min-free-disk-gb", "60",
	}); err != nil {
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
	if merge.Config.Softnet {
		t.Fatal("expected softnet disabled")
	}
	if merge.Config.ReclaimPolicy != config.ReclaimHard {
		t.Fatalf("reclaim_policy=%q", merge.Config.ReclaimPolicy)
	}
	if merge.Config.ReclaimGrace.Duration() != 20*time.Minute {
		t.Fatalf("reclaim_grace=%s", merge.Config.ReclaimGrace.Duration())
	}
	if merge.Config.MinFreeDiskGB != 60 {
		t.Fatalf("min_free_disk_gb=%d", merge.Config.MinFreeDiskGB)
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
	opts := sharedConfigOptions{}
	addSharedConfigFlags(cmd, &opts)
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
		t.Fatal("expected device flow skip")
	}
}

func TestNeedsAppDeviceFlowWhenOAuthClientChanged(t *testing.T) {
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

	if !needsAppDeviceFlow(cfg, configureMergeResult{OAuthAuthChanged: true}) {
		t.Fatal("expected device flow when oauth client id changed")
	}
}

func TestNeedsAppDeviceFlowWhenOrgChanged(t *testing.T) {
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
		Target:       config.TargetYAML("", "owner/repo", 1),
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
	opts := sharedConfigOptions{}
	addSharedConfigFlags(cmd, &opts)
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

	cmdMounts := &cobra.Command{}
	var refreshMounts bool
	cmdMounts.Flags().BoolVar(&refreshMounts, "refresh", false, "")
	optsMounts := sharedConfigOptions{}
	addSharedConfigFlags(cmdMounts, &optsMounts)
	if err := cmdMounts.ParseFlags([]string{"--refresh", "--mounts", "/tmp/cache"}); err != nil {
		t.Fatal(err)
	}
	if configureOnlyCredentialRefresh(cmdMounts, true) {
		t.Fatal("expected false when --mounts changes config")
	}
}

func TestMergeConfigureConfigRejectsNonPositiveDiskAndGrace(t *testing.T) {
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
	}
	if err := writeConfig(path, existing); err != nil {
		t.Fatal(err)
	}

	cmdDisk := &cobra.Command{}
	optsDisk := sharedConfigOptions{}
	addSharedConfigFlags(cmdDisk, &optsDisk)
	if err := cmdDisk.ParseFlags([]string{"--min-free-disk-gb", "0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeConfigureConfig(cmdDisk, configureMergeInput{
		Mode:       configureModeApp,
		OutputPath: path,
		Target:     configureTargetInput{Org: "my-org", RunnerGroup: 1},
		App:        configureAppInput{BrokerURL: config.DefaultHostedAppBrokerURL},
		Opts:       optsDisk,
	}); err == nil {
		t.Fatal("expected error for --min-free-disk-gb 0")
	}

	cmdGrace := &cobra.Command{}
	optsGrace := sharedConfigOptions{}
	addSharedConfigFlags(cmdGrace, &optsGrace)
	if err := cmdGrace.ParseFlags([]string{"--reclaim-grace", "0"}); err != nil {
		t.Fatal(err)
	}
	if _, err := mergeConfigureConfig(cmdGrace, configureMergeInput{
		Mode:       configureModeApp,
		OutputPath: path,
		Target:     configureTargetInput{Org: "my-org", RunnerGroup: 1},
		App:        configureAppInput{BrokerURL: config.DefaultHostedAppBrokerURL},
		Opts:       optsGrace,
	}); err == nil {
		t.Fatal("expected error for --reclaim-grace 0")
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
