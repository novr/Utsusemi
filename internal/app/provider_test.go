package app

import (
	"context"
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/runnercache"
	"github.com/novr/utsusemi/internal/target"
)

func validConfig() *config.Config {
	return &config.Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  config.Registration{Mode: config.ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
}

func TestBuildProviderUnsupported(t *testing.T) {
	cfg := &config.Config{Provider: "qemu"}
	_, err := buildProvider(cfg, provider.NewFakeExecutor(), false)
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBuildProviderDefaultsToTartCapabilities(t *testing.T) {
	p, err := buildProvider(&config.Config{}, provider.NewFakeExecutor(), false)
	if err != nil {
		t.Fatal(err)
	}
	caps := p.Capabilities()
	if caps.MaxConcurrent != 2 {
		t.Fatalf("max concurrent=%d", caps.MaxConcurrent)
	}
	if caps.RunnerArch != "osx-arm64" {
		t.Fatalf("runner arch=%q", caps.RunnerArch)
	}
}

func TestBuildProviderInjectsRunnerCacheMount(t *testing.T) {
	state := t.TempDir()
	userMount := t.TempDir()
	exec := provider.NewFakeExecutor()
	p, err := buildProvider(&config.Config{
		StateDir: state,
		Mounts:   []string{userMount + ":ro"},
	}, exec, false)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Start(context.Background(), "vm-1"); err != nil {
		t.Fatal(err)
	}
	var startArgs []string
	for _, c := range exec.Calls {
		if c.Name == "tart" && len(c.Args) > 0 && c.Args[0] == "run" {
			startArgs = c.Args
			break
		}
	}
	if startArgs == nil {
		t.Fatal("tart run not recorded")
	}
	entry := runnercache.MountEntry(state)
	foundCache, foundUser := false, false
	for _, a := range startArgs {
		if a == "--dir="+entry {
			foundCache = true
		}
		if a == "--dir="+userMount+":ro" {
			foundUser = true
		}
	}
	if !foundCache {
		t.Fatalf("missing runner cache mount in %v", startArgs)
	}
	if !foundUser {
		t.Fatalf("missing user mount in %v", startArgs)
	}
}

func TestValidateConfigSkipsProviderAvailability(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := ValidateConfig(validConfig()); err != nil {
		t.Fatalf("validate config: %v", err)
	}
}
