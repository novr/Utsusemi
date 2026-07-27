package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

const publicAppClientID = "Iv1.CHANGE_ME"

func newRegisterCmd() *cobra.Command {
	var (
		brokerURL   string
		org         string
		repo        string
		runnerGroup int64
		clientID    string
		outputPath  string
	)

	cmd := &cobra.Command{
		Use:   "register",
		Short: "Register host with Public App broker",
		RunE: func(cmd *cobra.Command, args []string) error {
			if brokerURL == "" {
				return fmt.Errorf("--broker is required")
			}
			if org == "" && repo == "" {
				return fmt.Errorf("either --org or --repo is required")
			}
			if clientID == "" {
				clientID = publicAppClientID
			}

			tgt, err := target.FromConfig(config.TargetYAML(org, repo, runnerGroup))
			if err != nil {
				return err
			}

			userToken, err := deviceFlow(cmd.Context(), clientID)
			if err != nil {
				return err
			}
			defer func() { userToken = "" }()

			credential, confirmedTarget, err := exchangeCredential(cmd.Context(), brokerURL, userToken, tgt)
			if err != nil {
				return err
			}

			cfg := &config.Config{
				Target: config.TargetYAML(confirmedTarget.Org, formatRepo(confirmedTarget), confirmedTarget.RunnerGroupID),
				Labels: []string{"self-hosted", "macOS", "tart", "arm64"},
				Registration: config.Registration{
					Mode:      config.ModeHostedApp,
					BrokerURL: brokerURL,
				},
				Provider:      "tart",
				BaseImage:     "ghcr.io/cirruslabs/macos-sequoia-base:latest",
				RunnerVersion: "2.336.0",
				PoolSize:      1,
			}
			config.ApplyDefaults(cfg)

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

	cmd.Flags().StringVar(&brokerURL, "broker", "", "broker base URL")
	cmd.Flags().StringVar(&org, "org", "", "GitHub organization")
	cmd.Flags().StringVar(&repo, "repo", "", "GitHub repository (owner/repo)")
	cmd.Flags().Int64Var(&runnerGroup, "runner-group-id", 1, "runner group id for org target")
	cmd.Flags().StringVar(&clientID, "client-id", "", "GitHub App client ID")
	cmd.Flags().StringVar(&outputPath, "output", configPath, "config output path")
	return cmd
}

func formatRepo(tgt target.Target) string {
	if tgt.Type != target.TypeRepo {
		return ""
	}
	return tgt.Owner + "/" + tgt.Repo
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
	switch tgt.Type {
	case target.TypeOrg:
		return map[string]any{
			"type":            "org",
			"org":             tgt.Org,
			"runner_group_id": tgt.RunnerGroupID,
		}
	case target.TypeRepo:
		return map[string]any{
			"type":  "repo",
			"owner": tgt.Owner,
			"repo":  tgt.Repo,
		}
	default:
		return map[string]any{}
	}
}

func parseTargetMap(raw map[string]any) (target.Target, error) {
	typ, _ := raw["type"].(string)
	switch typ {
	case "org":
		org, _ := raw["org"].(string)
		group, _ := raw["runner_group_id"].(float64)
		return target.Target{Type: target.TypeOrg, Org: org, RunnerGroupID: int64(group)}, nil
	case "repo":
		owner, _ := raw["owner"].(string)
		repo, _ := raw["repo"].(string)
		return target.Target{Type: target.TypeRepo, Owner: owner, Repo: repo}, nil
	default:
		return target.Target{}, fmt.Errorf("invalid target in response")
	}
}

func init() {
	_ = os.Getenv
}
