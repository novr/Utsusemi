package config

import (
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/target"
)

func TestValidateOK(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err != nil {
		t.Fatal(err)
	}
}

func TestValidateRejectsRunnerVersionLatest(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "LATEST",
		PoolSize:      1,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "concrete version") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRequiresSelfHostedLabel(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidatePoolSizeLimit(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      3,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "tart") {
		t.Errorf("error message should mention the provider name, got: %q", err.Error())
	}
}

func TestValidateSpawnTimeoutMustNotExceedJobTimeout(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
		SpawnTimeout:  Duration(2 * time.Hour),
		JobTimeout:    Duration(time.Hour),
	}
	ApplyDefaults(cfg)
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateHostedAppRequiresOrg(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Repo: "alice/app"},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeHostedApp, BrokerURL: "https://broker.example"},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error")
	}
}

// TestValidatePoolSizeLimitIsProviderScoped proves that pool_size validation
// uses the maxConcurrent value supplied by the selected provider, not a
// Tart-specific constant. We simulate a hypothetical second provider (e.g. a
// Linux ARM container backend) by passing maxConcurrent=5 and confirm that a
// pool_size=4 is accepted, whereas the same config fails with Tart's limit of 2.
func TestValidatePoolSizeLimitIsProviderScoped(t *testing.T) {
	cfg := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "linux"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      4,
	}

	if _, err := Validate(cfg, provider.NewStub(5)); err != nil {
		t.Fatalf("expected no error with maxConcurrent=5, got: %v", err)
	}

	_, err := Validate(cfg, provider.NewStub(2))
	if err == nil {
		t.Fatal("expected error with maxConcurrent=2, got nil")
	}
	if !strings.Contains(err.Error(), "tart") {
		t.Errorf("error message should mention the provider name, got: %q", err.Error())
	}
}

func TestValidateBrokerURL(t *testing.T) {
	if err := ValidateBrokerURL("https://broker.example"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateBrokerURL("http://127.0.0.1:8787"); err != nil {
		t.Fatal(err)
	}
	err := ValidateBrokerURL("http://localhost:8787")
	if err == nil {
		t.Fatal("expected localhost rejection")
	}
	if !strings.Contains(err.Error(), "127.0.0.1") {
		t.Fatalf("error=%v", err)
	}
	if err := ValidateBrokerURL("http://broker.example"); err == nil {
		t.Fatal("expected http rejection")
	}
}

func TestValidateHostedAppBrokerURL(t *testing.T) {
	base := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeHostedApp, BrokerURL: "http://broker.example"},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
	if _, err := Validate(base, provider.NewStub(2)); err == nil {
		t.Fatal("expected error for http broker url")
	}
	base.Registration.BrokerURL = "https://broker.example"
	if _, err := Validate(base, provider.NewStub(2)); err != nil {
		t.Fatal(err)
	}
}

func TestValidateEnv(t *testing.T) {
	ok := &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted", "macOS"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
		Env:           map[string]string{"HTTP_PROXY": "http://proxy:8080", "NO_PROXY": ""},
	}
	if _, err := Validate(ok, provider.NewStub(2)); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{name: "empty key", env: map[string]string{"": "x"}, want: "must not be empty"},
		{name: "equals in key", env: map[string]string{"FOO=BAR": "x"}, want: "must match"},
		{name: "space in key", env: map[string]string{"FOO BAR": "x"}, want: "must match"},
		{name: "dash option key", env: map[string]string{"-u": "HOME"}, want: "must match"},
		{name: "leading digit", env: map[string]string{"1FOO": "x"}, want: "must match"},
		{name: "reserved", env: map[string]string{"RUNNER_HOME": "/tmp"}, want: "reserved"},
		{name: "system proxy reserved", env: map[string]string{"UTSUSEMI_SYSTEM_PROXY_HTTP_HOST": "10.0.0.1"}, want: "reserved"},
		{name: "newline value", env: map[string]string{"FOO": "a\nb"}, want: "NUL or newline"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := *ok
			cfg.Env = tc.env
			_, err := Validate(&cfg, provider.NewStub(2))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}
}

