package config

import "testing"

func TestApplyDefaultsReclaimPolicy(t *testing.T) {
	cfg := &Config{}
	ApplyDefaults(cfg)
	if cfg.ReclaimPolicy != ReclaimGrace {
		t.Fatalf("ReclaimPolicy = %q, want %q", cfg.ReclaimPolicy, ReclaimGrace)
	}
}
