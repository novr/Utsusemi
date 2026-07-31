package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/novr/utsusemi/internal/version"
	"github.com/spf13/cobra"
)

var (
	configPath string
	initRoot   sync.Once
	rootCmd    = &cobra.Command{
		Use:           "utsusemi",
		Short:         "Ephemeral self-hosted GitHub Actions runners",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
)

func initRootCmd() {
	initRoot.Do(func() {
		rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath(), "path to config file")

		rootCmd.AddCommand(newVersionCmd())
		rootCmd.AddCommand(newRunCmd())
		rootCmd.AddCommand(newConfigureCmd())
		rootCmd.AddCommand(newValidateCmd())
		rootCmd.AddCommand(newStatusCmd())
		rootCmd.AddCommand(newDoctorCmd())

		listCmd := newListCmd()
		registerListCompletions(listCmd)
		rootCmd.AddCommand(listCmd)

		rootCmd.AddCommand(newCleanCmd())
	})
}

func main() {
	rootCmd.Version = version.String()
	rootCmd.SetVersionTemplate(version.Line() + "\n")

	initRootCmd()

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
