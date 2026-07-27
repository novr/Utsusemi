package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
)

func newConfigureCmd() *cobra.Command {
	var (
		pat         string
		outputPath  string
		org         string
		repo        string
		runnerGroup int64
		opts        runnerOptions
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
				Registration: config.Registration{
					Mode: config.ModeGitHubPAT,
				},
			}
			opts.apply(cfg)
			config.ApplyDefaults(cfg)
			if _, err := config.Validate(cfg, 2); err != nil {
				return err
			}
			if err := writeConfig(outputPath, cfg); err != nil {
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
	addRunnerFlags(cmd, &opts)
	return cmd
}
