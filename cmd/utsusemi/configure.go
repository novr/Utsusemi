package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/novr/utsusemi/internal/config"
)

func newConfigureCmd() *cobra.Command {
	var (
		pat         string
		outputPath  string
		org         string
		repo        string
		runnerGroup int64
		labels      string
		baseImage   string
		runnerVer   string
		poolSize    int
	)

	cmd := &cobra.Command{
		Use:   "configure",
		Short: "Write config and store credentials",
		RunE: func(cmd *cobra.Command, args []string) error {
			if strings.TrimSpace(pat) == "" {
				return fmt.Errorf("--pat is required")
			}
			if org == "" && repo == "" {
				return fmt.Errorf("either --org or --repo is required")
			}
			if org != "" && repo != "" {
				return fmt.Errorf("--org and --repo are mutually exclusive")
			}
			if org != "" && runnerGroup <= 0 {
				runnerGroup = 1
			}

			cfg := &config.Config{
				Target: config.TargetYAML(org, repo, runnerGroup),
				Labels: splitLabels(labels),
				Registration: config.Registration{
					Mode: config.ModeGitHubPAT,
				},
				Provider:      "tart",
				BaseImage:     baseImage,
				RunnerVersion: runnerVer,
				PoolSize:      poolSize,
			}
			config.ApplyDefaults(cfg)
			if _, err := config.Validate(cfg, 2); err != nil {
				return err
			}

			if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
				return err
			}
			data, err := yaml.Marshal(cfg)
			if err != nil {
				return err
			}
			if err := os.WriteFile(outputPath, data, 0o600); err != nil {
				return err
			}
			if err := saveCredential(cfg, pat); err != nil {
				return err
			}
			fmt.Printf("wrote config to %s\n", outputPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&pat, "pat", "", "GitHub fine-grained PAT")
	cmd.Flags().StringVar(&outputPath, "output", configPath, "config output path")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository (owner/repo)")
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "runner group id for org target")
	cmd.Flags().StringVar(&labels, "labels", "self-hosted,macOS,tart,arm64", "comma-separated runner labels")
	cmd.Flags().StringVar(&baseImage, "base-image", "ghcr.io/cirruslabs/macos-sequoia-base:latest", "tart base image")
	cmd.Flags().StringVar(&runnerVer, "runner-version", "2.336.0", "actions runner version")
	cmd.Flags().IntVar(&poolSize, "pool-size", 1, "pool size")
	return cmd
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
