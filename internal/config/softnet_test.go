package config

import "testing"

func TestSoftnetDefaultsOff(t *testing.T) {
	cfg := &Config{}
	if cfg.Softnet {
		t.Fatal("expected softnet off by default")
	}
}

func TestSoftnetRespectsValue(t *testing.T) {
	cfg := &Config{Softnet: true}
	if !cfg.Softnet {
		t.Fatal("expected softnet enabled")
	}
}
