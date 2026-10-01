package main

import (
	"testing"
	"time"

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

func TestNormalizeMounts(t *testing.T) {
	got := normalizeMounts([]string{" /a ", "", " /b "})
	if len(got) != 2 || got[0] != "/a" || got[1] != "/b" {
		t.Fatalf("got %v", got)
	}
	if got := normalizeMounts([]string{"", "  "}); len(got) != 0 {
		t.Fatalf("clear=%v", got)
	}
}

func TestSharedConfigOptionsApply(t *testing.T) {
	opts := sharedConfigOptions{
		labels:        "self-hosted,macOS",
		baseImage:     "example:latest",
		runnerVer:     "2.336.0",
		poolSize:      2,
		mounts:        []string{"~/cache"},
		softnet:       true,
		reclaimPolicy: config.ReclaimHard,
		reclaimGrace:  30 * time.Minute,
		minFreeDiskGB: 80,
	}
	cfg := &config.Config{}
	opts.apply(cfg)
	if cfg.Provider != "tart" || cfg.BaseImage != "example:latest" || cfg.PoolSize != 2 {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if len(cfg.Labels) != 2 || cfg.Labels[0] != "self-hosted" {
		t.Fatalf("unexpected labels: %v", cfg.Labels)
	}
	if len(cfg.Mounts) != 1 || cfg.Mounts[0] != "~/cache" || !cfg.Softnet {
		t.Fatalf("mounts/softnet: mounts=%v softnet=%v", cfg.Mounts, cfg.Softnet)
	}
	if cfg.ReclaimPolicy != config.ReclaimHard || cfg.ReclaimGrace.Duration() != 30*time.Minute || cfg.MinFreeDiskGB != 80 {
		t.Fatalf("reclaim/disk: %+v", cfg)
	}
}
