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

	"github.com/novr/utsusemi/internal/target"
)

func (r *GitHubPATRegistrar) createJITWithBase(ctx context.Context, base string, tgt target.Target, labels []string, name string) (JITConfig, error) {
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
			"ephemeral":       true,
		}
	case target.TypeRepo:
		path = fmt.Sprintf("/repos/%s/%s/actions/runners/generate-jitconfig", tgt.Owner, tgt.Repo)
		body = map[string]any{
			"name":      name,
			"labels":    labels,
			"ephemeral": true,
		}
	default:
		return JITConfig{}, fmt.Errorf("unsupported target type")
	}

	payload, err := json.Marshal(body)
	if err != nil {
		return JITConfig{}, err
	}
	var resp jitResponse
	if err := r.doWithBase(ctx, base, http.MethodPost, path, token, payload, &resp); err != nil {
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

func (r *GitHubPATRegistrar) doWithBase(ctx context.Context, base, method, path, token string, body []byte, out any) error {
	backoff := 10 * time.Millisecond
	for attempt := 0; attempt < 5; attempt++ {
		err := r.doRequest(ctx, base+path, method, token, body, out)
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

func (r *GitHubPATRegistrar) doRequest(ctx context.Context, url, method, token string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
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
