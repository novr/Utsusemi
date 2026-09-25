package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/notify"
)

func newConfigureAlertsCmd() *cobra.Command {
	var (
		webhookURL string
		clear      bool
	)
	cmd := &cobra.Command{
		Use:   "alerts",
		Short: "Configure alert webhook URL (Keychain)",
		Args:  cobra.NoArgs,
		Example: `  utsusemi configure alerts --webhook-url https://hooks.example/xxx
  utsusemi configure alerts --clear`,
		RunE: func(cmd *cobra.Command, args []string) error {
			store := credentialStoreOrDefault()
			if clear {
				if err := store.Delete(notify.AlertService, notify.AlertAccount); err != nil {
					if !errors.Is(err, keychain.ErrNotFound) {
						return fmt.Errorf("clear alert webhook: %w", err)
					}
				}
				fmt.Fprintln(cmd.OutOrStdout(), "alert webhook cleared")
				return nil
			}
			webhookURL = strings.TrimSpace(webhookURL)
			if webhookURL == "" {
				return fmt.Errorf("provide --webhook-url or --clear")
			}
			if err := notify.ValidateWebhookURL(webhookURL); err != nil {
				return err
			}
			if err := store.Set(notify.AlertService, notify.AlertAccount, webhookURL); err != nil {
				return fmt.Errorf("save alert webhook: %w", err)
			}
			fmt.Fprintln(cmd.OutOrStdout(), "alert webhook saved to Keychain")
			return nil
		},
	}
	cmd.Flags().StringVar(&webhookURL, "webhook-url", "", "webhook URL for agent alerts")
	cmd.Flags().BoolVar(&clear, "clear", false, "remove stored alert webhook")
	return cmd
}
