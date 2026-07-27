package main

import (
	"testing"

	"github.com/novr/utsusemi/internal/config"
)

func TestSplitLabels(t *testing.T) {
	got := splitLabels(" self-hosted, macOS ,,tart ")
	want := []string{"self-hosted", "macOS", "tart"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestRunnerOptionsApply(t *testing.T) {
	opts := runnerOptions{
		labels:    "self-hosted,macOS",
		baseImage: "example:latest",
		runnerVer: "2.336.0",
		poolSize:  2,
	}
	cfg := &config.Config{}
	opts.apply(cfg)
	if cfg.Provider != "tart" || cfg.BaseImage != "example:latest" || cfg.PoolSize != 2 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.Labels) != 2 || cfg.Labels[0] != "self-hosted" {
		t.Fatalf("unexpected labels: %v", cfg.Labels)
	}
}
