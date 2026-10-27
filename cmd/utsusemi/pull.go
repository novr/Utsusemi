package main

import (
	"context"

	"github.com/novr/utsusemi/internal/pull"
	"github.com/novr/utsusemi/internal/runnerrelease"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull",
		Short: "Fetch a base image or a runner tarball",
	}

	image := &cobra.Command{
		Use:   "image <image>",
		Short: "Sync a base image through the configured provider",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadConfigRuntime(cmd.Context())
			if err != nil {
				return err
			}
			return pull.Image(cmd.Context(), pull.ImageOptions{
				Ref:             args[0],
				StateDir:        rt.Config.StateDir,
				ConfigBaseImage: rt.Config.BaseImage,
				Sync:            rt.Provider.SyncImage,
				Stderr:          cmd.ErrOrStderr(),
			})
		},
	}

	runner := &cobra.Command{
		Use:   "runner <latest|version>",
		Short: "Cache a GitHub Actions runner tarball",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			rt, err := loadConfigRuntime(cmd.Context())
			if err != nil {
				return err
			}
			return pull.Runner(cmd.Context(), pull.RunnerOptions{
				VersionArg:          args[0],
				StateDir:            rt.Config.StateDir,
				Arch:                rt.Provider.Capabilities().RunnerArch,
				ConfigRunnerVersion: rt.Config.RunnerVersion,
				Latest: func(ctx context.Context) (string, error) {
					return runnerrelease.Latest(ctx, nil)
				},
				Logger: rt.Logger,
				Stderr: cmd.ErrOrStderr(),
			})
		},
	}
	registerPullRunnerCompletions(runner)

	cmd.AddCommand(image, runner)
	return cmd
}
