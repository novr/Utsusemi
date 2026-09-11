package brokerhttp

import (
	"bytes"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const (
	githubAPIVersion = "2022-11-28"
	userAgent        = "utsusemi-broker"
	maxInstallPages  = 10
	maxRunnerPages   = 20
)

type orgTarget struct {
	Org           string
	RunnerGroupID int64
}

func (e Env) githubBase() string {
	if e.GitHubAPI != "" {
		return strings.TrimRight(e.GitHubAPI, "/")
	}
	return "https://api.github.com"
}

func (e Env) httpClient() *http.Client {
	if e.HTTPClient != nil {
		return e.HTTPClient
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func createInstallationToken(env Env, installationID int64) (string, error) {
	if strings.TrimSpace(env.GitHubAppPrivateKey) == "" {
		return "", &httpError{status: http.StatusInternalServerError, message: "github app private key is not configured"}
	}
	appJWT, err := signAppJWT(env)
	if err != nil {
		return "", err
	}
	path := fmt.Sprintf("/app/installations/%d/access_tokens", installationID)
	var body struct {
		Token string `json:"token"`
	}
	if err := githubJSON(env, http.MethodPost, path, appJWT, nil, &body); err != nil {
		return "", githubAPIError()
	}
	if body.Token == "" {
		return "", githubAPIError()
	}
	return body.Token, nil
}

func createJIT(env Env, token string, tgt orgTarget, labels []string, name string) (map[string]any, error) {
	payload, err := json.Marshal(map[string]any{
		"name":            name,
		"runner_group_id": tgt.RunnerGroupID,
		"labels":          labels,
		"ephemeral":       true,
		"disable_update":  true,
	})
	if err != nil {
		return nil, err
	}
	path := fmt.Sprintf("/orgs/%s/actions/runners/generate-jitconfig", tgt.Org)
	var out map[string]any
	if err := githubJSON(env, http.MethodPost, path, token, payload, &out); err != nil {
		return nil, githubAPIError()
	}
	return out, nil
}

func deleteRunner(env Env, token string, tgt orgTarget, runnerID int64) error {
	path := fmt.Sprintf("/orgs/%s/actions/runners/%d", tgt.Org, runnerID)
	err := githubJSON(env, http.MethodDelete, path, token, nil, nil)
	if he, ok := err.(*httpError); ok && he.status == http.StatusNotFound {
		return nil
	}
	if err != nil {
		return githubAPIError()
	}
	return nil
}

func listRunners(env Env, token string, tgt orgTarget, prefix string) ([]map[string]any, error) {
	var runners []map[string]any
	for page := 1; page <= maxRunnerPages; page++ {
		path := fmt.Sprintf("/orgs/%s/actions/runners?per_page=100&page=%d", tgt.Org, page)
		var body struct {
			Runners []struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			} `json:"runners"`
		}
		if err := githubJSON(env, http.MethodGet, path, token, nil, &body); err != nil {
			return nil, githubAPIError()
		}
		for _, r := range body.Runners {
			if prefix == "" || strings.HasPrefix(r.Name, prefix) {
				runners = append(runners, map[string]any{"id": r.ID, "name": r.Name})
			}
		}
		if len(body.Runners) < 100 {
			if runners == nil {
				runners = []map[string]any{}
			}
			return runners, nil
		}
	}
	return nil, githubAPIError()
}

func findAppInstallation(env Env, userToken, org string) (int64, error) {
	appID, err := strconv.ParseInt(strings.TrimSpace(env.GitHubAppID), 10, 64)
	if err != nil || appID <= 0 {
		return 0, unauthorized()
	}
	wanted := strings.ToLower(org)
	for page := 1; page <= maxInstallPages; page++ {
		path := fmt.Sprintf("/user/installations?per_page=100&page=%d", page)
		var body struct {
			Installations []struct {
				ID      int64 `json:"id"`
				AppID   int64 `json:"app_id"`
				Account struct {
					Login string `json:"login"`
				} `json:"account"`
			} `json:"installations"`
		}
		if err := githubJSON(env, http.MethodGet, path, userToken, nil, &body); err != nil {
			return 0, unauthorized()
		}
		for _, item := range body.Installations {
			if item.AppID == appID && strings.EqualFold(item.Account.Login, wanted) {
				return item.ID, nil
			}
		}
		if len(body.Installations) < 100 {
			return 0, notFound("installation not found")
		}
	}
	return 0, notFound("installation not found")
}

func signAppJWT(env Env) (string, error) {
	key, err := parseRSAPrivateKey(env.GitHubAppPrivateKey)
	if err != nil {
		return "", &httpError{status: http.StatusInternalServerError, message: "github app private key is invalid"}
	}
	now := time.Now().Unix()
	header, _ := json.Marshal(map[string]string{"alg": "RS256", "typ": "JWT"})
	payload, _ := json.Marshal(map[string]any{
		"iat": now - 60,
		"exp": now + 9*60,
		"iss": env.GitHubAppID,
	})
	data := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	hashed := sha256.Sum256([]byte(data))
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, hashed[:])
	if err != nil {
		return "", githubAPIError()
	}
	return data + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func parseRSAPrivateKey(pemData string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("invalid pem")
	}
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("not rsa")
	}
	return key, nil
}

func githubJSON(env Env, method, path, token string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, env.githubBase()+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	req.Header.Set("User-Agent", userAgent)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := env.httpClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpError{status: resp.StatusCode, message: strings.TrimSpace(string(respBody))}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}
