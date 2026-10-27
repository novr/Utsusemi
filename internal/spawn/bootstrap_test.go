package spawn

import (
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/runnercache"
)

func TestBootstrapSpecUsesProviderRunnerArch(t *testing.T) {
	p := provider.NewStub(2)
	p.Caps.RunnerArch = "linux-arm64"

	spec := BootstrapSpecFor(&config.Config{RunnerVersion: "2.336.0"}, p)
	if spec.RunnerArch != "linux-arm64" {
		t.Fatalf("runner arch = %q", spec.RunnerArch)
	}
	env := spec.Env()
	if env["RUNNER_ARCH"] != "linux-arm64" {
		t.Fatalf("runner arch env = %q", env["RUNNER_ARCH"])
	}
}

func TestBootstrapSpecDefaultsRunnerArch(t *testing.T) {
	spec := BootstrapSpecFor(&config.Config{RunnerVersion: "2.336.0"}, provider.NewTartProvider(provider.NewFakeExecutor(), false, nil))
	if spec.RunnerArch != "osx-arm64" {
		t.Fatalf("runner arch = %q", spec.RunnerArch)
	}
	if spec.RunnerHome != defaultRunnerHome {
		t.Fatalf("runner home = %q", spec.RunnerHome)
	}
	if spec.RunnerCacheDir != runnercache.GuestCacheDir {
		t.Fatalf("runner cache dir = %q", spec.RunnerCacheDir)
	}
	env := spec.Env()
	if env["RUNNER_CACHE_DIR"] != runnercache.GuestCacheDir {
		t.Fatalf("RUNNER_CACHE_DIR = %q", env["RUNNER_CACHE_DIR"])
	}
}

func TestGuestEnvMergesConfigAndBootstrap(t *testing.T) {
	p := provider.NewStub(2)
	p.Caps.RunnerArch = "linux-arm64"
	cfg := &config.Config{
		RunnerVersion: "2.336.0",
		Env: map[string]string{
			"HTTP_PROXY":     "http://proxy:8080",
			"RUNNER_VERSION": "should-not-win",
		},
	}
	env := GuestEnv(cfg, p)
	if env["HTTP_PROXY"] != "http://proxy:8080" {
		t.Fatalf("HTTP_PROXY = %q", env["HTTP_PROXY"])
	}
	if env["RUNNER_VERSION"] != "2.336.0" {
		t.Fatalf("RUNNER_VERSION = %q, want bootstrap win", env["RUNNER_VERSION"])
	}
	if env["RUNNER_ARCH"] != "linux-arm64" {
		t.Fatalf("RUNNER_ARCH = %q", env["RUNNER_ARCH"])
	}
}

func TestGuestEnvSystemProxyDoesNotSetProcessProxy(t *testing.T) {
	p := provider.NewStub(2)
	cfg := &config.Config{
		RunnerVersion: "2.336.0",
		Env: map[string]string{
			"http_proxy": "http://proxy.example:8080",
			"no_proxy":   "10.0.0.0/8",
		},
		SystemProxy: &config.SystemProxy{
			HTTP:   "proxy.example:8080",
			HTTPS:  "proxy.example:8080",
			Bypass: []string{"localhost", "*.local"},
		},
	}
	env := GuestEnv(cfg, p)
	if env["UTSUSEMI_SYSTEM_PROXY_HTTP_HOST"] != "proxy.example" || env["UTSUSEMI_SYSTEM_PROXY_HTTP_PORT"] != "8080" {
		t.Fatalf("http = %q %q", env["UTSUSEMI_SYSTEM_PROXY_HTTP_HOST"], env["UTSUSEMI_SYSTEM_PROXY_HTTP_PORT"])
	}
	if env["UTSUSEMI_SYSTEM_PROXY_HTTPS_HOST"] != "proxy.example" || env["UTSUSEMI_SYSTEM_PROXY_HTTPS_PORT"] != "8080" {
		t.Fatalf("https = %q %q", env["UTSUSEMI_SYSTEM_PROXY_HTTPS_HOST"], env["UTSUSEMI_SYSTEM_PROXY_HTTPS_PORT"])
	}
	if env["UTSUSEMI_SYSTEM_PROXY_BYPASS"] != "localhost,*.local" {
		t.Fatalf("bypass = %q", env["UTSUSEMI_SYSTEM_PROXY_BYPASS"])
	}
	if strings.Contains(env["UTSUSEMI_SYSTEM_PROXY_BYPASS"], "proxy.example") {
		t.Fatalf("bypass gained proxy host: %q", env["UTSUSEMI_SYSTEM_PROXY_BYPASS"])
	}
	if env["http_proxy"] != "http://proxy.example:8080" || env["no_proxy"] != "10.0.0.0/8" {
		t.Fatalf("process proxy env changed: %v", env)
	}
	if _, ok := env["https_proxy"]; ok {
		t.Fatal("system_proxy must not create https_proxy")
	}

	omit := &config.Config{RunnerVersion: "2.336.0", SystemProxy: &config.SystemProxy{HTTP: "10.0.0.1:9"}}
	omitted := GuestEnv(omit, p)
	if _, ok := omitted["UTSUSEMI_SYSTEM_PROXY_BYPASS"]; ok {
		t.Fatal("omitted bypass must not set BYPASS")
	}
	if _, ok := omitted["http_proxy"]; ok {
		t.Fatal("system_proxy alone must not set http_proxy")
	}

	plain := GuestEnv(&config.Config{RunnerVersion: "2.336.0"}, p)
	for k := range plain {
		if strings.HasPrefix(k, "UTSUSEMI_SYSTEM_PROXY_") {
			t.Fatalf("unset system_proxy leaked %s", k)
		}
	}
}

func TestGuestEnvNilConfig(t *testing.T) {
	env := GuestEnv(nil, provider.NewStub(2))
	if env["RUNNER_HOME"] != defaultRunnerHome {
		t.Fatalf("RUNNER_HOME = %q", env["RUNNER_HOME"])
	}
}
