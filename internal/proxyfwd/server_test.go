package proxyfwd

import (
	"context"
	"io"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeResolver struct {
	mu    sync.Mutex
	addrs map[string][]netip.Addr
	err   error
	calls int32
}

func (f *fakeResolver) LookupHost(_ context.Context, host string) ([]netip.Addr, error) {
	atomic.AddInt32(&f.calls, 1)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	return append([]netip.Addr(nil), f.addrs[host]...), nil
}

func startEcho(t *testing.T) (addr string, closeFn func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(c)
		}
	}()
	return ln.Addr().String(), func() {
		_ = ln.Close()
		<-done
	}
}

func TestRelayBidirectional(t *testing.T) {
	upAddr, closeUp := startEcho(t)
	defer closeUp()

	listen := "127.0.0.1:0"
	// Server requires Listen as configured string; use ephemeral by binding via EnsureListening
	// with a concrete port. Bind temporary to learn a free port on loopback.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen = ln.Addr().String()
	_ = ln.Close()

	s := New(listen, upAddr, &fakeResolver{}, nil)
	s.BindRetry = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.EnsureListening(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
	if string(buf) != "ping" {
		t.Fatalf("got %q", buf)
	}
}

func TestEnsureListeningIdempotent(t *testing.T) {
	upAddr, closeUp := startEcho(t)
	defer closeUp()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	_ = ln.Close()

	s := New(listen, upAddr, &fakeResolver{}, nil)
	s.BindRetry = 10 * time.Millisecond
	ctx := context.Background()
	if err := s.EnsureListening(ctx); err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.EnsureListening(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestDelayedBind(t *testing.T) {
	upAddr, closeUp := startEcho(t)
	defer closeUp()

	// Pick a free port, but delay bringing 127.0.0.1 listener readiness is always
	// immediate. Instead verify BindRetry path by canceling mid-wait on a missing IP.
	s := New("203.0.113.1:19999", upAddr, &fakeResolver{}, nil) // TEST-NET-3, unlikely present
	s.BindRetry = 20 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	err := s.EnsureListening(ctx)
	if err == nil {
		s.Close()
		t.Fatal("expected timeout waiting for missing listen IP")
	}
}

func TestMaxConns(t *testing.T) {
	upLn, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upLn.Close()
	block := make(chan struct{})
	go func() {
		for {
			c, err := upLn.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				<-block
			}(c)
		}
	}()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	_ = ln.Close()

	s := New(listen, upLn.Addr().String(), &fakeResolver{}, nil)
	s.MaxConns = 1
	s.BindRetry = 10 * time.Millisecond
	if err := s.EnsureListening(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	c1, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c1.Close()
	time.Sleep(50 * time.Millisecond)

	c2, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	// second connection should be accepted then closed quickly by max-conns
	c2.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 1)
	_, err = c2.Read(buf)
	if err == nil {
		t.Fatal("expected second connection to be closed")
	}
	_ = c2.Close()
	close(block)
}

func TestResolveCacheAndInvalidate(t *testing.T) {
	upAddr, closeUp := startEcho(t)
	defer closeUp()
	host, port, _ := net.SplitHostPort(upAddr)
	ip := netip.MustParseAddr(host)

	res := &fakeResolver{addrs: map[string][]netip.Addr{"proxy.test": {ip}}}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	_ = ln.Close()

	s := New(listen, net.JoinHostPort("proxy.test", port), res, nil)
	s.ResolveTTL = time.Hour
	s.BindRetry = 10 * time.Millisecond
	if err := s.EnsureListening(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	for i := 0; i < 3; i++ {
		c, err := net.DialTimeout("tcp", listen, time.Second)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = c.Write([]byte("x"))
		buf := make([]byte, 1)
		_, _ = io.ReadFull(c, buf)
		_ = c.Close()
	}
	if got := atomic.LoadInt32(&res.calls); got != 1 {
		t.Fatalf("resolver calls=%d want 1 (cached)", got)
	}
}

func TestCloseReleases(t *testing.T) {
	upAddr, closeUp := startEcho(t)
	defer closeUp()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	_ = ln.Close()
	s := New(listen, upAddr, &fakeResolver{}, nil)
	if err := s.EnsureListening(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureListening(context.Background()); err == nil {
		t.Fatal("expected error after close")
	}
}

func TestEnsureListeningConcurrentAndRebind(t *testing.T) {
	upAddr, closeUp := startEcho(t)
	defer closeUp()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	listen := ln.Addr().String()
	_ = ln.Close()

	s := New(listen, upAddr, &fakeResolver{}, nil)
	s.BindRetry = 5 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errc := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errc <- s.EnsureListening(ctx)
		}()
	}
	wg.Wait()
	close(errc)
	for err := range errc {
		if err != nil {
			t.Fatal(err)
		}
	}
	defer s.Close()

	first := s.ln
	if first == nil {
		t.Fatal("listener missing")
	}
	if err := s.EnsureListening(ctx); err != nil {
		t.Fatal(err)
	}
	if s.ln != first {
		t.Fatal("same interface should keep the listener")
	}
	s.ifaceIndex = -1
	if err := s.EnsureListening(ctx); err != nil {
		t.Fatal(err)
	}
	if s.ln == first || s.ln == nil {
		t.Fatal("interface index change should rebind")
	}
	c, err := net.DialTimeout("tcp", listen, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	if _, err := io.ReadFull(c, buf); err != nil {
		t.Fatal(err)
	}
}

func TestRejectNonBridgeListen(t *testing.T) {
	ip := nonBridgeIPv4(t)
	if ip == "" {
		t.Skip("no non-bridge IPv4")
	}
	upAddr, closeUp := startEcho(t)
	defer closeUp()
	s := New(net.JoinHostPort(ip, "19998"), upAddr, &fakeResolver{}, nil)
	s.BindRetry = 10 * time.Millisecond
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	err := s.EnsureListening(ctx)
	if err == nil || !strings.Contains(err.Error(), "bridge") {
		t.Fatalf("err=%v", err)
	}
}

func nonBridgeIPv4(t *testing.T) string {
	t.Helper()
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatal(err)
	}
	for _, iface := range ifaces {
		if strings.HasPrefix(iface.Name, "bridge") || strings.HasPrefix(iface.Name, "lo") {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP == nil || ipnet.IP.To4() == nil || ipnet.IP.IsLoopback() {
				continue
			}
			return ipnet.IP.String()
		}
	}
	return ""
}

func TestRejectOutsideSubnet(t *testing.T) {
	// With listen on 127.0.0.1, allowed subnet is 127.0.0.0/8, so local dials are allowed.
	// Verify allowSrc rejects an address outside the captured IPNet.
	s := &Server{allowed: &net.IPNet{IP: net.ParseIP("192.168.64.1"), Mask: net.CIDRMask(24, 32)}}
	if s.allowSrc(&net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 1}) {
		t.Fatal("expected reject")
	}
	if !s.allowSrc(&net.TCPAddr{IP: net.ParseIP("192.168.64.5"), Port: 1}) {
		t.Fatal("expected allow")
	}
}
