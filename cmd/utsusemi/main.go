package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	configPath string
	rootCmd    = &cobra.Command{
		Use:   "utsusemi",
		Short: "Ephemeral self-hosted GitHub Actions runners on Apple Silicon Macs",
	}
)

func main() {
	rootCmd.PersistentFlags().StringVar(&configPath, "config", defaultConfigPath(), "path to config file")

	rootCmd.AddCommand(newRunCmd())
	rootCmd.AddCommand(newConfigureCmd())
	rootCmd.AddCommand(newValidateCmd())
	rootCmd.AddCommand(newRegisterCmd())
	rootCmd.AddCommand(newReclaimCmd())

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func defaultConfigPath() string {
	if path := os.Getenv("UTSUSEMI_CONFIG"); path != "" {
		return path
	}
	return "/etc/utsusemi/config.yaml"
}
