package registrar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

type BrokerRegistrar struct {
	client    *http.Client
	api       *httpClient
	store     keychain.Store
	cfg       *config.Config
	baseURL   string
	refreshMu sync.Mutex
	oauth     *hostcredential.OAuthClient
	logger    *slog.Logger
}

func NewBrokerRegistrar(store keychain.Store, cfg *config.Config, logger *slog.Logger) *BrokerRegistrar {
	client := &http.Client{Timeout: defaultHTTPTimeout}
	baseURL := strings.TrimRight(cfg.Registration.BrokerURL, "/")
	return &BrokerRegistrar{
		client:  client,
		api:     newBrokerHTTPClient(client, baseURL),
		store:   store,
		cfg:     cfg,
		baseURL: baseURL,
		logger:  logger,
	}
}

func (r *BrokerRegistrar) ValidateCredential(ctx context.Context, service, account string) error {
	tgt, err := target.FromConfig(r.cfg.Target)
	if err != nil {
		return err
	}
	targetBody, err := hostcredential.TargetPayload(tgt)
	if err != nil {
		return err
	}
	return r.requestWithCredential(ctx, tgt, func(token string) error {
		reqBody := map[string]any{
			"target": targetBody,
			"prefix": "",
		}
		return r.post(ctx, "/v1/runners/list", token, reqBody, &struct {
			Runners []Runner `json:"runners"`
		}{})
	})
}

func (r *BrokerRegistrar) CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (JITConfig, error) {
	if err := target.RequireOrg(tgt); err != nil {
		return JITConfig{}, err
	}
	targetBody, err := hostcredential.TargetPayload(tgt)
	if err != nil {
		return JITConfig{}, err
	}
	reqBody := map[string]any{
		"target": targetBody,
		"labels": labels,
		"name":   name,
	}
	var resp jitResponse
	err = r.requestWithCredential(ctx, tgt, func(token string) error {
		return r.post(ctx, "/v1/jitconfig", token, reqBody, &resp)
	})
	if err != nil {
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
	if err := target.RequireOrg(tgt); err != nil {
		return err
	}
	targetBody, err := hostcredential.TargetPayload(tgt)
	if err != nil {
		return err
	}
	path := fmt.Sprintf("/v1/runners/%d", runnerID)
	reqBody := map[string]any{"target": targetBody}
	return r.requestWithCredential(ctx, tgt, func(token string) error {
		return r.delete(ctx, path, token, reqBody)
	})
}

func (r *BrokerRegistrar) ListRunners(ctx context.Context, tgt target.Target, prefix string) ([]Runner, error) {
	if err := target.RequireOrg(tgt); err != nil {
		return nil, err
	}
	targetBody, err := hostcredential.TargetPayload(tgt)
	if err != nil {
		return nil, err
	}
	reqBody := map[string]any{
		"target": targetBody,
		"prefix": prefix,
	}
	var resp struct {
		Runners []Runner `json:"runners"`
	}
	err = r.requestWithCredential(ctx, tgt, func(token string) error {
		return r.post(ctx, "/v1/runners/list", token, reqBody, &resp)
	})
	if err != nil {
		return nil, err
	}
	return resp.Runners, nil
}

func (r *BrokerRegistrar) post(ctx context.Context, path, token string, body any, out any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return r.api.doWithRetry(ctx, http.MethodPost, path, token, payload, out)
}

func (r *BrokerRegistrar) delete(ctx context.Context, path, token string, body any) error {
	payload, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return r.api.doWithRetry(ctx, http.MethodDelete, path, token, payload, nil)
}
