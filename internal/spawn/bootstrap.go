package spawn

import (
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
)

const (
	defaultRunnerHome = "/Users/admin/actions-runner"
	defaultRunnerArch = "osx-arm64"
)

type BootstrapSpec struct {
	RunnerVersion string
	RunnerArch    string
	RunnerHome    string
}

func BootstrapSpecFor(cfg *config.Config, p provider.VMProvider) BootstrapSpec {
	spec := BootstrapSpec{
		RunnerArch: defaultRunnerArch,
		RunnerHome: defaultRunnerHome,
	}
	if cfg != nil {
		spec.RunnerVersion = cfg.RunnerVersion
	}
	if p != nil {
		if arch := p.Capabilities().RunnerArch; arch != "" {
			spec.RunnerArch = arch
		}
	}
	return spec
}

func (s BootstrapSpec) Env() map[string]string {
	return map[string]string{
		"RUNNER_VERSION": s.RunnerVersion,
		"RUNNER_ARCH":    s.RunnerArch,
		"RUNNER_HOME":    s.RunnerHome,
	}
}

func BootstrapEnv(cfg *config.Config, p provider.VMProvider) map[string]string {
	return BootstrapSpecFor(cfg, p).Env()
}
