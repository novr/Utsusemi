package credentialview

import (
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
)

func TestLoadPATMissing(t *testing.T) {
	cfg := &config.Config{
		Registration: config.Registration{Mode: config.ModeGitHubPAT},
	}
	config.ApplyDefaults(cfg)
	info, err := Load(cfg, keychain.NewMemoryStore())
	if err != nil {
		t.Fatal(err)
	}
	if info.Present {
		t.Fatalf("info=%+v", info)
	}
}
