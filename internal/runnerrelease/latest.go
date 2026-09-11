package runnerrelease

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const defaultLatestURL = "https://api.github.com/repos/actions/runner/releases/latest"

type Client struct {
	HTTPClient *http.Client
	URL        string
}

func (c *Client) Latest(ctx context.Context) (string, error) {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	url := c.URL
	if url == "" {
		url = defaultLatestURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "utsusemi")

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("fetch latest runner release: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return "", fmt.Errorf("fetch latest runner release: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var payload struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("parse latest runner release: %w", err)
	}
	version := strings.TrimPrefix(strings.TrimSpace(payload.TagName), "v")
	if version == "" {
		return "", fmt.Errorf("latest runner release has no tag_name")
	}
	return version, nil
}

func Latest(ctx context.Context, client *http.Client) (string, error) {
	return (&Client{HTTPClient: client}).Latest(ctx)
}

// Older returns true when a is strictly older than b (semver-like x.y.z).
func Older(a, b string) bool {
	ap := parseParts(a)
	bp := parseParts(b)
	for i := 0; i < 3; i++ {
		if ap[i] < bp[i] {
			return true
		}
		if ap[i] > bp[i] {
			return false
		}
	}
	return false
}

func parseParts(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	parts := strings.Split(v, ".")
	out := [3]int{}
	for i := 0; i < len(parts) && i < 3; i++ {
		var n int
		fmt.Sscanf(parts[i], "%d", &n)
		out[i] = n
	}
	return out
}
