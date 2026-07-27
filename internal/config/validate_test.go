package config

import (
	"testing"

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
	_, err := Validate(cfg, 2)
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
	_, err := Validate(cfg, 2)
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
	_, err := Validate(cfg, 2)
	if err == nil {
		t.Fatal("expected error")
	}
}
