package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/novr/utsusemi/internal/version"
	"github.com/spf13/cobra"
)

var (
	configPath string
	rootCmd    = &cobra.Command{
		Use:           "utsusemi",
		Short:         "Ephemeral self-hosted GitHub Actions runners",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
)

func main() {
	rootCmd.Version = version.String()
	rootCmd.SetVersionTemplate(version.Line() + "\n")

	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath(), "path to config file")

	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newListCmd())
	rootCmd.AddCommand(newCleanCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	if path := os.Getenv("UTSUSEMI_CONFIG"); path != "" {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/etc/utsusemi/config.yaml"
	}
	return filepath.Join(home, ".config", "utsusemi", "config.yaml")
}
