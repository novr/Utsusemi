package config

import "testing"

func TestMountsRespectsValue(t *testing.T) {
	cfg := &Config{Mounts: []string{"~/cache", "~/toolchains:ro"}}
	if len(cfg.Mounts) != 2 || cfg.Mounts[0] != "~/cache" {
		t.Fatalf("mounts = %v", cfg.Mounts)
	}
}
