package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/target"
)

const publicAppBrokerURL = "https://utsusemi-broker.novrd.workers.dev"

type deviceFlowResult struct {
	AccessToken  string
	RefreshToken string
}

func newConfigureAppCmd() *cobra.Command {
	var (
		brokerURL   string
		org         string
		runnerGroup int64
		outputPath  string
		force       bool
		opts        runnerOptions
	)

	cmd := &cobra.Command{
		Use:     "app",
		Short:   "Configure with the Utsusemi GitHub App",
		Example: `  utsusemi configure app --org my-org`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !strings.HasPrefix(brokerURL, "https://") &&
				!strings.HasPrefix(brokerURL, "http://127.0.0.1") &&
				!strings.HasPrefix(brokerURL, "http://localhost") {
				return fmt.Errorf("--broker must use https")
			}
			if org == "" {
				return fmt.Errorf("--org is required")
			}
			if err := confirmConfigOverwrite(outputPath, force, cmd.InOrStdin(), cmd.OutOrStdout()); err != nil {
				return err
			}

			tgt, err := target.FromConfig(config.TargetYAML(org, "", runnerGroup))
			if err != nil {
				return err
			}

			flow, err := deviceFlow(
				cmd.Context(),
				hostcredential.PublicAppClientID,
				cmd.InOrStdin(),
				cmd.OutOrStdout(),
			)
			if err != nil {
				return err
			}

			hostJWT, confirmedTarget, err := hostcredential.ExchangeHostJWT(
				cmd.Context(),
				http.DefaultClient,
				brokerURL,
				flow.AccessToken,
				tgt,
			)
			if err != nil {
				return fmt.Errorf("configure app exchange failed: %w", err)
			}

			credential, err := hostcredential.NewBundle(hostJWT, flow.RefreshToken)
			if err != nil {
				return err
			}

			cfg := &config.Config{
				Target: config.TargetYAML(confirmedTarget.Org, "", confirmedTarget.RunnerGroupID),
				Registration: config.Registration{
					Mode:      config.ModeHostedApp,
					BrokerURL: brokerURL,
				},
			}
			opts.apply(cfg)
			config.ApplyDefaults(cfg)
			if _, err := config.Validate(cfg, providerMaxConcurrent()); err != nil {
				return err
			}

			if err := saveCredential(cfg, credential); err != nil {
				return err
			}
			if err := writeConfig(outputPath, cfg); err != nil {
				return err
			}
			printConfigureSuccess(outputPath)
			return nil
		},
	}

	cmd.Flags().StringVar(&brokerURL, "broker", publicAppBrokerURL, "broker base URL")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "runner group id for org target")
	cmd.Flags().StringVar(&outputPath, "output", configPath, "config output path")
	cmd.Flags().BoolVar(&force, "force", false, "overwrite existing config without prompting")
	addRunnerFlags(cmd, &opts)
	return cmd
}

func deviceFlow(ctx context.Context, clientID string, in io.Reader, out io.Writer) (deviceFlowResult, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", "")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		return deviceFlowResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return deviceFlowResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return deviceFlowResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return deviceFlowResult{}, fmt.Errorf("device code request failed: %s", string(body))
	}

	var start struct {
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &start); err != nil {
		return deviceFlowResult{}, err
	}

	fmt.Fprintf(out, "GitHub device code: %s\n", start.UserCode)
	fmt.Fprintf(out, "Verification URL: %s\n", start.VerificationURI)
	openURL := start.VerificationURIComplete
	if openURL == "" {
		openURL = start.VerificationURI
	}
	if openURL != "" {
		if err := promptAndOpenBrowser(in, out, openURL); err != nil {
			return deviceFlowResult{}, err
		}
	}
	interval := time.Duration(start.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return deviceFlowResult{}, ctx.Err()
		case <-time.After(interval):
		}

		tokenForm := url.Values{}
		tokenForm.Set("client_id", clientID)
		tokenForm.Set("device_code", start.DeviceCode)
		tokenForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

		tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(tokenForm.Encode()))
		if err != nil {
			return deviceFlowResult{}, err
		}
		tokenReq.Header.Set("Accept", "application/json")
		tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		tokenResp, err := http.DefaultClient.Do(tokenReq)
		if err != nil {
			return deviceFlowResult{}, err
		}
		tokenBody, err := io.ReadAll(tokenResp.Body)
		tokenResp.Body.Close()
		if err != nil {
			return deviceFlowResult{}, err
		}

		var tokenResult struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Error        string `json:"error"`
			Interval     int    `json:"interval"`
		}
		if err := json.Unmarshal(tokenBody, &tokenResult); err != nil {
			return deviceFlowResult{}, err
		}
		switch tokenResult.Error {
		case "":
			if err := validateDeviceFlowTokens(tokenResult.AccessToken, tokenResult.RefreshToken); err != nil {
				return deviceFlowResult{}, err
			}
			return deviceFlowResult{
				AccessToken:  tokenResult.AccessToken,
				RefreshToken: tokenResult.RefreshToken,
			}, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "access_denied":
			return deviceFlowResult{}, fmt.Errorf("device authorization was denied or cancelled; run `utsusemi configure app` again and approve the GitHub App")
		case "expired_token":
			return deviceFlowResult{}, fmt.Errorf("device code expired; run `utsusemi configure app` again")
		default:
			return deviceFlowResult{}, fmt.Errorf("device flow failed: %s", tokenResult.Error)
		}
	}
	return deviceFlowResult{}, fmt.Errorf("device flow timed out")
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

func validateDeviceFlowTokens(accessToken, refreshToken string) error {
	if accessToken == "" {
		return fmt.Errorf("empty access token")
	}
	if refreshToken == "" {
		return fmt.Errorf("missing refresh token; enable User-to-server token expiration for the GitHub App (Optional Features → Opt-in)")
	}
	return nil
}
