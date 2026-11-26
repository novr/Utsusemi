//go:build !darwin

package hostdns

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

type netResolver struct {
	lookup func(ctx context.Context, host string) ([]netip.Addr, error)
}

func defaultResolver() Resolver {
	return &netResolver{lookup: lookupNet}
}

func lookupNet(ctx context.Context, host string) ([]netip.Addr, error) {
	lookupCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	ips, err := net.DefaultResolver.LookupNetIP(lookupCtx, "ip", host)
	if err != nil {
		return nil, err
	}
	return ips, nil
}

func (r *netResolver) LookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("empty host")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil, fmt.Errorf("host %q is an IP literal", host)
	}
	addrs, err := r.lookup(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	usable, err := filterUsable(addrs)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	return usable, nil
}
