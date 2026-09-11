package brokerhttp

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

func IsLoopbackBrokerURL(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	return host == "127.0.0.1" || strings.EqualFold(host, "localhost")
}

func CheckReachable(ctx context.Context, brokerURL string) error {
	if !IsLoopbackBrokerURL(brokerURL) {
		return nil
	}
	base := strings.TrimRight(brokerURL, "/") + "/"
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base, nil)
	if err != nil {
		return err
	}
	resp, err := (&http.Client{Timeout: 2 * time.Second}).Do(req)
	if err != nil {
		return fmt.Errorf("local broker not reachable at %s (start `utsusemi broker` first): %w", brokerURL, err)
	}
	defer resp.Body.Close()
	return nil
}
