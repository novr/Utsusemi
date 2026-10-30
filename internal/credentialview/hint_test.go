package credentialview

import (
	"errors"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
)

func TestMissingKeychainError(t *testing.T) {
	tests := []struct {
		mode string
		want string
	}{
		{
			mode: config.ModeHostedApp,
			want: "credential missing from keychain service \"repo-a\" — run `utsusemi configure app`",
		},
		{
			mode: config.ModeGitHubPAT,
			want: "credential missing from keychain service \"repo-a\" — run `utsusemi configure token` with the token on stdin or --token",
		},
	}
	for _, tc := range tests {
		err := MissingKeychainError(tc.mode, "repo-a")
		if !errors.Is(err, keychain.ErrNotFound) {
			t.Fatalf("mode=%s errors.Is=%v", tc.mode, err)
		}
		if err.Error() != tc.want {
			t.Fatalf("mode=%s msg=%q", tc.mode, err.Error())
		}
	}
}

func TestReconfigureClauseUnknownModeNamesBothCommands(t *testing.T) {
	err := MissingKeychainError("other", "repo-a")
	msg := err.Error()
	if !strings.Contains(msg, "utsusemi configure app") || !strings.Contains(msg, "utsusemi configure token") {
		t.Fatalf("msg=%q", msg)
	}
}

func TestKeychainServiceChanged(t *testing.T) {
	hosted := KeychainServiceChanged(config.ModeHostedApp, "utsusemi-credential", "repo-a")
	if hosted != "credential_keychain_service changed from \"utsusemi-credential\" to \"repo-a\"; changing it does not move the Keychain item — run `utsusemi configure app`" {
		t.Fatalf("hosted=%q", hosted)
	}
	pat := KeychainServiceChanged(config.ModeGitHubPAT, "old", "new")
	if !strings.Contains(pat, "from \"old\" to \"new\"") || !strings.Contains(pat, "utsusemi configure token") || strings.Contains(pat, "configure app") {
		t.Fatalf("pat=%q", pat)
	}
}
