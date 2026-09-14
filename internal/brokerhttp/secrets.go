package brokerhttp

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
)

const (
	DefaultKeychainService = "utsusemi-broker"
	DefaultJWTIssuer       = "utsusemi-broker"
	DefaultJWTVersion      = "1"

	EnvGitHubAppID                     = "GITHUB_APP_ID"
	EnvGitHubAppPrivateKey             = "GITHUB_APP_PRIVATE_KEY"
	EnvGitHubAppPrivateKeyFile         = "UTSUSEMI_GITHUB_APP_PRIVATE_KEY_FILE"
	EnvCredentialSigningPrivateKey     = "CREDENTIAL_SIGNING_PRIVATE_KEY"
	EnvCredentialSigningPrivateKeyFile = "UTSUSEMI_CREDENTIAL_SIGNING_KEY_FILE"
	EnvJWTIssuer                       = "JWT_ISSUER"
	EnvJWTVersion                      = "JWT_VERSION"
)

type Secrets struct {
	GitHubAppID         string `json:"github_app_id"`
	GitHubAppPrivateKey string `json:"github_app_private_key"`
	SigningKeyPEM       string `json:"credential_signing_private_key"`
	JWTIssuer           string `json:"jwt_issuer"`
	JWTVersion          string `json:"jwt_version"`
}

type SourceConfig struct {
	AppID             string
	AppPrivateKeyFile string
	SigningKeyFile    string
	JWTIssuer         string
	JWTVersion        string
	EnvFile           string
}

func LoadBrokerSecrets(cfg SourceConfig) (Secrets, error) {
	if runtime.GOOS != "darwin" {
		return cfg.resolveFromEnv()
	}
	if cfg.prefersEnvSource() {
		return cfg.resolveFromEnv()
	}
	return cfg.loadDarwinKeychain()
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
	return s.withJWTDefaults(), nil
}

func SaveSecrets(store keychain.Store, service, account string, s Secrets) error {
	s = s.withJWTDefaults()
	data, err := json.Marshal(s)
	if err != nil {
		return err
	}
	return store.Set(service, account, string(data))
}

func (s Secrets) withJWTDefaults() Secrets {
	if s.JWTIssuer == "" {
		s.JWTIssuer = DefaultJWTIssuer
	}
	if s.JWTVersion == "" {
		s.JWTVersion = DefaultJWTVersion
	}
	return s
}

func (s Secrets) Env() Env {
	d := s.withJWTDefaults()
	return Env{
		GitHubAppID:         d.GitHubAppID,
		GitHubAppPrivateKey: d.GitHubAppPrivateKey,
		SigningKeyPEM:       d.SigningKeyPEM,
		JWTIssuer:           d.JWTIssuer,
		JWTVersion:          d.JWTVersion,
	}
}

func (c SourceConfig) prefersEnvSource() bool {
	if strings.TrimSpace(c.EnvFile) != "" {
		return true
	}
	return envAppCredentials()
}

func (c SourceConfig) resolveFromEnv() (Secrets, error) {
	if strings.TrimSpace(c.EnvFile) != "" {
		if err := loadEnvFile(c.EnvFile); err != nil {
			return Secrets{}, err
		}
	}
	appID := firstNonEmpty(c.AppID, os.Getenv(EnvGitHubAppID))
	if strings.TrimSpace(appID) == "" {
		return Secrets{}, fmt.Errorf("broker github app id missing; set %s or pass --app-id", EnvGitHubAppID)
	}
	appKeyPEM, err := readSecretPEM(c.AppPrivateKeyFile, EnvGitHubAppPrivateKeyFile, EnvGitHubAppPrivateKey, "GitHub App private key")
	if err != nil {
		return Secrets{}, err
	}
	signingPEM, err := readSigningKeyPEM(c.SigningKeyFile, EnvCredentialSigningPrivateKeyFile, EnvCredentialSigningPrivateKey)
	if err != nil {
		return Secrets{}, err
	}
	return c.secretsFromPEMs(appID, appKeyPEM, signingPEM)
}

