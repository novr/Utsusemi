package config

import "testing"

func TestApplyDefaultsHostedAppBrokerURL(t *testing.T) {
	cfg := &Config{Registration: Registration{Mode: ModeHostedApp}}
	ApplyDefaults(cfg)
	if cfg.Registration.BrokerURL != DefaultHostedAppBrokerURL {
		t.Fatalf("BrokerURL = %q, want %q", cfg.Registration.BrokerURL, DefaultHostedAppBrokerURL)
	}
}

func TestApplyDefaultsReclaimPolicy(t *testing.T) {
	cfg := &Config{}
	ApplyDefaults(cfg)
	if cfg.ReclaimPolicy != ReclaimGrace {
		t.Fatalf("ReclaimPolicy = %q, want %q", cfg.ReclaimPolicy, ReclaimGrace)
	}
}
