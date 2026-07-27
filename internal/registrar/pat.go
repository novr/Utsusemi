package registrar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

const githubAPI = "https://api.github.com"

type GitHubPATRegistrar struct {
	client  *http.Client
	store   keychain.Store
	service string
	account string
}

func NewGitHubPATRegistrar(store keychain.Store, service, account string) *GitHubPATRegistrar {
	return &GitHubPATRegistrar{
		client:  &http.Client{Timeout: 30 * time.Second},
		store:   store,
		service: service,
		account: account,
	}
}

func (r *GitHubPATRegistrar) ValidateCredential(ctx context.Context, service, account string) error {
	_, err := r.token()
	if err != nil {
		return fmt.Errorf("credential missing from keychain: %w", err)
	}
	return nil
}

func (r *GitHubPATRegistrar) token() (string, error) {
	return r.store.Get(r.service, r.account)
}

func (r *GitHubPATRegistrar) CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (JITConfig, error) {
	token, err := r.token()
	if err != nil {
		return JITConfig{}, err
	}

	var path string
	var body any
	switch tgt.Type {
	case target.TypeOrg:
		path = fmt.Sprintf("/orgs/%s/actions/runners/generate-jitconfig", tgt.Org)
		body = map[string]any{
			"name":              name,
			"runner_group_id":   tgt.RunnerGroupID,
			"labels":            labels,
			"work_folder":       "_work",
			"ephemeral":         true,
			"disable_update":    true,
		}
	case target.TypeRepo:
		path = fmt.Sprintf("/repos/%s/%s/actions/runners/generate-jitconfig", tgt.Owner, tgt.Repo)
		body = map[string]any{
			"name":           name,
			"labels":         labels,
			"work_folder":    "_work",
			"ephemeral":      true,
			"disable_update": true,
		}
	default:
		return JITConfig{}, fmt.Errorf("unsupported target type")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return JITConfig{}, err
	}

	var resp jitResponse
	if err := r.doWithRetry(ctx, http.MethodPost, path, token, payload, &resp); err != nil {
		return JITConfig{}, err
	}
	return JITConfig{
		Encoded: resp.EncodedJITConfig,
		Runner: Runner{
			ID:   resp.Runner.ID,
			Name: resp.Runner.Name,
		},
	}, nil
}

func (r *GitHubPATRegistrar) DeleteRunner(ctx context.Context, tgt target.Target, runnerID int64) error {
	token, err := r.token()
	if err != nil {
		return err
	}
	path, err := deletePath(tgt, runnerID)
	if err != nil {
		return err
	}
	return r.doWithRetry(ctx, http.MethodDelete, path, token, nil, nil)
}

func (r *GitHubPATRegistrar) ListRunners(ctx context.Context, tgt target.Target, prefix string) ([]Runner, error) {
	token, err := r.token()
	if err != nil {
		return nil, err
	}
	path, err := listPath(tgt)
	if err != nil {
		return nil, err
	}

	var runners []Runner
	page := 1
	for {
		pagedPath := fmt.Sprintf("%s?per_page=100&page=%d", path, page)
		var resp listResponse
		if err := r.doWithRetry(ctx, http.MethodGet, pagedPath, token, nil, &resp); err != nil {
			return nil, err
		}
		for _, item := range resp.Runners {
			if prefix == "" || strings.HasPrefix(item.Name, prefix) {
				runners = append(runners, Runner{ID: item.ID, Name: item.Name})
			}
		}
		if len(resp.Runners) < 100 {
			break
		}
		page++
	}
	return runners, nil
}

type jitResponse struct {
	EncodedJITConfig string `json:"encoded_jit_config"`
	Runner           struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runner"`
}

type listResponse struct {
	Runners []struct {
		ID   int64  `json:"id"`
		Name string `json:"name"`
	} `json:"runners"`
}

func listPath(tgt target.Target) (string, error) {
	switch tgt.Type {
	case target.TypeOrg:
		return fmt.Sprintf("/orgs/%s/actions/runners", tgt.Org), nil
	case target.TypeRepo:
		return fmt.Sprintf("/repos/%s/%s/actions/runners", tgt.Owner, tgt.Repo), nil
	default:
		return "", fmt.Errorf("unsupported target type")
	}
}

func deletePath(tgt target.Target, runnerID int64) (string, error) {
	switch tgt.Type {
	case target.TypeOrg:
		return fmt.Sprintf("/orgs/%s/actions/runners/%d", tgt.Org, runnerID), nil
	case target.TypeRepo:
		return fmt.Sprintf("/repos/%s/%s/actions/runners/%d", tgt.Owner, tgt.Repo, runnerID), nil
	default:
		return "", fmt.Errorf("unsupported target type")
	}
}

func (r *GitHubPATRegistrar) doWithRetry(ctx context.Context, method, path, token string, body []byte, out any) error {
	backoff := time.Second
	for attempt := 0; attempt < 5; attempt++ {
		err := r.do(ctx, method, path, token, body, out)
		if err == nil {
			return nil
		}
		if !isRetryable(err) || attempt == 4 {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}
	return fmt.Errorf("request failed after retries")
}

func isRetryable(err error) bool {
	apiErr, ok := err.(*apiError)
	if !ok {
		return false
	}
	return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
}

type apiError struct {
	StatusCode int
	Message    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("github api %d: %s", e.StatusCode, e.Message)
}

func (r *GitHubPATRegistrar) do(ctx context.Context, method, path, token string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, githubAPI+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &apiError{StatusCode: resp.StatusCode, Message: strings.TrimSpace(string(respBody))}
	}
	if out == nil || len(respBody) == 0 {
		return nil
	}
	return json.Unmarshal(respBody, out)
}
