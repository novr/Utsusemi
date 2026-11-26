package spawn

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"strings"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
)

//go:embed hosts_setup.sh
var hostsSetupScript string

func formatHostsMap(entries []config.HostsMapEntry) ([]byte, error) {
	if len(entries) == 0 {
		return nil, fmt.Errorf("no hosts entries")
	}
	var b strings.Builder
	for _, e := range entries {
		if !e.IP.IsValid() || e.Host == "" {
			return nil, fmt.Errorf("invalid hosts entry")
		}
		if strings.ContainsAny(e.Host, " \t\r\n") {
			return nil, fmt.Errorf("invalid hosts hostname")
		}
		fmt.Fprintf(&b, "%s %s\n", e.IP.String(), e.Host)
	}
	return []byte(b.String()), nil
}

func applyProxyHosts(ctx context.Context, log *slog.Logger, p provider.VMProvider, vmName string, entries []config.HostsMapEntry) error {
	if len(entries) == 0 {
		return nil
	}
	payload, err := formatHostsMap(entries)
	if err != nil {
		return err
	}
	for _, e := range entries {
		log.Info("injecting proxy host into guest /etc/hosts", "host", e.Host, "ip", e.IP.String())
	}
	if err := p.ExecStdin(ctx, vmName, "bash", []string{"-c", hostsSetupScript}, payload, nil); err != nil {
		return fmt.Errorf("guest /etc/hosts setup failed (guest user needs passwordless sudo): %w", err)
	}
	return nil
}
