package hostdns

import (
	"bufio"
	"net/netip"
	"strings"
)

// parseDscacheutil extracts ip_address / ipv6_address values from dscacheutil -q host output.
// Unresolved names often exit 0 with empty stdout; callers must treat empty addrs as failure.
func parseDscacheutil(out []byte) []netip.Addr {
	var addrs []netip.Addr
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		switch key {
		case "ip_address", "ipv6_address":
			if a, err := netip.ParseAddr(val); err == nil {
				addrs = append(addrs, a)
			}
		}
	}
	return addrs
}
