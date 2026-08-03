package spawn

import (
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
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
}
