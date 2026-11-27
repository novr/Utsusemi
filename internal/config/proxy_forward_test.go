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

func TestValidateProxyForwardDefaultsListen(t *testing.T) {
	cfg := validProxyForwardCfg(t)
	cfg.ProxyForward.Listen = ""
	if _, err := Validate(cfg, provider.NewStub(2)); err != nil {
		t.Fatalf("omitted listen should use the default: %v", err)
	}

	cfg.ProxyForward.Upstream = ""
	if _, err := Validate(cfg, provider.NewStub(2)); err == nil || !strings.Contains(err.Error(), "upstream") {
		t.Fatalf("missing upstream error=%v", err)
	}
}

func TestApplyProxyForwardDefaultsFillsEnvOnly(t *testing.T) {
	cfg := validProxyForwardCfg(t)
	ApplyProxyForwardDefaults(cfg)

	want := map[string]string{
		"http_proxy":  "http://127.0.0.1:8888",
		"https_proxy": "http://127.0.0.1:8888",
		"no_proxy":    "localhost,127.0.0.1",
	}
	if len(cfg.Env) != len(want) {
		t.Fatalf("env=%v", cfg.Env)
	}
	for k, v := range want {
		if cfg.Env[k] != v {
			t.Fatalf("env[%q]=%q, want %q", k, cfg.Env[k], v)
		}
	}
	if cfg.SystemProxy != nil {
		t.Fatalf("system_proxy must stay opt-in: %+v", cfg.SystemProxy)
	}
}

func TestApplyProxyForwardDefaultsKeepsExplicitEnv(t *testing.T) {
	cfg := validProxyForwardCfg(t)
	cfg.Env = map[string]string{
		"HTTP_PROXY": "http://explicit:8080",
		"no_proxy":   "example.test",
	}
	ApplyProxyForwardDefaults(cfg)

	want := map[string]string{
		"HTTP_PROXY":  "http://explicit:8080",
		"https_proxy": "http://127.0.0.1:8888",
		"no_proxy":    "example.test",
	}
	if len(cfg.Env) != len(want) {
		t.Fatalf("env=%v", cfg.Env)
	}
	for k, v := range want {
		if cfg.Env[k] != v {
			t.Fatalf("env[%q]=%q, want %q", k, cfg.Env[k], v)
		}
	}
}

func TestApplyProxyForwardDefaultsSystemProxy(t *testing.T) {
	t.Run("nested opt in", func(t *testing.T) {
		cfg := validProxyForwardCfg(t)
		cfg.ProxyForward.Listen = ""
		cfg.ProxyForward.SystemProxy = true
		ApplyProxyForwardDefaults(cfg)
		if cfg.ProxyForward.Listen != DefaultProxyForwardListen {
			t.Fatalf("listen=%q", cfg.ProxyForward.Listen)
		}
		if cfg.SystemProxy == nil || cfg.SystemProxy.HTTP != DefaultProxyForwardListen || cfg.SystemProxy.HTTPS != DefaultProxyForwardListen {
			t.Fatalf("system_proxy=%+v", cfg.SystemProxy)
		}
	})
	t.Run("empty block opts in", func(t *testing.T) {
		cfg := validProxyForwardCfg(t)
		cfg.SystemProxy = &SystemProxy{}
		ApplyProxyForwardDefaults(cfg)
		sp := cfg.SystemProxy
		if sp.HTTP != "127.0.0.1:8888" || sp.HTTPS != "127.0.0.1:8888" {
			t.Fatalf("endpoints=%q %q", sp.HTTP, sp.HTTPS)
		}
		if len(sp.Bypass) != 2 || sp.Bypass[0] != "localhost" || sp.Bypass[1] != "127.0.0.1" {
			t.Fatalf("bypass=%v", sp.Bypass)
		}
	})
	t.Run("bypass-only block keeps bypass", func(t *testing.T) {
		cfg := validProxyForwardCfg(t)
		cfg.SystemProxy = &SystemProxy{Bypass: []string{"example.test"}}
		ApplyProxyForwardDefaults(cfg)
		sp := cfg.SystemProxy
		if sp.HTTP != "127.0.0.1:8888" || len(sp.Bypass) != 1 || sp.Bypass[0] != "example.test" {
			t.Fatalf("system_proxy=%+v", sp)
		}
	})
	t.Run("explicit endpoint is untouched", func(t *testing.T) {
		cfg := validProxyForwardCfg(t)
		cfg.SystemProxy = &SystemProxy{HTTPS: "explicit:8080"}
		ApplyProxyForwardDefaults(cfg)
		sp := cfg.SystemProxy
		if sp.HTTP != "" || sp.HTTPS != "explicit:8080" || sp.Bypass != nil {
			t.Fatalf("explicit system_proxy was modified: %+v", sp)
		}
	})
}

func TestApplyProxyForwardDefaultsNormalizesListen(t *testing.T) {
	cfg := validProxyForwardCfg(t)
	cfg.ProxyForward.Listen = " 127.0.0.1:8888 "
	ApplyProxyForwardDefaults(cfg)
	if got := cfg.Env["http_proxy"]; got != "http://127.0.0.1:8888" {
		t.Fatalf("http_proxy=%q", got)
	}
}

func TestApplyProxyForwardDefaultsNoopCases(t *testing.T) {
	cfg := &Config{Env: map[string]string{"http_proxy": "http://explicit:8080"}}
	ApplyProxyForwardDefaults(cfg)
	if len(cfg.Env) != 1 {
		t.Fatalf("env changed without proxy_forward: %v", cfg.Env)
	}

	bad := validProxyForwardCfg(t)
	bad.ProxyForward.Listen = "192.168.64.1:8888"
	ApplyProxyForwardDefaults(bad)
	if len(bad.Env) != 0 {
		t.Fatalf("invalid listen must not be propagated: %v", bad.Env)
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
		{"empty upstream", func(c *Config) { c.ProxyForward.Upstream = "" }, "required"},
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
