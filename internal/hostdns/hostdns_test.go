package hostdns

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"
)

func TestParseDscacheutil(t *testing.T) {
	out := []byte(`name: proxy.example
ip_address: 10.0.0.5

name: proxy.example
ipv6_address: 2001:db8::1
`)
	addrs := parseDscacheutil(out)
	if len(addrs) != 2 {
		t.Fatalf("addrs=%v", addrs)
	}
	if !addrs[0].Is4() || addrs[0].String() != "10.0.0.5" {
		t.Fatalf("v4=%v", addrs[0])
	}
	if !addrs[1].Is6() || addrs[1].String() != "2001:db8::1" {
		t.Fatalf("v6=%v", addrs[1])
	}
	if len(parseDscacheutil(nil)) != 0 {
		t.Fatal("empty output must yield no addrs")
	}
}

func TestPreferForHosts(t *testing.T) {
	v4 := netip.MustParseAddr("10.0.0.5")
	v6 := netip.MustParseAddr("2001:db8::1")
	got := PreferForHosts([]netip.Addr{v6, v4, netip.MustParseAddr("10.0.0.6")})
	if len(got) != 1 || got[0].String() != "10.0.0.5" {
		t.Fatalf("prefer first v4: %v", got)
	}
	got = PreferForHosts([]netip.Addr{v6, netip.MustParseAddr("2001:db8::2")})
	if len(got) != 1 || !got[0].Is6() || got[0].String() != "2001:db8::1" {
		t.Fatalf("v6 only first: %v", got)
	}
	if PreferForHosts(nil) != nil {
		t.Fatal("empty")
	}
}

func TestFilterUsable(t *testing.T) {
	_, err := filterUsable([]netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("::1"),
		netip.MustParseAddr("0.0.0.0"),
		netip.MustParseAddr("169.254.1.1"),
		netip.MustParseAddr("fe80::1"),
		netip.MustParseAddr("224.0.0.1"),
	})
	if err == nil {
		t.Fatal("expected no usable")
	}
	got, err := filterUsable([]netip.Addr{
		netip.MustParseAddr("127.0.0.1"),
		netip.MustParseAddr("10.0.0.5"),
		netip.MustParseAddr("10.0.0.5"),
		netip.MustParseAddr("::ffff:10.0.0.5"),
		netip.MustParseAddr("169.254.1.1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].String() != "10.0.0.5" {
		t.Fatalf("got=%v", got)
	}
}

func TestDSCResolverLookup(t *testing.T) {
	r := NewDSCResolver(func(ctx context.Context, name string, args []string) ([]byte, error) {
		if name != "dscacheutil" {
			t.Fatalf("name=%s", name)
		}
		want := []string{"-q", "host", "-a", "name", "proxy.example"}
		if strings.Join(args, " ") != strings.Join(want, " ") {
			t.Fatalf("args=%v", args)
		}
		return []byte("name: proxy.example\nip_address: 10.0.0.5\nipv6_address: 2001:db8::1\n"), nil
	})
	addrs, err := r.LookupHost(context.Background(), "proxy.example")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 2 {
		t.Fatalf("addrs=%v", addrs)
	}

	r = NewDSCResolver(func(context.Context, string, []string) ([]byte, error) {
		return nil, nil
	})
	if _, err := r.LookupHost(context.Background(), "missing.example"); err == nil {
		t.Fatal("expected not found")
	}

	r = NewDSCResolver(func(context.Context, string, []string) ([]byte, error) {
		return []byte("name: localhost\nip_address: 127.0.0.1\n"), nil
	})
	if _, err := r.LookupHost(context.Background(), "localhost"); err == nil {
		t.Fatal("expected loopback rejected")
	}

	if _, err := r.LookupHost(context.Background(), "10.0.0.5"); err == nil || !strings.Contains(err.Error(), "IP literal") {
		t.Fatalf("literal: %v", err)
	}

	r = NewDSCResolver(func(context.Context, string, []string) ([]byte, error) {
		return nil, errors.New("exec failed")
	})
	if _, err := r.LookupHost(context.Background(), "proxy.example"); err == nil {
		t.Fatal("expected exec error")
	}
}
