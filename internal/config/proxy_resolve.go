package config

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"github.com/novr/utsusemi/internal/hostdns"
)

var proxyEnvKeys = []string{
	"http_proxy", "HTTP_PROXY", "https_proxy", "HTTPS_PROXY",
}

// hostnameSafe matches names safe for a single /etc/hosts field (no spaces/newlines).
var hostnameSafe = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$|^[A-Za-z0-9]$`)

// HostsMapEntry is one guest /etc/hosts line: IP plus hostname.
type HostsMapEntry struct {
	IP   netip.Addr
	Host string
}

// ProxyHosts returns unique non-literal hostnames from config.env proxy vars and
// system_proxy endpoints. cfg is not modified. Order is sorted for stability.
func ProxyHosts(cfg *Config) ([]string, error) {
	if cfg == nil {
		return nil, nil
	}
	seen := map[string]struct{}{}
	var hosts []string
	add := func(host string) error {
		host = strings.TrimSpace(host)
		host = strings.TrimSuffix(host, ".")
		if host == "" {
			return nil
		}
		if _, err := netip.ParseAddr(host); err == nil {
			return nil
		}
		if err := validateProxyHostname(host); err != nil {
			return err
		}
		key := strings.ToLower(host)
		if _, ok := seen[key]; ok {
			return nil
		}
		seen[key] = struct{}{}
		hosts = append(hosts, host)
		return nil
	}

	for _, key := range proxyEnvKeys {
		raw, ok := cfg.Env[key]
		if !ok || strings.TrimSpace(raw) == "" {
			continue
		}
		host, err := proxyURLHostname(raw)
		if err != nil {
			return nil, fmt.Errorf("env.%s: %w", key, err)
		}
		if err := add(host); err != nil {
			return nil, fmt.Errorf("env.%s: %w", key, err)
		}
	}
	if cfg.SystemProxy != nil {
		for _, kind := range []struct {
			name string
			raw  string
		}{
			{"http", cfg.SystemProxy.HTTP},
			{"https", cfg.SystemProxy.HTTPS},
		} {
			host, _, err := splitProxyEndpoint(kind.name, kind.raw)
			if err != nil {
				return nil, err
			}
			if err := add(host); err != nil {
				return nil, fmt.Errorf("system_proxy.%s: %w", kind.name, err)
			}
		}
	}
	sort.Strings(hosts)
	return hosts, nil
}

// ResolveProxyHosts maps ProxyHosts through resolver and returns IPv4-preferring
// hosts entries for the guest. Does not mutate cfg.
func ResolveProxyHosts(ctx context.Context, cfg *Config, resolver hostdns.Resolver) ([]HostsMapEntry, error) {
	if cfg == nil || !cfg.ResolveProxyOnHost {
		return nil, nil
	}
	if resolver == nil {
		resolver = hostdns.Default()
	}
	hosts, err := ProxyHosts(cfg)
	if err != nil {
		return nil, err
	}
	var entries []HostsMapEntry
	for _, host := range hosts {
		addrs, err := resolver.LookupHost(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("resolve proxy host %q: %w", host, err)
		}
		for _, addr := range hostdns.PreferForHosts(addrs) {
			entries = append(entries, HostsMapEntry{IP: addr, Host: host})
		}
	}
	return entries, nil
}

func proxyURLHostname(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("empty proxy URL")
	}
	// Match actions/runner PrependHttpIfMissing: bare host:port is http://host:port.
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid proxy URL")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", fmt.Errorf("proxy URL scheme must be http or https")
	}
	host := strings.TrimSuffix(u.Hostname(), ".")
	if host == "" {
		return "", fmt.Errorf("proxy URL missing host")
	}
	return host, nil
}

func validateProxyHostname(host string) error {
	if host == "" {
		return fmt.Errorf("empty hostname")
	}
	if strings.ContainsAny(host, " \t\r\n") {
		return fmt.Errorf("hostname %q must not contain whitespace", host)
	}
	// IP literals are skipped by ProxyHosts before this; keep the guard for callers.
	if net.ParseIP(host) != nil {
		return nil
	}
	if !hostnameSafe.MatchString(host) {
		return fmt.Errorf("hostname %q is not a valid host name", host)
	}
	return nil
}

func validateResolveProxyOnHost(cfg *Config) error {
	if cfg == nil || !cfg.ResolveProxyOnHost {
		return nil
	}
	_, err := ProxyHosts(cfg)
	return err
}
