package main

import (
	"fmt"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/credentialview"
	"github.com/novr/utsusemi/internal/keychain"
)

func printHostedCredentialStatus(cfg *config.Config) error {
	if cfg.Registration.Mode != config.ModeHostedApp {
		return nil
	}
	info, err := credentialview.Load(cfg, keychain.New())
	if err != nil {
		return err
	}
	if !info.Present {
		return fmt.Errorf("hosted app credential not configured")
	}
	fmt.Printf("GitHub authorized user: %s\n", info.GitHubUser)
	fmt.Printf("Host credential expires in: %s\n", info.ExpiresIn)
	return nil
}
