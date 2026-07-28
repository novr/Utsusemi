package hostcredential

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultDeviceCodeURL = "https://github.com/login/device/code"

type DeviceFlowResult struct {
	AccessToken  string
	RefreshToken string
}

type DeviceFlowPrompt struct {
	WriteUserCode func(userCode, verificationURI string)
	OpenBrowser   func(verificationURI string) error
}

type DeviceFlowClient struct {
	HTTPClient    *http.Client
	DeviceCodeURL string
	TokenURL      string
}

func (c *DeviceFlowClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *DeviceFlowClient) deviceCodeURL() string {
	if c.DeviceCodeURL != "" {
		return c.DeviceCodeURL
	}
	return DefaultDeviceCodeURL
}

func (c *DeviceFlowClient) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return DefaultOAuthTokenURL
}

func (c *DeviceFlowClient) Authorize(ctx context.Context, clientID string, prompt DeviceFlowPrompt) (DeviceFlowResult, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("scope", "")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.deviceCodeURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return DeviceFlowResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return DeviceFlowResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return DeviceFlowResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return DeviceFlowResult{}, fmt.Errorf("device code request failed: %s", string(body))
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
		return DeviceFlowResult{}, err
	}

	if prompt.WriteUserCode != nil {
		prompt.WriteUserCode(start.UserCode, start.VerificationURI)
	}
	openURL := start.VerificationURIComplete
	if openURL == "" {
		openURL = start.VerificationURI
	}
	if openURL != "" && prompt.OpenBrowser != nil {
		if err := prompt.OpenBrowser(openURL); err != nil {
			return DeviceFlowResult{}, err
		}
	}

	interval := time.Duration(start.Interval) * time.Second
	deadline := time.Now().Add(time.Duration(start.ExpiresIn) * time.Second)

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return DeviceFlowResult{}, ctx.Err()
		case <-time.After(interval):
		}

		tokenForm := url.Values{}
		tokenForm.Set("client_id", clientID)
		tokenForm.Set("device_code", start.DeviceCode)
		tokenForm.Set("grant_type", "urn:ietf:params:oauth:grant-type:device_code")

		tokenReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL(), strings.NewReader(tokenForm.Encode()))
		if err != nil {
			return DeviceFlowResult{}, err
		}
		tokenReq.Header.Set("Accept", "application/json")
		tokenReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		tokenResp, err := c.httpClient().Do(tokenReq)
		if err != nil {
			return DeviceFlowResult{}, err
		}
		tokenBody, err := io.ReadAll(tokenResp.Body)
		tokenResp.Body.Close()
		if err != nil {
			return DeviceFlowResult{}, err
		}

		var tokenResult struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
			Error        string `json:"error"`
			Interval     int    `json:"interval"`
		}
		if err := json.Unmarshal(tokenBody, &tokenResult); err != nil {
			return DeviceFlowResult{}, err
		}
		switch tokenResult.Error {
		case "":
			if err := validateDeviceFlowTokens(tokenResult.AccessToken, tokenResult.RefreshToken); err != nil {
				return DeviceFlowResult{}, err
			}
			return DeviceFlowResult{
				AccessToken:  tokenResult.AccessToken,
				RefreshToken: tokenResult.RefreshToken,
			}, nil
		case "authorization_pending":
			continue
		case "slow_down":
			interval += 5 * time.Second
			continue
		case "access_denied":
			return DeviceFlowResult{}, fmt.Errorf("device authorization was denied or cancelled; %s and approve the GitHub App", ReconfigureAppHint)
		case "expired_token":
			return DeviceFlowResult{}, fmt.Errorf("device code expired; %s", ReconfigureAppHint)
		default:
			return DeviceFlowResult{}, fmt.Errorf("device flow failed: %s", tokenResult.Error)
		}
	}
	return DeviceFlowResult{}, fmt.Errorf("device flow timed out")
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
