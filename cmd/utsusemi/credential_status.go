package main

import (
	"fmt"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/keychain"
)

func printHostedCredentialStatus(cfg *config.Config) error {
	if cfg.Registration.Mode != config.ModeHostedApp {
		return nil
	}
	store := keychain.New()
	raw, err := store.Get(cfg.CredentialService(), cfg.CredentialAccount())
	if err != nil {
		return err
	}
	status, err := hostcredential.Describe(raw)
	if err != nil {
		return err
	}
	fmt.Printf("GitHub authorized user: %s\n", status.GitHubUser)
	fmt.Printf("Host credential expires in: %s\n", formatRemaining(status.HostJWTExpiresIn))
	return nil
}

func formatRemaining(remaining time.Duration) string {
	if remaining < 0 {
		return "expired"
	}
	return remaining.Round(time.Minute).String()
}
