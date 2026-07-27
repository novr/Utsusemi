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
	return nil, fmt.Errorf("broker registrar does not support list runners")
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return r.do(req, out)
}

func (r *BrokerRegistrar) delete(ctx context.Context, path, token string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	return r.do(req, nil)
}

func (r *BrokerRegistrar) do(req *http.Request, out any) error {
	resp, err := r.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("broker api %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}
