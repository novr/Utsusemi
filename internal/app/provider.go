package app

import (
	"fmt"
	"strings"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/target"
)

// ValidateConfig validates config shape and pool limits using provider capabilities.
// It does not require the provider runtime (e.g. tart in PATH) to be present.
func ValidateConfig(cfg *config.Config) (target.Target, error) {
	p, err := buildProvider(cfg, provider.RealExecutor{}, false)
	if err != nil {
		return target.Target{}, err
	}
	return config.Validate(cfg, p)
}

func newProvider(cfg *config.Config, exec provider.CommandExecutor) (provider.VMProvider, error) {
	return buildProvider(cfg, exec, true)
}

func buildProvider(cfg *config.Config, exec provider.CommandExecutor, requireAvailable bool) (provider.VMProvider, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}
	name := strings.TrimSpace(cfg.Provider)
	if name == "" {
		name = "tart"
	}
	switch name {
	case "tart":
		p := provider.NewTartProvider(exec, cfg.Softnet)
		if requireAvailable {
			if err := p.Available(); err != nil {
				return nil, err
			}
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unsupported provider %q", cfg.Provider)
	}
}
