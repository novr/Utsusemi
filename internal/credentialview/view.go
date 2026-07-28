package credentialview

import (
	"fmt"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/timefmt"
)

type Info struct {
	Mode       string `json:"mode"`
	Present    bool   `json:"present"`
	GitHubUser string `json:"github_user,omitempty"`
	ExpiresIn  string `json:"expires_in,omitempty"`
}

func Load(cfg *config.Config, store keychain.Store) (Info, error) {
	switch cfg.Registration.Mode {
	case config.ModeHostedApp:
		raw, err := store.Get(cfg.CredentialService(), cfg.CredentialAccount())
		if err != nil {
			return Info{Mode: config.ModeHostedApp}, nil
		}
		desc, err := hostcredential.Describe(raw)
		if err != nil {
			return Info{Mode: config.ModeHostedApp}, nil
		}
		return Info{
			Mode:       config.ModeHostedApp,
			Present:    true,
			GitHubUser: desc.GitHubUser,
			ExpiresIn:  timefmt.ExpiresIn(desc.HostJWTExpiresIn),
		}, nil
	case config.ModeGitHubPAT:
		_, err := store.Get(cfg.CredentialService(), cfg.CredentialAccount())
		return Info{
			Mode:    config.ModeGitHubPAT,
			Present: err == nil,
		}, nil
	default:
		return Info{}, fmt.Errorf("unsupported registration.mode %q", cfg.Registration.Mode)
	}
}
