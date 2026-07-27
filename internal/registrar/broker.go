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

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

type BrokerRegistrar struct {
	client  *http.Client
	store   keychain.Store
	cfg     *config.Config
	baseURL string
}

func NewBrokerRegistrar(store keychain.Store, cfg *config.Config) *BrokerRegistrar {
	return &BrokerRegistrar{
		client:  &http.Client{Timeout: 30 * time.Second},
		store:   store,
		cfg:     cfg,
		baseURL: strings.TrimRight(cfg.Registration.BrokerURL, "/"),
	}
}

func (r *BrokerRegistrar) ValidateCredential(ctx context.Context, service, account string) error {
	_, err := r.credential()
	if err != nil {
		return fmt.Errorf("credential missing from keychain: %w", err)
	}
	return nil
}

func (r *BrokerRegistrar) credential() (string, error) {
	return r.store.Get(r.cfg.CredentialService(), r.cfg.CredentialAccount())
}

func (r *BrokerRegistrar) CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (JITConfig, error) {
	token, err := r.credential()
	if err != nil {
		return JITConfig{}, err
	}
	reqBody := map[string]any{
		"target": targetPayload(tgt),
		"labels": labels,
		"name":   name,
	}
	var resp jitResponse
	if err := r.post(ctx, "/v1/jitconfig", token, reqBody, &resp); err != nil {
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

func (r *BrokerRegistrar) DeleteRunner(ctx context.Context, tgt target.Target, runnerID int64) error {
	token, err := r.credential()
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/runners/%d", runnerID)
	reqBody := map[string]any{"target": targetPayload(tgt)}
	return r.delete(ctx, path, token, reqBody)
}

func (r *BrokerRegistrar) ListRunners(ctx context.Context, tgt target.Target, prefix string) ([]Runner, error) {
	token, err := r.credential()
	if err != nil {
		return nil, err
	}
	reqBody := map[string]any{
		"target": targetPayload(tgt),
		"prefix": prefix,
	}
	var resp struct {
		Runners []Runner `json:"runners"`
	}
	if err := r.post(ctx, "/v1/runners/list", token, reqBody, &resp); err != nil {
		return nil, err
	}
	return resp.Runners, nil
}

func targetPayload(tgt target.Target) map[string]any {
	switch tgt.Type {
	case target.TypeOrg:
		return map[string]any{
			"type":              "org",
			"org":               tgt.Org,
			"runner_group_id":   tgt.RunnerGroupID,
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

func (r *BrokerRegistrar) post(ctx context.Context, path, token string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return r.doWithRetry(ctx, http.MethodPost, path, token, payload, out)
}

func (r *BrokerRegistrar) delete(ctx context.Context, path, token string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return r.doWithRetry(ctx, http.MethodDelete, path, token, payload, nil)
}

func (r *BrokerRegistrar) doWithRetry(ctx context.Context, method, path, token string, body []byte, out any) error {
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

func (r *BrokerRegistrar) do(ctx context.Context, method, path, token string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, r.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
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
