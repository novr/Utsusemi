package config

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"unicode"
)

const systemProxyEnvPrefix = "UTSUSEMI_SYSTEM_PROXY_"

const (
	envSystemProxyHTTPHost  = systemProxyEnvPrefix + "HTTP_HOST"
	envSystemProxyHTTPPort  = systemProxyEnvPrefix + "HTTP_PORT"
	envSystemProxyHTTPSHost = systemProxyEnvPrefix + "HTTPS_HOST"
	envSystemProxyHTTPSPort = systemProxyEnvPrefix + "HTTPS_PORT"
	envSystemProxyBypass    = systemProxyEnvPrefix + "BYPASS"
)

// SystemProxy is the guest macOS system HTTP(S) proxy applied with networksetup.
// It is not copied into http_proxy / https_proxy; those stay in config.env.
type SystemProxy struct {
	HTTP   string   `yaml:"http,omitempty"`
	HTTPS  string   `yaml:"https,omitempty"`
	Bypass []string `yaml:"bypass,omitempty"`
}

func validateSystemProxy(sp *SystemProxy) error {
	if sp == nil {
		return nil
	}
	if strings.TrimSpace(sp.HTTP) == "" && strings.TrimSpace(sp.HTTPS) == "" {
		return fmt.Errorf("system_proxy requires http or https")
	}
	if _, _, err := splitProxyEndpoint("http", sp.HTTP); err != nil {
		return err
	}
	if _, _, err := splitProxyEndpoint("https", sp.HTTPS); err != nil {
		return err
	}
	for _, entry := range sp.Bypass {
		if err := validateBypassEntry(entry); err != nil {
			return err
		}
	}
	return nil
}

func splitProxyEndpoint(kind, raw string) (host, port string, err error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", nil
	}
	if strings.Contains(raw, "://") || strings.Contains(raw, "@") {
		return "", "", fmt.Errorf("system_proxy %s %q must be host:port", kind, raw)
	}
	host, port, err = net.SplitHostPort(raw)
	if err != nil || host == "" {
		return "", "", fmt.Errorf("system_proxy %s %q must be host:port", kind, raw)
	}
	n, convErr := strconv.Atoi(port)
	if convErr != nil || n < 1 || n > 65535 {
		return "", "", fmt.Errorf("system_proxy %s port %q must be 1-65535", kind, port)
	}
	if err := validateProxyHost(kind, host); err != nil {
		return "", "", err
	}
	return host, strconv.Itoa(n), nil
}

func validateProxyHost(kind, host string) error {
	if strings.HasPrefix(host, "-") {
		return fmt.Errorf("system_proxy %s host %q must not start with '-'", kind, host)
	}
	if strings.ContainsAny(host, ",/") {
		return fmt.Errorf("system_proxy %s host %q must be host:port", kind, host)
	}
	for _, r := range host {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("system_proxy %s host %q must not contain whitespace or control characters", kind, host)
		}
	}
	return nil
}

func validateBypassEntry(entry string) error {
	if entry == "" || strings.TrimSpace(entry) != entry {
		return fmt.Errorf("system_proxy bypass entry must not be empty or padded")
	}
	for _, r := range entry {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return fmt.Errorf("system_proxy bypass entry %q must not contain whitespace or control characters", entry)
		}
	}
	if strings.ContainsAny(entry, ",/") {
		return fmt.Errorf("system_proxy bypass entry %q must not contain ',' or '/'", entry)
	}
	if strings.HasPrefix(entry, "-") {
		return fmt.Errorf("system_proxy bypass entry %q must not start with '-'", entry)
	}
	// networksetup treats the single argument Empty as "clear every exclusion".
	if entry == "Empty" {
		return fmt.Errorf("system_proxy bypass entry %q clears all exclusions", entry)
	}
	if entry == "<local>" {
		return nil
	}
	if strings.ContainsAny(entry, "<>") {
		return fmt.Errorf("system_proxy bypass entry %q must not contain '<' or '>'", entry)
	}
	return nil
}

// SystemProxyEnv returns bootstrap-only guest variables. Bypass is the operator
// list with no proxy host appended. An empty or omitted bypass sets no variable.
func SystemProxyEnv(sp *SystemProxy) map[string]string {
	if sp == nil {
		return nil
	}
	out := map[string]string{}
	if host, port, err := splitProxyEndpoint("http", sp.HTTP); err == nil && host != "" {
		out[envSystemProxyHTTPHost] = host
		out[envSystemProxyHTTPPort] = port
	}
	if host, port, err := splitProxyEndpoint("https", sp.HTTPS); err == nil && host != "" {
		out[envSystemProxyHTTPSHost] = host
		out[envSystemProxyHTTPSPort] = port
	}
	if len(sp.Bypass) > 0 {
		out[envSystemProxyBypass] = strings.Join(sp.Bypass, ",")
	}
	return out
}
