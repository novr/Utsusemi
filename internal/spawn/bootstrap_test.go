package spawn

import (
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/runnercache"
)

func TestBootstrapSpecUsesProviderRunnerArch(t *testing.T) {
	p := provider.NewStub(2)
	p.Caps.RunnerArch = "linux-arm64"

	spec := BootstrapSpecFor(&config.Config{RunnerVersion: "2.336.0"}, p)
	if spec.RunnerArch != "linux-arm64" {
		t.Fatalf("runner arch = %q", spec.RunnerArch)
	}
	env := spec.Env()
	if env["RUNNER_ARCH"] != "linux-arm64" {
		t.Fatalf("runner arch env = %q", env["RUNNER_ARCH"])
	}
}

func TestBootstrapSpecDefaultsRunnerArch(t *testing.T) {
	spec := BootstrapSpecFor(&config.Config{RunnerVersion: "2.336.0"}, provider.NewTartProvider(provider.NewFakeExecutor(), false, nil))
	if spec.RunnerArch != "osx-arm64" {
		t.Fatalf("runner arch = %q", spec.RunnerArch)
	}
	if spec.RunnerHome != defaultRunnerHome {
		t.Fatalf("runner home = %q", spec.RunnerHome)
	}
	if spec.RunnerCacheDir != runnercache.GuestCacheDir {
		t.Fatalf("runner cache dir = %q", spec.RunnerCacheDir)
	}
	env := spec.Env()
	if env["RUNNER_CACHE_DIR"] != runnercache.GuestCacheDir {
		t.Fatalf("RUNNER_CACHE_DIR = %q", env["RUNNER_CACHE_DIR"])
	}
}

func TestGuestEnvMergesConfigAndBootstrap(t *testing.T) {
	p := provider.NewStub(2)
	p.Caps.RunnerArch = "linux-arm64"
	cfg := &config.Config{
		RunnerVersion: "2.336.0",
		Env: map[string]string{
			"HTTP_PROXY":     "http://proxy:8080",
			"RUNNER_VERSION": "should-not-win",
		},
	}
	env := GuestEnv(cfg, p)
	if env["HTTP_PROXY"] != "http://proxy:8080" {
		t.Fatalf("HTTP_PROXY = %q", env["HTTP_PROXY"])
	}
	if env["RUNNER_VERSION"] != "2.336.0" {
		t.Fatalf("RUNNER_VERSION = %q, want bootstrap win", env["RUNNER_VERSION"])
	}
	if env["RUNNER_ARCH"] != "linux-arm64" {
		t.Fatalf("RUNNER_ARCH = %q", env["RUNNER_ARCH"])
	}
}

func TestGuestEnvNilConfig(t *testing.T) {
	env := GuestEnv(nil, provider.NewStub(2))
	if env["RUNNER_HOME"] != defaultRunnerHome {
		t.Fatalf("RUNNER_HOME = %q", env["RUNNER_HOME"])
	}
}
