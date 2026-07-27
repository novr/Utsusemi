package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
)

func newConfigureTokenCmd() *cobra.Command {
	var (
		tokenFlag   string
		outputPath  string
		org         string
		repo        string
		runnerGroup int64
		force       bool
		opts        runnerOptions
	)

	cmd := &cobra.Command{
		Use:   "token",
		Short: "Configure with a fine-grained personal access token",
		Args:  cobra.NoArgs,
		Example: `  printf '%s' "$TOKEN" | utsusemi configure token --repo owner/repo
  utsusemi configure token --token "$TOKEN" --org my-org`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if org == "" && repo == "" {
				return fmt.Errorf("either --org or --repo is required")
			}
			if org != "" && repo != "" {
				return fmt.Errorf("--org and --repo are mutually exclusive")
			}
			if org != "" && runnerGroup <= 0 {
				runnerGroup = 1
			}
			if err := confirmConfigOverwrite(outputPath, force, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
				return err
			}

			token, err := resolveToken(cmd.InOrStdin(), tokenFlag)
			if err != nil {
				return err
			}

			cfg := &config.Config{
				Target: config.TargetYAML(org, repo, runnerGroup),
				Registration: config.Registration{
					Mode: config.ModeGitHubPAT,
				},
			}
			opts.apply(cfg)
			config.ApplyDefaults(cfg)
			if _, err := config.Validate(cfg, providerMaxConcurrent()); err != nil {
				return err
			}
			if err := saveCredential(cfg, token); err != nil {
				return err
			}
			if err := writeConfig(outputPath, cfg); err != nil {
				return err
			}
			printConfigureSuccess(outputPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&tokenFlag, "token", "", "GitHub token (prefer stdin)")
	cmd.Flags().StringVar(&outputPath, "output", configPath, "config output path")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository (owner/repo)")
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "runner group id for org target")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config without prompting")
	addRunnerFlags(cmd, &opts)
	return cmd
}

func resolveToken(stdin io.Reader, flagValue string) (string, error) {
	readStdin := true
	if file, ok := stdin.(*os.File); ok {
		info, err := file.Stat()
		if err != nil {
			return "", fmt.Errorf("inspect stdin: %w", err)
		}
		readStdin = info.Mode()&os.ModeCharDevice == 0
	}
	if readStdin {
		data, err := io.ReadAll(io.LimitReader(stdin, 64*1024+1))
		if err != nil {
			return "", fmt.Errorf("read token from stdin: %w", err)
		}
		if len(data) > 64*1024 {
			return "", fmt.Errorf("token from stdin is too large")
		}
		if token := strings.TrimSpace(string(data)); token != "" {
			return token, nil
		}
	}
	if token := strings.TrimSpace(flagValue); token != "" {
		return token, nil
	}
	return "", fmt.Errorf("token is required via stdin or --token")
}
