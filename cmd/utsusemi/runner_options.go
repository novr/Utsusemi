package main

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
)

const (
	defaultLabels        = "self-hosted,macOS,tart,arm64"
	defaultBaseImage     = "ghcr.io/cirruslabs/macos-sequoia-base:latest"
	defaultRunnerVersion = "2.336.0"
	defaultPoolSize      = 1
)

type runnerOptions struct {
	labels    string
	baseImage string
	runnerVer string
	poolSize  int
}

func (o runnerOptions) apply(cfg *config.Config) {
	cfg.Labels = splitLabels(o.labels)
	cfg.Provider = "tart"
	cfg.BaseImage = o.baseImage
	cfg.RunnerVersion = o.runnerVer
	cfg.PoolSize = o.poolSize
}

func addRunnerFlags(cmd *cobra.Command, opts *runnerOptions) {
	cmd.Flags().StringVar(&opts.labels, "labels", defaultLabels, "comma-separated runner labels")
	cmd.Flags().StringVar(&opts.baseImage, "base-image", defaultBaseImage, "tart base image")
	cmd.Flags().StringVar(&opts.runnerVer, "runner-version", defaultRunnerVersion, "actions runner version")
	cmd.Flags().IntVar(&opts.poolSize, "pool-size", defaultPoolSize, "pool size")
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