func TestValidateSystemProxy(t *testing.T) {
	base := func() *Config {
		return &Config{
			Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
			Labels:        []string{"self-hosted", "macOS"},
			Registration:  Registration{Mode: ModeGitHubPAT},
			Provider:      "tart",
			BaseImage:     "ghcr.io/example/image:1",
			RunnerVersion: "2.336.0",
			PoolSize:      1,
		}
	}
	ok := base()
	ok.SystemProxy = &SystemProxy{
		HTTP:   "proxy.example:8080",
		HTTPS:  "[2001:db8::1]:443",
		Bypass: []string{"localhost", "*.local", "<local>"},
	}
	if _, err := Validate(ok, provider.NewStub(2)); err != nil {
		t.Fatal(err)
	}
	env := SystemProxyEnv(ok.SystemProxy)
	if env["UTSUSEMI_SYSTEM_PROXY_HTTP_HOST"] != "proxy.example" || env["UTSUSEMI_SYSTEM_PROXY_HTTP_PORT"] != "8080" {
		t.Fatalf("http endpoint = %v", env)
	}
	if env["UTSUSEMI_SYSTEM_PROXY_HTTPS_HOST"] != "2001:db8::1" || env["UTSUSEMI_SYSTEM_PROXY_HTTPS_PORT"] != "443" {
		t.Fatalf("https endpoint = %v", env)
	}
	if env["UTSUSEMI_SYSTEM_PROXY_BYPASS"] != "localhost,*.local,<local>" {
		t.Fatalf("bypass = %q", env["UTSUSEMI_SYSTEM_PROXY_BYPASS"])
	}
	if strings.Contains(env["UTSUSEMI_SYSTEM_PROXY_BYPASS"], "proxy.example") {
		t.Fatalf("bypass must not gain the proxy host: %q", env["UTSUSEMI_SYSTEM_PROXY_BYPASS"])
	}

	noBypass := base()
	noBypass.SystemProxy = &SystemProxy{HTTPS: "proxy.example:8080"}
	if _, err := Validate(noBypass, provider.NewStub(2)); err != nil {
		t.Fatal(err)
	}
	if _, ok := SystemProxyEnv(noBypass.SystemProxy)["UTSUSEMI_SYSTEM_PROXY_BYPASS"]; ok {
		t.Fatal("omitted bypass must not set BYPASS")
	}
	if _, ok := SystemProxyEnv(noBypass.SystemProxy)["UTSUSEMI_SYSTEM_PROXY_HTTP_HOST"]; ok {
		t.Fatal("http must stay unset")
	}
	dashed := base()
	dashed.SystemProxy = &SystemProxy{HTTP: "my-proxy.example:8080"}
	if _, err := Validate(dashed, provider.NewStub(2)); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		sp   *SystemProxy
		want string
	}{
		{name: "empty", sp: &SystemProxy{}, want: "requires http or https"},
		{name: "bypass only", sp: &SystemProxy{Bypass: []string{"localhost"}}, want: "requires http or https"},
		{name: "url", sp: &SystemProxy{HTTP: "http://proxy.example:8080"}, want: "host:port"},
		{name: "userinfo", sp: &SystemProxy{HTTP: "user:pass@proxy.example:8080"}, want: "host:port"},
		{name: "port", sp: &SystemProxy{HTTPS: "proxy.example:0"}, want: "1-65535"},
		{name: "cidr", sp: &SystemProxy{HTTP: "proxy.example:8080", Bypass: []string{"10.0.0.0/24"}}, want: "',' or '/'"},
		{name: "brackets", sp: &SystemProxy{HTTP: "proxy.example:8080", Bypass: []string{"<foo>"}}, want: "'<' or '>'"},
		{name: "host space", sp: &SystemProxy{HTTP: "proxy.example :8080"}, want: "whitespace or control"},
		{name: "host dash", sp: &SystemProxy{HTTP: "-proxy.example:80"}, want: "must not start with '-'"},
		{name: "host slash", sp: &SystemProxy{HTTP: "10.0.0.0/24:80"}, want: "host:port"},
		{name: "bypass empty token", sp: &SystemProxy{HTTP: "proxy.example:8080", Bypass: []string{"Empty"}}, want: "clears all exclusions"},
		{name: "bypass dash", sp: &SystemProxy{HTTP: "proxy.example:8080", Bypass: []string{"-local"}}, want: "must not start with '-'"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := base()
			cfg.SystemProxy = tc.sp
			_, err := Validate(cfg, provider.NewStub(2))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want substring %q", err, tc.want)
			}
		})
	}

	var parsed Config
	if err := yaml.Unmarshal([]byte("system_proxy: {}\n"), &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.SystemProxy == nil {
		t.Fatal("empty system_proxy mapping must be present")
	}
	parsed.Labels = []string{"self-hosted"}
	parsed.Registration.Mode = ModeGitHubPAT
	parsed.Provider = "tart"
	parsed.BaseImage = "ghcr.io/example/image:1"
	parsed.RunnerVersion = "2.336.0"
	parsed.PoolSize = 1
	parsed.Target = target.ConfigYAML{Org: "my-org", RunnerGroupID: 1}
	if _, err := Validate(&parsed, provider.NewStub(2)); err == nil {
		t.Fatal("expected empty system_proxy to fail validation")
	}
}
