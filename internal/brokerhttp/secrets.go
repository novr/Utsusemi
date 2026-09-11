package brokerhttp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/novr/utsusemi/internal/keychain"
)

const (
	DefaultKeychainService = "utsusemi-broker"
	DefaultJWTIssuer       = "utsusemi-broker"
	DefaultJWTVersion      = "1"
)

type Secrets struct {
	GitHubAppID         string `json:"github_app_id"`
	GitHubAppPrivateKey string `json:"github_app_private_key"`
	SigningKeyPEM       string `json:"credential_signing_private_key"`
	JWTIssuer           string `json:"jwt_issuer"`
	JWTVersion          string `json:"jwt_version"`
}

func LoadSecrets(store keychain.Store, service, account string) (Secrets, error) {
	raw, err := store.Get(service, account)
	if err != nil {
		return Secrets{}, err
	}
	var s Secrets
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return Secrets{}, fmt.Errorf("parse broker secrets: %w", err)
	}
	if strings.TrimSpace(s.GitHubAppID) == "" || strings.TrimSpace(s.GitHubAppPrivateKey) == "" {
		return Secrets{}, fmt.Errorf("broker github app credentials missing from keychain")
	}
	if strings.TrimSpace(s.SigningKeyPEM) == "" {
		return Secrets{}, fmt.Errorf("broker credential signing key missing from keychain")
	}
	if s.JWTIssuer == "" {
		s.JWTIssuer = DefaultJWTIssuer
	}
	if s.JWTVersion == "" {
		s.JWTVersion = DefaultJWTVersion
	}
	return s, nil
}

func SaveSecrets(store keychain.Store, service, account string, s Secrets) error {
	if s.JWTIssuer == "" {
		s.JWTIssuer = DefaultJWTIssuer
	}
	if s.JWTVersion == "" {
		s.JWTVersion = DefaultJWTVersion
	}
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return store.Set(service, account, string(data))
}

func (s Secrets) Env() Env {
	return Env{
		GitHubAppID:         s.GitHubAppID,
		GitHubAppPrivateKey: s.GitHubAppPrivateKey,
		SigningKeyPEM:       s.SigningKeyPEM,
		JWTIssuer:           s.JWTIssuer,
		JWTVersion:          s.JWTVersion,
	}
}
