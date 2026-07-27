package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

const (
	publicAppClientID  = "Iv23ctWrJ3Yq0JDLEa85"
	publicAppBrokerURL = "https://utsusemi-broker.novrd.workers.dev"
)

func newRegisterCmd() *cobra.Command {
	var (
		brokerURL   string
		org         string
		runnerGroup int64
		outputPath  string
		opts        runnerOptions
	)

	cmd := &cobra.Command{
		Use:     "register",
		Short:   "Register host with Public App broker",
		Example: `  utsusemi register --org my-org`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !strings.HasPrefix(brokerURL, "https://") &&
				!strings.HasPrefix(brokerURL, "http://127.0.0.1") &&
				!strings.HasPrefix(brokerURL, "http://localhost") {
				return fmt.Errorf("--broker must use https")
			}
			if org == "" {
				return fmt.Errorf("--org is required")
			}

			tgt, err := target.FromConfig(config.TargetYAML(org, "", runnerGroup))
			if err != nil {
				return err
			}

			userToken, err := deviceFlow(cmd.Context(), publicAppClientID)
			if err != nil {
				return err
			}

			credential, confirmedTarget, err := exchangeCredential(cmd.Context(), brokerURL, userToken, tgt)
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
			if _, err := config.Validate(cfg, 2); err != nil {
				return err
			}

			store := keychain.New()
			if err := store.Set(cfg.CredentialService(), cfg.CredentialAccount(), credential); err != nil {
				return err
			}
			if outputPath != "" {
				if err := writeConfig(outputPath, cfg); err != nil {
					return err
				}
				fmt.Printf("wrote config to %s\n", outputPath)
			}
			fmt.Println("registration complete; credential stored in keychain")
			return nil
		},
	}

	cmd.Flags().StringVar(&brokerURL, "broker", publicAppBrokerURL, "broker base URL")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "runner group id for org target")
	cmd.Flags().StringVar(&outputPath, "output", configPath, "config output path")
	addRunnerFlags(cmd, &opts)
	return cmd
}

func deviceFlow(ctx context.Context, clientID string) (string, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", "")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/device/code", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device code request failed: %s", string(body))
	}

	var start struct {
		DeviceCode      string `json:"device_code"`
		UserCode        string `json:"user_code"`
		VerificationURI string `json:"verification_uri"`
		ExpiresIn       int    `json:"expires_in"`
		Interval        int    `json:"interval"`
	}
	if err := json.Unmarshal(body, &start); err != nil {
		return "", err
	}

	fmt.Printf("Visit %s and enter code: %s\n", start.VerificationURI, start.UserCode)
	interval := time.Duration(start.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(interval):
		}

		tokenForm := url.Values{}
		tokenForm.Set("client_id", clientID)
		tokenForm.Set("device_code", start.DeviceCode)
		tokenForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

		tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://github.com/login/oauth/access_token", strings.NewReader(tokenForm.Encode()))
		if err != nil {
			return "", err
		}
		tokenReq.Header.Set("Accept", "application/json")
		tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		tokenResp, err := http.DefaultClient.Do(tokenReq)
		if err != nil {
			return "", err
		}
		tokenBody, err := io.ReadAll(tokenResp.Body)
		tokenResp.Body.Close()
		if err != nil {
			return "", err
		}

		var tokenResult struct {
			AccessToken string `json:"access_token"`
			Error       string `json:"error"`
			Interval    int    `json:"interval"`
		}
		if err := json.Unmarshal(tokenBody, &tokenResult); err != nil {
			return "", err
		}
		switch tokenResult.Error {
		case "":
			if tokenResult.AccessToken == "" {
				return "", fmt.Errorf("empty access token")
			}
			return tokenResult.AccessToken, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "access_denied":
			return "", fmt.Errorf("device authorization was denied or cancelled; run register again and approve the GitHub App")
		case "expired_token":
			return "", fmt.Errorf("device code expired; run register again")
		default:
			return "", fmt.Errorf("device flow failed: %s", tokenResult.Error)
		}
	}
	return "", fmt.Errorf("device flow timed out")
}

func exchangeCredential(ctx context.Context, brokerURL, userToken string, tgt target.Target) (string, target.Target, error) {
	payload := map[string]any{"target": targetPayload(tgt)}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", target.Target{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(brokerURL, "/")+"/v1/register/exchange", strings.NewReader(string(body)))
	if err != nil {
		return "", target.Target{}, err
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", target.Target{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", target.Target{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", target.Target{}, fmt.Errorf("register exchange failed: %s", string(respBody))
	}

	var result struct {
		Credential string         `json:"credential"`
		Target     map[string]any `json:"target"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", target.Target{}, err
	}
	confirmed, err := parseTargetMap(result.Target)
	if err != nil {
		return "", target.Target{}, err
	}
	return result.Credential, confirmed, nil
}

func targetPayload(tgt target.Target) map[string]any {
	if tgt.Type != target.TypeOrg {
		return map[string]any{}
	}
	return map[string]any{
		"type":            "org",
		"org":             tgt.Org,
		"runner_group_id": tgt.RunnerGroupID,
	}
}

func parseTargetMap(raw map[string]any) (target.Target, error) {
	typ, _ := raw["type"].(string)
	if typ != "org" {
		return target.Target{}, fmt.Errorf("invalid target in response")
	}
	org, _ := raw["org"].(string)
	group, _ := raw["runner_group_id"].(float64)
	return target.Target{Type: target.TypeOrg, Org: org, RunnerGroupID: int64(group)}, nil
}