func (c SourceConfig) loadDarwinKeychain() (Secrets, error) {
	store := keychain.New()
	service, account := brokerKeychainTarget()
	existing, loadErr := LoadSecrets(store, service, account)

	if loadErr == nil && !c.hasAppBootstrapFlags() {
		if _, err := ValidateSigningKeyPEM(existing.SigningKeyPEM); err != nil {
			return Secrets{}, err
		}
		return existing, nil
	}
	if !c.hasAppBootstrapFlags() {
		return Secrets{}, fmt.Errorf("broker credentials missing; pass --app-id and --app-private-key-file")
	}

	appKeyPEM, err := readPEMFile(c.AppPrivateKeyFile, "GitHub App private key")
	if err != nil {
		return Secrets{}, err
	}

	secrets := Secrets{}
	if loadErr == nil {
		secrets = existing
	}
	secrets.GitHubAppID = strings.TrimSpace(c.AppID)
	secrets.GitHubAppPrivateKey = appKeyPEM
	secrets = c.applyJWTFields(secrets)

	if c.SigningKeyFile != "" {
		signPEM, err := readSigningKeyPEM(c.SigningKeyFile, EnvCredentialSigningPrivateKeyFile, EnvCredentialSigningPrivateKey)
		if err != nil {
			return Secrets{}, err
		}
		secrets.SigningKeyPEM = signPEM
	}
	if strings.TrimSpace(secrets.SigningKeyPEM) == "" {
		pem, err := GenerateSigningKeyPEM()
		if err != nil {
			return Secrets{}, err
		}
		secrets.SigningKeyPEM = pem
	}

	if err := SaveSecrets(store, service, account, secrets); err != nil {
		return Secrets{}, err
	}
	return secrets, nil
}

func (c SourceConfig) secretsFromPEMs(appID, appKeyPEM, signingPEM string) (Secrets, error) {
	return c.applyJWTFields(Secrets{
		GitHubAppID:         strings.TrimSpace(appID),
		GitHubAppPrivateKey: appKeyPEM,
		SigningKeyPEM:       signingPEM,
	}), nil
}

func (c SourceConfig) hasAppBootstrapFlags() bool {
	return strings.TrimSpace(c.AppID) != "" && c.AppPrivateKeyFile != ""
}

func (c SourceConfig) applyJWTFields(s Secrets) Secrets {
	s.JWTIssuer = firstNonEmpty(c.JWTIssuer, os.Getenv(EnvJWTIssuer), s.JWTIssuer, DefaultJWTIssuer)
	s.JWTVersion = firstNonEmpty(c.JWTVersion, os.Getenv(EnvJWTVersion), s.JWTVersion, DefaultJWTVersion)
	return s
}

func brokerKeychainTarget() (string, string) {
	return DefaultKeychainService, config.DefaultCredentialAccount
}

func envAppKeyMaterial() bool {
	return strings.TrimSpace(os.Getenv(EnvGitHubAppPrivateKeyFile)) != "" ||
		strings.TrimSpace(os.Getenv(EnvGitHubAppPrivateKey)) != ""
}

func envAppCredentials() bool {
	return strings.TrimSpace(os.Getenv(EnvGitHubAppID)) != "" && envAppKeyMaterial()
}

func readSigningKeyPEM(flagFile, envFileKey, envInlineKey string) (string, error) {
	pem, err := readSecretPEM(flagFile, envFileKey, envInlineKey, "credential signing key")
	if err != nil {
		return "", err
	}
	if _, err := ValidateSigningKeyPEM(pem); err != nil {
		return "", err
	}
	return pem, nil
}

func readSecretPEM(flagFile, envFileKey, envInlineKey, label string) (string, error) {
	if path := firstNonEmpty(flagFile, os.Getenv(envFileKey)); path != "" {
		return readPEMFile(path, label)
	}
	if inline := strings.TrimSpace(os.Getenv(envInlineKey)); inline != "" {
		return inline, nil
	}
	return "", fmt.Errorf("broker %s missing; set %s / %s or pass a key file flag", label, envFileKey, envInlineKey)
}

func readPEMFile(path, label string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read %s file: %w", label, err)
	}
	pem := strings.TrimSpace(string(data))
	if pem == "" {
		return "", fmt.Errorf("broker %s file is empty", label)
	}
	return pem, nil
}

func loadEnvFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read env file: %w", err)
	}
	for lineNum, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("env file %s:%d: expected KEY=VALUE", path, lineNum+1)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return fmt.Errorf("env file %s:%d: empty key", path, lineNum+1)
		}
		if err := os.Setenv(key, value); err != nil {
			return fmt.Errorf("env file %s:%d: %w", path, lineNum+1, err)
		}
	}
	return nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
