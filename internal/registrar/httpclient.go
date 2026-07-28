package registrar

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultHTTPTimeout = 30 * time.Second

type apiError struct {
	StatusCode int
	Message    string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("api %d: %s", e.StatusCode, e.Message)
}

type httpClient struct {
	client  *http.Client
	baseURL string
	prepare func(req *http.Request, body []byte)
}

func newGitHubHTTPClient(client *http.Client) *httpClient {
	return &httpClient{
		client:  client,
		baseURL: githubAPI,
		prepare: func(req *http.Request, body []byte) {
			req.Header.Set("Accept", "application/vnd.github+json")
			req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
		},
	}
}

func newBrokerHTTPClient(client *http.Client, baseURL string) *httpClient {
	return &httpClient{
		client:  client,
		baseURL: baseURL,
		prepare: func(req *http.Request, body []byte) {
			if body != nil {
				req.Header.Set("Content-Type", "application/json")
			}
		},
	}
}

func (c *httpClient) doWithRetry(ctx context.Context, method, path, token string, body []byte, out any) error {
	backoff := time.Second
	for attempt := 0; attempt < 5; attempt++ {
		err := c.do(ctx, method, path, token, body, out)
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

func (c *httpClient) do(ctx context.Context, method, path, token string, body []byte, out any) error {
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if c.prepare != nil {
		c.prepare(req, body)
	}

	resp, err := c.client.Do(req)
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

func isRetryable(err error) bool {
	apiErr, ok := asAPIError(err)
	if !ok {
		return false
	}
	return apiErr.StatusCode == http.StatusTooManyRequests || apiErr.StatusCode >= 500
}

func IsUnauthorized(err error) bool {
	apiErr, ok := asAPIError(err)
	return ok && apiErr.StatusCode == http.StatusUnauthorized
}

func asAPIError(err error) (*apiError, bool) {
	var apiErr *apiError
	if errors.As(err, &apiErr) {
		return apiErr, true
	}
	return nil, false
}
