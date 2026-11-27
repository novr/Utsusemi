package config

import (
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/provider"
)

func validProxyForwardCfg(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Target:        TargetYAML("acme", "", 1),
		Labels:        []string{"self-hosted"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "img:1",
		RunnerVersion: "2.338.2",
		PoolSize:      1,
		ProxyForward: &ProxyForward{
			Listen:   "127.0.0.1:8888",
			Upstream: "proxy.example:8080",
		},
	}
}

func TestValidateProxyForwardOK(t *testing.T) {
	cfg := validProxyForwardCfg(t)
	p := provider.NewStub(2)
	if _, err := Validate(cfg, p); err != nil {
		t.Fatal(err)
	}
	cfg.ProxyForward.Upstream = "127.0.0.1:8888"
	if _, err := Validate(cfg, p); err != nil {
		t.Fatalf("same host/guest loopback port should be allowed: %v", err)
	}
	cfg.Softnet = true
	if _, err := Validate(cfg, p); err != nil {
		t.Fatalf("softnet with proxy_forward should be allowed: %v", err)
	}
}

func TestApplyProxyForwardDefaultsOnlyFillsMissingValues(t *testing.T) {
	cfg := validProxyForwardCfg(t)
	ApplyProxyForwardDefaults(cfg)

	if got := cfg.Env["http_proxy"]; got != "http://127.0.0.1:8888" {
		t.Fatalf("http_proxy=%q", got)
	}
	if got := cfg.Env["https_proxy"]; got != "http://127.0.0.1:8888" {
		t.Fatalf("https_proxy=%q", got)
	}
	if got := cfg.Env["no_proxy"]; got != "localhost,127.0.0.1" {
		t.Fatalf("no_proxy=%q", got)
	}
	if cfg.SystemProxy != nil {
		t.Fatalf("system_proxy should remain opt-in: %+v", cfg.SystemProxy)
	}

	cfg = validProxyForwardCfg(t)
	cfg.Env = map[string]string{
		"HTTP_PROXY": "http://explicit-http:8080",
		"no_proxy":   "example.test",
	}
	cfg.SystemProxy = &SystemProxy{
		HTTP:   "explicit-http:8080",
		Bypass: []string{"example.test"},
	}
	ApplyProxyForwardDefaults(cfg)

	wantEnv := map[string]string{
		"HTTP_PROXY":  "http://explicit-http:8080",
		"https_proxy": "http://127.0.0.1:8888",
		"no_proxy":    "example.test",
	}
	for key, want := range wantEnv {
		if got := cfg.Env[key]; got != want {
			t.Fatalf("env[%q]=%q, want %q", key, got, want)
		}
	}
	if got := cfg.SystemProxy.HTTP; got != "explicit-http:8080" {
		t.Fatalf("system_proxy.http=%q", got)
	}
	if got := cfg.SystemProxy.HTTPS; got != "127.0.0.1:8888" {
		t.Fatalf("system_proxy.https=%q", got)
	}
	if len(cfg.SystemProxy.Bypass) != 1 || cfg.SystemProxy.Bypass[0] != "example.test" {
		t.Fatalf("system_proxy.bypass=%v", cfg.SystemProxy.Bypass)
	}
}

func TestApplyProxyForwardDefaultsDisabledWithoutForwarder(t *testing.T) {
	cfg := &Config{Env: map[string]string{"http_proxy": "http://explicit:8080"}}
	ApplyProxyForwardDefaults(cfg)
	if len(cfg.Env) != 1 || cfg.Env["http_proxy"] != "http://explicit:8080" {
		t.Fatalf("env changed without proxy_forward: %v", cfg.Env)
	}
}

func TestValidateProxyForwardRejects(t *testing.T) {
	cases := []struct {
		name string
		mut  func(*Config)
		want string
	}{
		{"listen non-loopback", func(c *Config) { c.ProxyForward.Listen = "192.168.64.1:8888" }, "loopback"},
		{"listen unspecified", func(c *Config) { c.ProxyForward.Listen = "0.0.0.0:8888" }, "loopback"},
		{"listen hostname", func(c *Config) { c.ProxyForward.Listen = "bridge.local:8888" }, "loopback"},
		{"listen localhost", func(c *Config) { c.ProxyForward.Listen = "localhost:8888" }, "127.0.0.1"},
		{"upstream localhost", func(c *Config) { c.ProxyForward.Upstream = "localhost:8080" }, "127.0.0.1"},
		{"upstream unspecified", func(c *Config) { c.ProxyForward.Upstream = "0.0.0.0:8080" }, "0.0.0.0"},
		{"action cache port conflict", func(c *Config) {
			c.ActionCache = true
			c.ProxyForward.Listen = "127.0.0.1:19527"
		}, "action_cache"},
		{"scheme", func(c *Config) { c.ProxyForward.Upstream = "http://proxy.example:8080" }, "host:port"},
		{"empty listen", func(c *Config) { c.ProxyForward.Listen = "" }, "required"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := validProxyForwardCfg(t)
			tc.mut(cfg)
			_, err := Validate(cfg, provider.NewStub(2))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v want substring %q", err, tc.want)
			}
		})
	}
}

func TestCloneProxyForwardIndependent(t *testing.T) {
	cfg := &Config{
		ProxyForward: &ProxyForward{Listen: "127.0.0.1:8888", Upstream: "proxy.example:8080"},
	}
	got := Clone(cfg)
	cfg.ProxyForward.Listen = "127.0.0.1:9999"
	if got.ProxyForward.Listen != "127.0.0.1:8888" {
		t.Fatalf("proxy forward leaked: %+v", got.ProxyForward)
	}
}
