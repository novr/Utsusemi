package config

import (
	"context"
	"fmt"
	"net/netip"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/target"
)

type mapResolver map[string][]netip.Addr

func (m mapResolver) LookupHost(_ context.Context, host string) ([]netip.Addr, error) {
	addrs, ok := m[host]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return addrs, nil
}

func baseCfg() *Config {
	return &Config{
		Target:        target.ConfigYAML{Org: "my-org", RunnerGroupID: 1},
		Labels:        []string{"self-hosted"},
		Registration:  Registration{Mode: ModeGitHubPAT},
		Provider:      "tart",
		BaseImage:     "img",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
	}
}

func TestProxyHostsCollectsAndDedups(t *testing.T) {
	cfg := baseCfg()
	cfg.Env = map[string]string{
		"http_proxy":  "proxy.example:8080",
		"HTTP_PROXY":  "http://proxy.example:8080",
		"https_proxy": "https://user:pass@proxy.example:8443",
		"no_proxy":    "localhost",
	}
	cfg.SystemProxy = &SystemProxy{
		HTTP:  "proxy.example:8080",
		HTTPS: "10.0.0.5:8080",
	}
	hosts, err := ProxyHosts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != "proxy.example" {
		t.Fatalf("hosts=%v", hosts)
	}
	if cfg.Env["https_proxy"] != "https://user:pass@proxy.example:8443" {
		t.Fatal("cfg mutated")
	}
}

func TestProxyHostsSchemeLessAndUserinfo(t *testing.T) {
	cfg := baseCfg()
	cfg.Env = map[string]string{
		"http_proxy":  "user:pass@proxy.example:8080",
		"https_proxy": "localhost:3128",
	}
	hosts, err := ProxyHosts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 2 {
		t.Fatalf("hosts=%v", hosts)
	}
	joined := strings.Join(hosts, ",")
	if !strings.Contains(joined, "proxy.example") || !strings.Contains(joined, "localhost") {
		t.Fatalf("hosts=%v", hosts)
	}
}

func TestProxyHostsTrimsTrailingDot(t *testing.T) {
	cfg := baseCfg()
	cfg.Env = map[string]string{
		"http_proxy": "http://proxy.example.:8080",
	}
	hosts, err := ProxyHosts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 1 || hosts[0] != "proxy.example" {
		t.Fatalf("hosts=%v", hosts)
	}
}

func TestProxyHostsSkipsIPLiterals(t *testing.T) {
	cfg := baseCfg()
	cfg.Env = map[string]string{
		"http_proxy":  "http://10.0.0.5:8080",
		"https_proxy": "http://[2001:db8::1]:8080",
	}
	hosts, err := ProxyHosts(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(hosts) != 0 {
		t.Fatalf("hosts=%v", hosts)
	}
}

func TestProxyHostsRejectsBadHostname(t *testing.T) {
	cfg := baseCfg()
	cfg.Env = map[string]string{"http_proxy": "http://bad host:8080"}
	if _, err := ProxyHosts(cfg); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateResolveProxyOnHost(t *testing.T) {
	cfg := baseCfg()
	cfg.ResolveProxyOnHost = true
	cfg.Env = map[string]string{"http_proxy": "http://proxy.example:8080"}
	if _, err := Validate(cfg, provider.NewStub(2)); err != nil {
		t.Fatal(err)
	}
	cfg.Env["http_proxy"] = "ftp://proxy.example:8080"
	if _, err := Validate(cfg, provider.NewStub(2)); err == nil {
		t.Fatal("expected scheme error")
	}
}

func TestResolveProxyHosts(t *testing.T) {
	cfg := baseCfg()
	cfg.ResolveProxyOnHost = true
	cfg.Env = map[string]string{"https_proxy": "https://proxy.example:8443"}
	r := mapResolver{
		"proxy.example": {
			netip.MustParseAddr("2001:db8::1"),
			netip.MustParseAddr("10.0.0.5"),
			netip.MustParseAddr("10.0.0.6"),
		},
	}
	entries, err := ResolveProxyHosts(context.Background(), cfg, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Host != "proxy.example" || entries[0].IP.String() != "10.0.0.5" {
		t.Fatalf("entries=%v", entries)
	}

	cfg.ResolveProxyOnHost = false
	entries, err = ResolveProxyHosts(context.Background(), cfg, r)
	if err != nil || len(entries) != 0 {
		t.Fatalf("opt-out: %v %v", entries, err)
	}

	cfg.ResolveProxyOnHost = true
	if _, err := ResolveProxyHosts(context.Background(), cfg, mapResolver{}); err == nil {
		t.Fatal("expected resolve failure")
	}
}
