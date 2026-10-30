package main

import (
	"context"
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"gopkg.in/yaml.v3"

	"github.com/novr/utsusemi/internal/app"
	"github.com/novr/utsusemi/internal/config"
)

//go:embed config.template.yaml
var configTemplate []byte

func newConfigurePathCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "path",
		Short: "Print the resolved config file path",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Fprintln(cmd.OutOrStdout(), configPath)
			return nil
		},
	}
}

func newConfigureShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Print the current config as YAML",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConfigShow(cmd, configPath)
		},
	}
}

func newConfigureEditCmd() *cobra.Command {
	var (
		force bool
		opts  sharedConfigOptions
	)
	cmd := &cobra.Command{
		Use:   "edit",
		Short: "Edit config.yaml in $EDITOR, or apply shared flags without opening an editor",
		Example: `  utsusemi configure edit
  utsusemi configure edit --runner-version latest
  utsusemi configure edit --pool-size 2
  utsusemi configure edit --mounts ~/utsusemi-cache/swiftpm`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if sharedConfigFlagsChanged(cmd) {
				return runConfigEditPatch(cmd, configPath, opts, force, nil)
			}
			return runConfigEdit(cmd, configPath)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "with shared flags: update without prompting (ignored in $EDITOR mode)")
	addSharedConfigFlags(cmd, &opts)
	return cmd
}

func runConfigEditPatch(
	cmd *cobra.Command,
	path string,
	opts sharedConfigOptions,
	force bool,
	latest func(context.Context) (string, error),
) error {
	existing, cfg, err := loadExistingConfig(path)
	if err != nil {
		return err
	}
	if !existing {
		return fmt.Errorf("config not found at %s; run `utsusemi configure app` or `utsusemi configure token` first", path)
	}
	if err := confirmConfigOverwrite(path, force, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
		return err
	}

	if err := resolveRunnerVersionFlag(cmd.Context(), cmd, &opts, true, latest); err != nil {
		return err
	}
	if err := applySharedConfigOptions(cmd, opts, cfg, false); err != nil {
		return err
	}
	check := config.Clone(cfg)
	config.ApplyDefaults(check)
	if _, err := app.ValidateConfig(check); err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	patched, err := applyConfigYAMLUpdates(raw, sharedFlagYAMLUpdates(cmd, opts))
	if err != nil {
		return err
	}
	if err := writeConfigBytes(path, patched); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote config to %s\n", path)
	fmt.Fprintln(cmd.OutOrStdout(), "next: run `utsusemi validate`, then restart the agent if it is running")
	return nil
}

func runConfigEdit(cmd *cobra.Command, path string) error {
	editor, err := resolveEditor()
	if err != nil {
		return err
	}

	initial, err := configEditInitialBytes(path)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".utsusemi-config-edit-*.yaml")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	keepTemp := false
	defer func() {
		if !keepTemp {
			_ = os.Remove(tmpPath)
		}
	}()
	if _, err := tmp.Write(initial); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}

	if err := runEditor(editor, tmpPath, cmd); err != nil {
		return fmt.Errorf("editor exited with error: %w", err)
	}

	edited, err := os.ReadFile(tmpPath)
	if err != nil {
		return err
	}
	cfg := &config.Config{}
	if err := yaml.Unmarshal(edited, cfg); err != nil {
		keepTemp = true
		return fmt.Errorf("parse edited config: %w (temp file: %s)", err, tmpPath)
	}
	check := config.Clone(cfg)
	config.ApplyDefaults(check)
	if _, err := app.ValidateConfig(check); err != nil {
		keepTemp = true
		return fmt.Errorf("validate edited config: %w (temp file: %s)", err, tmpPath)
	}
	if err := writeConfigBytes(path, edited); err != nil {
		return err
	}
	fmt.Fprintf(cmd.OutOrStdout(), "wrote config to %s\n", path)
	fmt.Fprintln(cmd.OutOrStdout(), "next: run `utsusemi validate`")
	return nil
}

func runConfigShow(cmd *cobra.Command, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}
	if err := yaml.Unmarshal(data, &config.Config{}); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	_, err = cmd.OutOrStdout().Write(data)
	return err
}

func runEditor(editor, path string, cmd *cobra.Command) error {
	editCmd := exec.Command("sh", "-c", editor+" "+shellQuote(path))
	editCmd.Stdin = cmd.InOrStdin()
	editCmd.Stdout = cmd.OutOrStdout()
	editCmd.Stderr = cmd.ErrOrStderr()
	return editCmd.Run()
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

func resolveEditor() (string, error) {
	for _, key := range []string{"VISUAL", "EDITOR"} {
		if v := os.Getenv(key); v != "" {
			return v, nil
		}
	}
	return "", fmt.Errorf("set EDITOR or VISUAL")
}

func configEditInitialBytes(path string) ([]byte, error) {
	_, err := os.Stat(path)
	if err == nil {
		return os.ReadFile(path)
	}
	if !os.IsNotExist(err) {
		return nil, err
	}
	return configTemplate, nil
}
