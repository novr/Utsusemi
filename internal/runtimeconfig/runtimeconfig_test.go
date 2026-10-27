package runtimeconfig

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
)

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	cfg := &config.Config{
		Target:        config.TargetYAML("my-org", "", 5),
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.337.0",
		PoolSize:      2,
		Labels:        []string{"self-hosted", "macOS"},
		MinFreeDiskGB: 50,
		VMNamePrefix:  "utsusemi-",
		StateDir:      dir,
		SpawnTimeout:  config.Duration(10 * time.Minute),
		JobTimeout:    config.Duration(6 * time.Hour),
		SystemProxy: &config.SystemProxy{
			HTTP:   "proxy.example:8080",
			HTTPS:  "proxy.example:8080",
			Bypass: []string{"localhost"},
		},
	}
	config.ApplyDefaults(cfg)

	snap := FromConfig(cfg, "agent-1")
	if err := Save(dir, snap); err != nil {
		t.Fatal(err)
	}
	got, ok := Load(dir)
	if !ok {
		t.Fatal("Load missing")
	}
	if got.AgentID != "agent-1" || got.BaseImage != cfg.BaseImage || got.PoolSize != 2 {
		t.Fatalf("got=%+v", got)
	}
	if got.Target != "org:my-org (group 5)" {
		t.Fatalf("target=%q", got.Target)
	}
	if got.SystemProxyHTTP != "proxy.example:8080" || got.SystemProxyHTTPS != "proxy.example:8080" || len(got.SystemProxyBypass) != 1 || got.SystemProxyBypass[0] != "localhost" {
		t.Fatalf("system_proxy=%q %q %v", got.SystemProxyHTTP, got.SystemProxyHTTPS, got.SystemProxyBypass)
	}
	if _, err := filepath.Rel(dir, filepath.Join(dir, fileName)); err != nil {
		t.Fatal(err)
	}
}

func TestLoadMissing(t *testing.T) {
	if _, ok := Load(t.TempDir()); ok {
		t.Fatal("expected missing")
	}
}
