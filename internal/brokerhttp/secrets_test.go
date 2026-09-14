package brokerhttp

import (
	"os"
	"path/filepath"
	"testing"
)

func writeBrokerPEMFixtures(t *testing.T) (appPEM, signPEM string) {
	t.Helper()
	dir := t.TempDir()
	appPEM = filepath.Join(dir, "app.pem")
	signPEM = filepath.Join(dir, "sign.pem")
	if err := os.WriteFile(appPEM, []byte("-----BEGIN RSA PRIVATE KEY-----\napp\n-----END RSA PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	signingKey, err := GenerateSigningKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(signPEM, []byte(signingKey), 0o600); err != nil {
		t.Fatal(err)
	}
	return appPEM, signPEM
}

func TestSourceConfigResolveFromFiles(t *testing.T) {
	appPEM, signPEM := writeBrokerPEMFixtures(t)
	t.Setenv(EnvGitHubAppID, "42")
	t.Setenv(EnvGitHubAppPrivateKeyFile, appPEM)
	t.Setenv(EnvCredentialSigningPrivateKeyFile, signPEM)

	got, err := (SourceConfig{}).resolveFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got.GitHubAppID != "42" || got.GitHubAppPrivateKey == "" || got.SigningKeyPEM == "" {
		t.Fatalf("unexpected secrets: %+v", got)
	}
	if got.JWTIssuer != DefaultJWTIssuer || got.JWTVersion != DefaultJWTVersion {
		t.Fatalf("defaults: %+v", got)
	}
}

func TestSourceConfigResolveRequiresSigningKey(t *testing.T) {
	dir := t.TempDir()
	appPEM := filepath.Join(dir, "app.pem")
	if err := os.WriteFile(appPEM, []byte("app"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvGitHubAppID, "42")
	t.Setenv(EnvGitHubAppPrivateKeyFile, appPEM)
	t.Setenv(EnvCredentialSigningPrivateKeyFile, "")
	t.Setenv(EnvCredentialSigningPrivateKey, "")

	if _, err := (SourceConfig{}).resolveFromEnv(); err == nil {
		t.Fatal("expected missing signing key error")
	}
}

func TestSourceConfigResolveJWTIssuerFromEnv(t *testing.T) {
	appPEM, signPEM := writeBrokerPEMFixtures(t)
	envPath := filepath.Join(filepath.Dir(appPEM), "broker.env")
	if err := os.WriteFile(envPath, []byte("GITHUB_APP_ID=42\nJWT_ISSUER=custom-issuer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvGitHubAppPrivateKeyFile, appPEM)
	t.Setenv(EnvCredentialSigningPrivateKeyFile, signPEM)

	got, err := (SourceConfig{EnvFile: envPath}).resolveFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if got.JWTIssuer != "custom-issuer" {
		t.Fatalf("issuer=%q", got.JWTIssuer)
	}
}

func TestSourceConfigPrefersEnvSourceIgnoresAppIDAlone(t *testing.T) {
	t.Setenv(EnvGitHubAppID, "42")
	t.Setenv(EnvGitHubAppPrivateKey, "")
	t.Setenv(EnvGitHubAppPrivateKeyFile, "")

	if (SourceConfig{}).prefersEnvSource() {
		t.Fatal("GITHUB_APP_ID alone must not select env source")
	}
}

func TestSourceConfigPrefersEnvSourceIgnoresSigningKeyFileAlone(t *testing.T) {
	if (SourceConfig{SigningKeyFile: "/tmp/sign.pem"}).prefersEnvSource() {
		t.Fatal("signing key file alone must not select env source")
	}
}

func TestSourceConfigPrefersEnvSourceIgnoresBootstrapFlags(t *testing.T) {
	cfg := SourceConfig{AppID: "42", AppPrivateKeyFile: "/tmp/app.pem"}
	if cfg.prefersEnvSource() {
		t.Fatal("bootstrap flags must use keychain on darwin")
	}
}

func TestSourceConfigPrefersEnvSourceIgnoresStaleKeyFileEnv(t *testing.T) {
	t.Setenv(EnvGitHubAppID, "")
	t.Setenv(EnvGitHubAppPrivateKeyFile, "/tmp/stale-app.pem")

	if (SourceConfig{}).prefersEnvSource() {
		t.Fatal("app key file env without app id must not select env source")
	}
}

func TestSourceConfigPrefersEnvSourceWithEnvFile(t *testing.T) {
	if !(SourceConfig{EnvFile: "/etc/utsusemi-broker/env"}).prefersEnvSource() {
		t.Fatal("env file must select env source")
	}
}

func TestSourceConfigPrefersEnvSourceWithInlineKeys(t *testing.T) {
	t.Setenv(EnvGitHubAppID, "42")
	t.Setenv(EnvGitHubAppPrivateKey, "pem")
	t.Setenv(EnvGitHubAppPrivateKeyFile, "")

	if !(SourceConfig{}).prefersEnvSource() {
		t.Fatal("expected env source")
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broker.env")
	if err := os.WriteFile(path, []byte("# comment\nGITHUB_APP_ID=99\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(EnvGitHubAppID, "")
	if err := loadEnvFile(path); err != nil {
		t.Fatal(err)
	}
	if os.Getenv(EnvGitHubAppID) != "99" {
		t.Fatalf("got %q", os.Getenv(EnvGitHubAppID))
	}
}
