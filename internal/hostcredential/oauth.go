package hostcredential

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/novr/utsusemi/internal/target"
)

const DefaultOAuthTokenURL = "https://github.com/login/oauth/access_token"

type OAuthClient struct {
	HTTPClient *http.Client
	TokenURL   string
}

func (c *OAuthClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c *OAuthClient) tokenURL() string {
	if c.TokenURL != "" {
		return c.TokenURL
	}
	return DefaultOAuthTokenURL
}

type RefreshResult struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int
}

func (c *OAuthClient) RefreshGitHubToken(ctx context.Context, clientID, refreshToken string) (RefreshResult, error) {
	form := url.Values{}
	form.Set("client_id", clientID)
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.tokenURL(), strings.NewReader(form.Encode()))
	if err != nil {
		return RefreshResult{}, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := c.httpClient().Do(req)
	if err != nil {
		return RefreshResult{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return RefreshResult{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return RefreshResult{}, fmt.Errorf("refresh token failed: %s", strings.TrimSpace(string(body)))
	}

	var result struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
		Error        string `json:"error"`
		ErrorDesc    string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return RefreshResult{}, fmt.Errorf("parse refresh response: %w", err)
	}
	if result.Error != "" {
		if result.Error == "invalid_grant" {
			return RefreshResult{}, fmt.Errorf("%s: %s; %s", result.Error, result.ErrorDesc, ReconfigureAppHint)
		}
		return RefreshResult{}, fmt.Errorf("refresh token failed: %s", result.Error)
	}
	if result.AccessToken == "" || result.RefreshToken == "" {
		return RefreshResult{}, fmt.Errorf("refresh response missing tokens")
	}
	return RefreshResult{
		AccessToken:  result.AccessToken,
		RefreshToken: result.RefreshToken,
		ExpiresIn:    result.ExpiresIn,
	}, nil
}

func ExchangeHostJWT(ctx context.Context, client *http.Client, brokerURL, userToken string, tgt target.Target) (string, target.Target, error) {
	if client == nil {
		client = http.DefaultClient
	}
	targetPayload, err := TargetPayload(tgt)
	if err != nil {
		return "", target.Target{}, err
	}
	body, err := json.Marshal(map[string]any{"target": targetPayload})
	if err != nil {
		return "", target.Target{}, err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		strings.TrimRight(brokerURL, "/")+CredentialExchangePath,
		strings.NewReader(string(body)),
	)
	if err != nil {
		return "", target.Target{}, err
	}
	req.Header.Set("Authorization", "Bearer "+userToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return "", target.Target{}, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", target.Target{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return "", target.Target{}, fmt.Errorf("exchange failed: %s", string(respBody))
	}

	var result struct {
		Credential string         `json:"credential"`
		Target     map[string]any `json:"target"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", target.Target{}, err
	}
	confirmed, err := ParseTargetMap(result.Target)
	if err != nil {
		return "", target.Target{}, err
	}
	if result.Credential == "" {
		return "", target.Target{}, fmt.Errorf("exchange returned empty credential")
	}
	return result.Credential, confirmed, nil
}

const defaultGitHubUserURL = "https://api.github.com/user"

func FetchGitHubUserLogin(ctx context.Context, client *http.Client, accessToken string) (string, error) {
	return fetchGitHubUserLogin(ctx, client, accessToken, defaultGitHubUserURL)
}

func fetchGitHubUserLogin(ctx context.Context, client *http.Client, accessToken, userURL string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, userURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github user lookup failed: %s", strings.TrimSpace(string(body)))
	}
	var user struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &user); err != nil {
		return "", fmt.Errorf("parse github user: %w", err)
	}
	if user.Login == "" {
		return "", fmt.Errorf("github user login missing")
	}
	return user.Login, nil
}
