package hostdns

import (
	"context"
	"fmt"
	"net/netip"
	"time"
)

const DefaultTimeout = 5 * time.Second

// Resolver looks up hostnames using the host's system DNS view.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]netip.Addr, error)
}

// OutputFunc runs a command and returns its stdout (darwin dscacheutil path).
type OutputFunc func(ctx context.Context, name string, args []string) ([]byte, error)

// Default returns the platform resolver (dscacheutil on darwin, net.Resolver elsewhere).
func Default() Resolver {
	return defaultResolver()
}

// PreferForHosts picks one address for guest /etc/hosts: first IPv4 if any,
// otherwise first IPv6. Tart guests are typically IPv4-NAT; multi-A injection
// is unnecessary for a single proxy hop and makes guest verification brittle.
func PreferForHosts(addrs []netip.Addr) []netip.Addr {
	var first4, first6 netip.Addr
	have4, have6 := false, false
	for _, a := range addrs {
		if a.Is4() {
			if !have4 {
				first4 = a
				have4 = true
			}
			continue
		}
		if !have6 {
			first6 = a
			have6 = true
		}
	}
	if have4 {
		return []netip.Addr{first4}
	}
	if have6 {
		return []netip.Addr{first6}
	}
	return nil
}

func filterUsable(addrs []netip.Addr) ([]netip.Addr, error) {
	out := make([]netip.Addr, 0, len(addrs))
	seen := map[netip.Addr]struct{}{}
	for _, a := range addrs {
		a = a.Unmap()
		if !a.IsValid() || a.IsUnspecified() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsMulticast() {
			continue
		}
		if _, ok := seen[a]; ok {
			continue
		}
		seen[a] = struct{}{}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no usable addresses")
	}
	return out, nil
}
