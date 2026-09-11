package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/credentialview"
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
  utsusemi configure token --token "$TOKEN" --org my-org
  utsusemi configure token --pool-size 2`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if runnerGroup <= 0 {
				runnerGroup = 1
			}
			path := configureOutputPath(cmd, outputPath)
			if err := confirmConfigOverwrite(path, force, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
				return err
			}

			token, err := resolveTokenOptional(cmd.InOrStdin(), tokenFlag)
			if err != nil {
				return err
			}

			merge, err := mergeConfigureConfig(cmd, configureMergeInput{
				Mode:       configureModeToken,
				OutputPath: path,
				Target: configureTargetInput{
					Org:         org,
					Repo:        repo,
					RunnerGroup: runnerGroup,
				},
				Opts: opts,
			})
			if err != nil {
				return err
			}

			success := configureSuccess{WroteConfig: true}
			if token != "" {
				if err := saveCredential(merge.Config, token); err != nil {
					return err
				}
				success.CredentialUpdated = true
			} else {
				info, err := credentialview.Load(merge.Config, credentialStoreOrDefault())
				if err != nil {
					return err
				}
				if !info.Present {
					success.CredentialMissing = true
				}
			}

			if err := writeConfig(path, merge.Config); err != nil {
				return err
			}
			printConfigureSuccess(path, success)
			return nil
		},
	}

	cmd.Flags().StringVar(&tokenFlag, "token", "", "GitHub token (prefer stdin)")
	cmd.Flags().StringVar(&outputPath, "output", configPath, "config output path")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository (owner/repo)")
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "runner group id")
	cmd.Flags().BoolVar(&force, "force", false, "update existing config without prompting")
	addRunnerFlags(cmd, &opts)
	return cmd
}

func resolveTokenOptional(stdin io.Reader, flagValue string) (string, error) {
	if token := strings.TrimSpace(flagValue); token != "" {
		return token, nil
	}
	readStdin := false
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
	return "", nil
}
