package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/novr/utsusemi/internal/brokerhttp"
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/spf13/cobra"
)

func newBrokerCmd() *cobra.Command {
	var (
		listen     string
		appID      string
		appKeyFile string
		issuer     string
		jwtVersion string
	)
	cmd := &cobra.Command{
		Use:   "broker",
		Short: "Run a local GitHub App broker on 127.0.0.1",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr, err := brokerhttp.ParseListen(listen)
			if err != nil {
				return err
			}
			store := keychain.New()
			secrets, err := loadOrStoreBrokerSecrets(store, appID, appKeyFile, issuer, jwtVersion)
			if err != nil {
				return err
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			srv := &http.Server{
				Handler:           brokerhttp.Handler(secrets.Env()),
				ReadHeaderTimeout: 10 * time.Second,
			}
			ctx, stop := signal.NotifyContext(cmd.Context(), syscall.SIGINT, syscall.SIGTERM)
			defer stop()
			go func() {
				<-ctx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				_ = srv.Shutdown(shutdownCtx)
			}()
			fmt.Fprintf(cmd.OutOrStdout(), "utsusemi broker listening on http://%s\n", addr)
			err = srv.Serve(ln)
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		},
	}
	cmd.Flags().StringVar(&listen, "listen", brokerhttp.DefaultListen, "listen address (127.0.0.1 only)")
	cmd.Flags().StringVar(&appID, "app-id", "", "GitHub App ID (required on first run)")
	cmd.Flags().StringVar(&appKeyFile, "app-private-key-file", "", "GitHub App private key PEM file (required on first run)")
	cmd.Flags().StringVar(&issuer, "jwt-issuer", brokerhttp.DefaultJWTIssuer, "host JWT issuer")
	cmd.Flags().StringVar(&jwtVersion, "jwt-version", brokerhttp.DefaultJWTVersion, "host JWT version")
	return cmd
}

func loadOrStoreBrokerSecrets(store keychain.Store, appID, appKeyFile, issuer, jwtVersion string) (brokerhttp.Secrets, error) {
	existing, loadErr := brokerhttp.LoadSecrets(store, brokerhttp.DefaultKeychainService, config.DefaultCredentialAccount)
	if loadErr == nil && strings.TrimSpace(appID) == "" && appKeyFile == "" {
		return existing, nil
	}
	if strings.TrimSpace(appID) == "" || appKeyFile == "" {
		return brokerhttp.Secrets{}, fmt.Errorf("broker credentials missing; pass --app-id and --app-private-key-file")
	}
	pemBytes, err := os.ReadFile(appKeyFile)
	if err != nil {
		return brokerhttp.Secrets{}, fmt.Errorf("read app private key: %w", err)
	}
	secrets := brokerhttp.Secrets{}
	if loadErr == nil {
		secrets = existing
	}
	secrets.GitHubAppID = strings.TrimSpace(appID)
	secrets.GitHubAppPrivateKey = string(pemBytes)
	secrets.JWTIssuer = issuer
	secrets.JWTVersion = jwtVersion
	if strings.TrimSpace(secrets.SigningKeyPEM) == "" {
		pem, genErr := brokerhttp.GenerateSigningKeyPEM()
		if genErr != nil {
			return brokerhttp.Secrets{}, genErr
		}
		secrets.SigningKeyPEM = pem
	}
	if err := brokerhttp.SaveSecrets(store, brokerhttp.DefaultKeychainService, config.DefaultCredentialAccount, secrets); err != nil {
		return brokerhttp.Secrets{}, err
	}
	return secrets, nil
}
