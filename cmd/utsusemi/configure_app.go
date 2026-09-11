package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/brokerhttp"
	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/credentialview"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/target"
)

func newConfigureAppCmd() *cobra.Command {
	var (
		oauthClientID string
		brokerURL     string
		org           string
		runnerGroup   int64
		outputPath    string
		force         bool
		refresh       bool
		opts          runnerOptions
	)

	cmd := &cobra.Command{
		Use:   "app",
		Short: "Configure with the Utsusemi GitHub App",
		Example: `  utsusemi configure app --org my-org
  utsusemi configure app --pool-size 2
  utsusemi configure app --runner-version latest
  utsusemi configure app --refresh`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if runnerGroup <= 0 {
				runnerGroup = 1
			}
			if err := config.ValidateBrokerURL(brokerURL); err != nil {
				return err
			}
			path := configureOutputPath(cmd, outputPath)
			if refresh {
				if err := ensureConfigureAppRefreshCompatible(path); err != nil {
					return err
				}
			}
			if !configureOnlyCredentialRefresh(cmd, refresh) {
				if err := confirmConfigOverwrite(path, force, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
					return err
				}
			}

			merge, err := mergeConfigureConfig(cmd, configureMergeInput{
				Mode:       configureModeApp,
				OutputPath: path,
				Target: configureTargetInput{
					Org:         org,
					RunnerGroup: runnerGroup,
				},
				App: configureAppInput{
					BrokerURL:     brokerURL,
					OAuthClientID: strings.TrimSpace(oauthClientID),
				},
				Opts: opts,
			})
			if err != nil {
				return err
			}

			success := configureSuccess{WroteConfig: true}
			refreshed := false
			if (refresh || needsAppDeviceFlow(merge.Config, merge)) &&
				brokerhttp.IsLoopbackBrokerURL(merge.Config.Registration.BrokerURL) {
				if err := brokerhttp.CheckReachable(cmd.Context(), merge.Config.Registration.BrokerURL); err != nil {
					return err
				}
			}
			if refresh {
				githubUser, err := tryAppOAuthRefresh(cmd.Context(), merge.Config)
				if err == nil {
					success.CredentialUpdated = true
					success.GitHubUser = githubUser
					refreshed = true
				} else {
					fmt.Fprintf(cmd.ErrOrStderr(), "OAuth refresh failed: %v; starting device flow\n", err)
				}
			}
			if !refreshed && (refresh || needsAppDeviceFlow(merge.Config, merge)) {
				githubUser, credential, err := runAppDeviceFlow(cmd, merge.Config)
				if err != nil {
					return err
				}
				if err := saveCredential(merge.Config, credential); err != nil {
					return err
				}
				success.CredentialUpdated = true
				success.GitHubUser = githubUser
			}

			if configureAppConfigFlagsChanged(cmd) || !refresh || !refreshed {
				if err := writeConfig(path, merge.Config); err != nil {
					return err
				}
			} else {
				success.WroteConfig = false
			}
			printConfigureSuccess(path, success)
			return nil
		},
	}

	cmd.Flags().StringVar(&brokerURL, "broker", config.DefaultHostedAppBrokerURL, "broker base URL")
	cmd.Flags().StringVar(&oauthClientID, "oauth-client-id", "", "GitHub App OAuth client ID (defaults to the public Utsusemi App)")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "runner group id for org target")
	cmd.Flags().StringVar(&outputPath, "output", configPath, "config output path")
	cmd.Flags().BoolVar(&force, "force", false, "update existing config without prompting")
	cmd.Flags().BoolVar(&refresh, "refresh", false, "refresh hosted_app credential via OAuth (falls back to device flow)")
	addRunnerFlags(cmd, &opts)
	return cmd
}

func tryAppOAuthRefresh(ctx context.Context, cfg *config.Config) (string, error) {
	tgt, err := target.FromConfig(cfg.Target)
	if err != nil {
		return "", err
	}
	if err := config.ValidateBrokerURL(cfg.Registration.BrokerURL); err != nil {
		return "", err
	}
	store := credentialStoreOrDefault()
	mgr := hostcredential.NewManager(hostcredential.ManagerOptions{
		Store:         store,
		Service:       cfg.CredentialService(),
		Account:       cfg.CredentialAccount(),
		BrokerURL:     cfg.Registration.BrokerURL,
		OAuthClientID: cfg.Registration.OAuthClientID,
		LockPath:      filepath.Join(cfg.StateDir, "credential.refresh.lock"),
		HTTPClient:    &http.Client{Timeout: 30 * time.Second},
	})
	if _, err := mgr.EnsureFresh(ctx, tgt, true); err != nil {
		return "", err
	}
	info, err := credentialview.Load(cfg, store)
	if err != nil {
		return "", err
	}
	if !info.Present {
		return "", fmt.Errorf("credential missing after refresh")
	}
	return info.GitHubUser, nil
}

func runAppDeviceFlow(cmd *cobra.Command, cfg *config.Config) (string, string, error) {
	tgt, err := target.FromConfig(cfg.Target)
	if err != nil {
		return "", "", err
	}
	brokerURL := cfg.Registration.BrokerURL
	if err := config.ValidateBrokerURL(brokerURL); err != nil {
		return "", "", err
	}

	flowClient := &hostcredential.DeviceFlowClient{}
	flow, err := flowClient.Authorize(cmd.Context(), hostcredential.ResolveOAuthClientID(cfg.Registration.OAuthClientID), hostcredential.DeviceFlowPrompt{
		WriteUserCode: func(userCode, verificationURI string) {
			fmt.Fprintf(cmd.OutOrStdout(), "GitHub device code: %s\n", userCode)
			fmt.Fprintf(cmd.OutOrStdout(), "Verification URL: %s\n", verificationURI)
		},
		OpenBrowser: func(verificationURI string) error {
			return promptAndOpenBrowser(cmd.InOrStdin(), cmd.OutOrStdout(), verificationURI)
		},
	})
	if err != nil {
		return "", "", err
	}

	githubUser, err := hostcredential.FetchGitHubUserLogin(cmd.Context(), http.DefaultClient, flow.AccessToken)
	if err != nil {
		return "", "", fmt.Errorf("lookup github user: %w", err)
	}

	hostJWT, confirmedTarget, err := hostcredential.ExchangeHostJWT(
		cmd.Context(),
		http.DefaultClient,
		brokerURL,
		flow.AccessToken,
		tgt,
	)
	if err != nil {
		return "", "", fmt.Errorf("configure app exchange failed: %w", err)
	}

	credential, err := hostcredential.NewBundle(hostJWT, flow.RefreshToken, githubUser)
	if err != nil {
		return "", "", err
	}

	cfg.Target = config.TargetYAML(confirmedTarget.Org, "", confirmedTarget.RunnerGroupID)
	return githubUser, credential, nil
}

func openBrowser(rawURL string) error {
	return exec.Command("open", rawURL).Start()
}

func promptAndOpenBrowser(in io.Reader, out io.Writer, rawURL string) error {
	inFile, inputIsFile := in.(*os.File)
	outFile, outputIsFile := out.(*os.File)
	if !inputIsFile || !outputIsFile || !isTerminal(inFile) || !isTerminal(outFile) {
		return nil
	}

	fmt.Fprint(out, "Copy the code, then press Enter to open GitHub in your browser: ")
	if _, err := bufio.NewReader(in).ReadString('\n'); err != nil && err != io.EOF {
		return fmt.Errorf("wait for browser confirmation: %w", err)
	}
	if err := openBrowser(rawURL); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	return nil
}
