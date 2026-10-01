package main

import (
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
)

const (
	defaultLabels        = "self-hosted,macOS,tart,arm64"
	defaultBaseImage     = "ghcr.io/cirruslabs/macos-sequoia-base:latest"
	defaultRunnerVersion = "2.336.0"
	defaultPoolSize      = 1
)

type sharedConfigOptions struct {
	labels        string
	baseImage     string
	runnerVer     string
	poolSize      int
	mounts        []string
	softnet       bool
	reclaimPolicy string
	reclaimGrace  time.Duration
	minFreeDiskGB int
}

func (o sharedConfigOptions) apply(cfg *config.Config) {
	cfg.Labels = splitLabels(o.labels)
	cfg.Provider = "tart"
	cfg.BaseImage = o.baseImage
	cfg.RunnerVersion = o.runnerVer
	cfg.PoolSize = o.poolSize
	cfg.Mounts = normalizeMounts(o.mounts)
	cfg.Softnet = o.softnet
	cfg.ReclaimPolicy = o.reclaimPolicy
	cfg.ReclaimGrace = config.Duration(o.reclaimGrace)
	cfg.MinFreeDiskGB = o.minFreeDiskGB
}

func addSharedConfigFlags(cmd *cobra.Command, opts *sharedConfigOptions) {
	cmd.Flags().StringVar(&opts.labels, "labels", defaultLabels, "comma-separated runner labels")
	cmd.Flags().StringVar(&opts.baseImage, "base-image", defaultBaseImage, "base image used to create runner VMs")
	cmd.Flags().StringVar(&opts.runnerVer, "runner-version", defaultRunnerVersion, "actions runner version (use latest to resolve from GitHub)")
	cmd.Flags().IntVar(&opts.poolSize, "pool-size", defaultPoolSize, "pool size")
	cmd.Flags().StringArrayVar(&opts.mounts, "mounts", nil, "host directory share for Tart --dir (repeatable; --mounts= clears)")
	cmd.Flags().BoolVar(&opts.softnet, "softnet", false, "use Softnet networking (disable with --softnet=false)")
	cmd.Flags().StringVar(&opts.reclaimPolicy, "reclaim-policy", config.DefaultReclaimPolicy, "reclaim policy: soft, grace, or hard")
	cmd.Flags().DurationVar(&opts.reclaimGrace, "reclaim-grace", config.DefaultReclaimGrace, "grace window when reclaim-policy is grace")
	cmd.Flags().IntVar(&opts.minFreeDiskGB, "min-free-disk-gb", config.DefaultMinFreeDiskGB, "pause spawning when free disk is below this many GB")
}

func splitLabels(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeMounts(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	for _, m := range in {
		m = strings.TrimSpace(m)
		if m != "" {
			out = append(out, m)
		}
	}
	return out
}
