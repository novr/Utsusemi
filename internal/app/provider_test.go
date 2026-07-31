package app

import (
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
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

func TestValidateConfigSkipsProviderAvailability(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := ValidateConfig(validConfig()); err != nil {
		t.Fatalf("validate config: %v", err)
	}
}
