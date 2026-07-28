package registrar

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

const githubAPI = "https://api.github.com"

type GitHubPATRegistrar struct {
	api     *httpClient
	store   keychain.Store
	service string
	account string
}

func NewGitHubPATRegistrar(store keychain.Store, service, account string) *GitHubPATRegistrar {
	client := &http.Client{Timeout: 30 * time.Second}
	return &GitHubPATRegistrar{
		api:     newGitHubHTTPClient(client),
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
			"name":            name,
			"runner_group_id": tgt.RunnerGroupID,
			"labels":          labels,
			"work_folder":     "_work",
			"ephemeral":       true,
			"disable_update":  true,
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
	if err := r.api.doWithRetry(ctx, http.MethodPost, path, token, payload, &resp); err != nil {
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
	return r.api.doWithRetry(ctx, http.MethodDelete, path, token, nil, nil)
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
		if err := r.api.doWithRetry(ctx, http.MethodGet, pagedPath, token, nil, &resp); err != nil {
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
