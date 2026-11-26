package config

import (
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// ProxyForward is an opt-in host-side TCP relay from a Tart NAT listen address
// to one fixed upstream proxy. It does not rewrite env or system_proxy.
type ProxyForward struct {
	Listen   string `yaml:"listen"`
	Upstream string `yaml:"upstream"`
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
	upHost, upPort, err := splitForwardEndpoint("proxy_forward.upstream", pf.Upstream, false)
	if err != nil {
		return err
	}
	if listenHost == upHost && listenPort == upPort {
		return fmt.Errorf("proxy_forward.listen must not equal proxy_forward.upstream")
	}
	if cfg.Softnet {
		return fmt.Errorf("proxy_forward cannot be used with softnet: true (guest cannot reach the host listen address)")
	}
	return nil
}

// splitForwardEndpoint parses host:port.
// listenStrict rejects hostnames and loopback/unspecified/link-local/multicast.
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
			return "", "", fmt.Errorf("%s %q must be an IP:port (hostname not allowed)", kind, raw)
		}
		if err := rejectListenAddr(kind, addr); err != nil {
			return "", "", err
		}
		return addr.String(), port, nil
	}

	if parseErr == nil {
		if err := rejectUpstreamAddr(kind, addr); err != nil {
			return "", "", err
		}
		return addr.String(), port, nil
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

func rejectListenAddr(kind string, addr netip.Addr) error {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid():
		return fmt.Errorf("%s address is invalid", kind)
	case addr.IsUnspecified():
		return fmt.Errorf("%s must not be 0.0.0.0 or ::", kind)
	case addr.IsLoopback():
		return fmt.Errorf("%s must not be loopback (guest cannot reach 127.0.0.1)", kind)
	case addr.IsLinkLocalUnicast(), addr.IsMulticast():
		return fmt.Errorf("%s address %s is not allowed", kind, addr)
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
