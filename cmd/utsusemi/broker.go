package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/novr/utsusemi/internal/brokerhttp"
	"github.com/spf13/cobra"
)

func newBrokerCmd() *cobra.Command {
	var (
		listen         string
		appID          string
		appKeyFile     string
		signingKeyFile string
		envFile        string
		issuer         string
		jwtVersion     string
	)
	cmd := &cobra.Command{
		Use:   "broker",
		Short: "Run a GitHub App broker on 127.0.0.1",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			addr, err := brokerhttp.ParseListen(listen)
			if err != nil {
				return err
			}
			secrets, err := brokerhttp.LoadBrokerSecrets(brokerhttp.SourceConfig{
				AppID:             appID,
				AppPrivateKeyFile: appKeyFile,
				SigningKeyFile:    signingKeyFile,
				EnvFile:           envFile,
				JWTIssuer:         issuer,
				JWTVersion:        jwtVersion,
			})
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
	cmd.Flags().StringVar(&appID, "app-id", "", "GitHub App ID")
	cmd.Flags().StringVar(&appKeyFile, "app-private-key-file", "", "GitHub App private key PEM file")
	cmd.Flags().StringVar(&signingKeyFile, "credential-signing-key-file", "", "host JWT signing key PEM file (required on Linux)")
	cmd.Flags().StringVar(&envFile, "env-file", "", "optional KEY=VALUE file (e.g. systemd EnvironmentFile)")
	cmd.Flags().StringVar(&issuer, "jwt-issuer", "", "host JWT issuer (default: utsusemi-broker)")
	cmd.Flags().StringVar(&jwtVersion, "jwt-version", "", "host JWT version (default: 1)")
	return cmd
}
