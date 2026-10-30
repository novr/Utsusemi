package credentialview

import (
	"fmt"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
)

type missingKeychainError struct {
	hint string
}

func (e *missingKeychainError) Error() string { return e.hint }

func (e *missingKeychainError) Unwrap() error { return keychain.ErrNotFound }

// MissingKeychainError is the operator-facing failure when the configured
// keychain service has no item. Error text is only the hint; Unwrap is
// keychain.ErrNotFound.
func MissingKeychainError(mode, service string) error {
	return &missingKeychainError{
		hint: fmt.Sprintf("credential missing from keychain service %q — %s", service, reconfigureClause(mode)),
	}
}

func KeychainServiceChanged(mode, from, to string) string {
	return fmt.Sprintf("credential_keychain_service changed from %q to %q; changing it does not move the Keychain item — %s", from, to, reconfigureClause(mode))
}

func reconfigureClause(mode string) string {
	switch mode {
	case config.ModeGitHubPAT:
		return "run `utsusemi configure token` with the token on stdin or --token"
	case config.ModeHostedApp:
		return "run `utsusemi configure app`"
	default:
		return "run `utsusemi configure app`, or `utsusemi configure token` with the token on stdin or --token"
	}
}
