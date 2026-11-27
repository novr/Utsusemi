package config

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

const reservedActionCacheAddr = "127.0.0.1:19527"

// ProxyForward tunnels guest loopback TCP connections over `tart exec -i` to
// one fixed upstream proxy dialed from the host.
type ProxyForward struct {
	Listen   string `yaml:"listen"`
	Upstream string `yaml:"upstream"`
}

// ApplyProxyForwardDefaults fills only unset proxy settings on the effective
// runtime config. It does not override values supplied by the operator.
func ApplyProxyForwardDefaults(cfg *Config) {
	if cfg == nil || cfg.ProxyForward == nil {
		return
	}

	proxyURL := "http://" + cfg.ProxyForward.Listen
	if cfg.Env == nil {
		cfg.Env = make(map[string]string)
	}
	if !hasEnvKey(cfg.Env, "http_proxy", "HTTP_PROXY") {
		cfg.Env["http_proxy"] = proxyURL
	}
	if !hasEnvKey(cfg.Env, "https_proxy", "HTTPS_PROXY") {
		cfg.Env["https_proxy"] = proxyURL
	}
	if !hasEnvKey(cfg.Env, "no_proxy", "NO_PROXY") {
		cfg.Env["no_proxy"] = "localhost,127.0.0.1"
	}

	// system_proxy remains opt-in because it changes the guest-wide macOS
	// network service. When present, only its missing fields are filled.
	if cfg.SystemProxy == nil {
		return
	}
	if strings.TrimSpace(cfg.SystemProxy.HTTP) == "" {
		cfg.SystemProxy.HTTP = cfg.ProxyForward.Listen
	}
	if strings.TrimSpace(cfg.SystemProxy.HTTPS) == "" {
		cfg.SystemProxy.HTTPS = cfg.ProxyForward.Listen
	}
	if cfg.SystemProxy.Bypass == nil {
		cfg.SystemProxy.Bypass = []string{"localhost", "127.0.0.1"}
	}
}

func hasEnvKey(env map[string]string, keys ...string) bool {
	for _, key := range keys {
		if _, ok := env[key]; ok {
			return true
		}
	}
	return false
}

func validateProxyForward(cfg *Config) error {
	if cfg == nil || cfg.ProxyForward == nil {
		return nil
	}
	pf := cfg.ProxyForward
	listenHost, listenPort, err := splitForwardEndpoint("proxy_forward.listen", pf.Listen, true)
	if err != nil {
		return err
	}
	if _, _, err := splitForwardEndpoint("proxy_forward.upstream", pf.Upstream, false); err != nil {
		return err
	}
	if cfg.ActionCache && net.JoinHostPort(listenHost, listenPort) == reservedActionCacheAddr {
		return fmt.Errorf("proxy_forward.listen %s conflicts with action_cache (%s)", pf.Listen, reservedActionCacheAddr)
	}
	return nil
}

// splitForwardEndpoint parses host:port.
// listenStrict requires a guest loopback IP:port (e.g. 127.0.0.1:8888).
// upstream allows hostnames and 127.0.0.1, but rejects localhost / 0.0.0.0 / ::.
func splitForwardEndpoint(kind, raw string, listenStrict bool) (host, port string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", fmt.Errorf("%s is required", kind)
	}
	if strings.Contains(raw, "://") || strings.Contains(raw, "@") {
		return "", "", fmt.Errorf("%s %q must be host:port", kind, raw)
	}
	host, port, err = net.SplitHostPort(raw)
	if err != nil || host == "" {
		return "", "", fmt.Errorf("%s %q must be host:port", kind, raw)
	}
	n, convErr := strconv.Atoi(port)
	if convErr != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("%s port %q must be 1-65535", kind, port)
	}
	port = strconv.Itoa(n)

	if strings.EqualFold(host, "localhost") {
		return "", "", fmt.Errorf("%s host %q must be 127.0.0.1 (not localhost)", kind, host)
	}

	addr, parseErr := netip.ParseAddr(host)
	if listenStrict {
		if parseErr != nil {
			return "", "", fmt.Errorf("%s %q must be a guest loopback IP:port (e.g. 127.0.0.1:8888)", kind, raw)
		}
		if err := requireLoopbackListen(kind, addr); err != nil {
			return "", "", err
		}
		return addr.Unmap().String(), port, nil
	}

	if parseErr == nil {
		if err := rejectUpstreamAddr(kind, addr); err != nil {
			return "", "", err
		}
		return addr.Unmap().String(), port, nil
	}
	if err := validateForwardHostname(kind, host); err != nil {
		return "", "", err
	}
	return host, port, nil
}

func validateForwardHostname(kind, host string) error {
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf("%s host %q must not start with '-'", kind, host)
	}
	if strings.ContainsAny(host, ",/") {
		return fmt.Errorf("%s %q must be host:port", kind, host)
	}
	for _, r := range host {
		if r <= ' ' || r == 0x7f {
			return fmt.Errorf("%s host %q must not contain whitespace or control characters", kind, host)
		}
	}
	return nil
}

func requireLoopbackListen(kind string, addr netip.Addr) error {
	addr = addr.Unmap()
	if !addr.IsValid() {
		return fmt.Errorf("%s address is invalid", kind)
	}
	if !addr.IsLoopback() {
		return fmt.Errorf("%s must be a guest loopback IP:port (e.g. 127.0.0.1:8888), got %s", kind, addr)
	}
	return nil
}

func rejectUpstreamAddr(kind string, addr netip.Addr) error {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid():
		return fmt.Errorf("%s address is invalid", kind)
	case addr.IsUnspecified():
		return fmt.Errorf("%s must not be 0.0.0.0 or ::", kind)
	case addr.IsLoopback() && !addr.Is4():
		return fmt.Errorf("%s loopback must be 127.0.0.1", kind)
	case addr.IsLinkLocalUnicast(), addr.IsMulticast():
		return fmt.Errorf("%s address %s is not allowed", kind, addr)
	}
	return nil
}
