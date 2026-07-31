package config

import (
	"strings"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/target"
)

func TestValidateOK(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRequiresSelfHostedLabel(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidatePoolSizeLimit(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      3,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tart") {
		t.Errorf("error message should mention the provider name, got: %q", err.Error())
	}
}

func TestValidateSpawnTimeoutMustNotExceedJobTimeout(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
		SpawnTimeout:  Duration(2 * time.Hour),
		JobTimeout:    Duration(time.Hour),
	}
	ApplyDefaults(cfg)
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateHostedAppRequiresOrg(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Repo: "alice/app"},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeHostedApp, BrokerURL: "https://broker.example"},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestValidatePoolSizeLimitIsProviderScoped proves that pool_size validation
// uses the maxConcurrent value supplied by the selected provider, not a
// Tart-specific constant. We simulate a hypothetical second provider (e.g. a
// Linux ARM container backend) by passing maxConcurrent=5 and confirm that a
// pool_size=4 is accepted, whereas the same config fails with Tart's limit of 2.
func TestValidatePoolSizeLimitIsProviderScoped(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "linux"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      4,
	}

	if _, err := Validate(cfg, provider.NewStub(5)); err != nil {
		t.Fatalf("expected no error with maxConcurrent=5, got: %v", err)
	}

	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error with maxConcurrent=2, got nil")
	}
	if !strings.Contains(err.Error(), "tart") {
		t.Errorf("error message should mention the provider name, got: %q", err.Error())
	}
}

func TestValidateHostedAppBrokerURL(t *testing.T) {
	base := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeHostedApp, BrokerURL: "http://broker.example"},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	if _, err := Validate(base, provider.NewStub(2)); err == nil {
		t.Fatal("expected error for http broker url")
	}
	base.Registration.BrokerURL = "https://broker.example"
	if _, err := Validate(base, provider.NewStub(2)); err != nil {
		t.Fatal(err)
	}
}
