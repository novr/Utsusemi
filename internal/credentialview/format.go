package credentialview

import (
	"fmt"

	"github.com/novr/utsusemi/internal/config"
)

func StatusLine(c Info) string {
	switch c.Mode {
	case config.ModeHostedApp:
		if c.Present {
			return fmt.Sprintf("credential: user %s, expires in %s\n", c.GitHubUser, c.ExpiresIn)
		}
		return "credential: hosted_app (not configured)\n"
	case config.ModeGitHubPAT:
		if c.Present {
			return "credential: github_pat (present)\n"
		}
		return "credential: github_pat (missing)\n"
	default:
		return fmt.Sprintf("credential: %s\n", c.Mode)
	}
}
