package hostdns

import (
	"bytes"
	"context"
	"fmt"
	"net/netip"
	"os/exec"
	"strings"
)

type dscResolver struct {
	output OutputFunc
}

// NewDSCResolver builds a dscacheutil-backed resolver (tests inject Output).
func NewDSCResolver(output OutputFunc) Resolver {
	if output == nil {
		output = realDSCOutput
	}
	return &dscResolver{output: output}
}

func realDSCOutput(ctx context.Context, name string, args []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return out, fmt.Errorf("%w: %s", err, msg)
		}
		return out, err
	}
	return out, nil
}

func (r *dscResolver) LookupHost(ctx context.Context, host string) ([]netip.Addr, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, fmt.Errorf("empty host")
	}
	if _, err := netip.ParseAddr(host); err == nil {
		return nil, fmt.Errorf("host %q is an IP literal", host)
	}
	lookupCtx, cancel := context.WithTimeout(ctx, DefaultTimeout)
	defer cancel()
	out, err := r.output(lookupCtx, "dscacheutil", []string{"-q", "host", "-a", "name", host})
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	addrs := parseDscacheutil(out)
	if len(addrs) == 0 {
		return nil, fmt.Errorf("resolve %q: not found", host)
	}
	usable, err := filterUsable(addrs)
	if err != nil {
		return nil, fmt.Errorf("resolve %q: %w", host, err)
	}
	return usable, nil
}
