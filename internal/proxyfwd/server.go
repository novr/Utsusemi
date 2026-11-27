package proxyfwd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/novr/utsusemi/internal/hostdns"
)

const (
	DefaultMaxConns     = 512
	DefaultDialTimeout  = 10 * time.Second
	DefaultKeepAlive    = 30 * time.Second
	DefaultDrainTimeout = 30 * time.Second
	DefaultBindRetry    = time.Second
	DefaultResolveTTL   = 30 * time.Second
)

// Server relays TCP byte streams from listenAddr to a fixed upstream.
type Server struct {
	Listen   string
	Upstream string
	Resolver hostdns.Resolver
	Logger   *slog.Logger

	MaxConns     int
	DialTimeout  time.Duration
	KeepAlive    time.Duration
	DrainTimeout time.Duration
	BindRetry    time.Duration
	ResolveTTL   time.Duration

	bindMu       sync.Mutex
	mu           sync.Mutex
	ln           net.Listener
	listenIP     netip.Addr
	ifaceIndex   int
	ifaceName    string
	allowed      *net.IPNet
	closed       bool
	active       int32
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	lastLimitLog atomic.Int64

	resolveMu sync.Mutex
	cachedIP  netip.Addr
	cachedAt  time.Time
	inflight  *resolveCall
}

type resolveCall struct {
	done chan struct{}
	ip   netip.Addr
	err  error
}

// New builds a Server. Call EnsureListening after the Tart NAT bridge exists.
func New(listen, upstream string, resolver hostdns.Resolver, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.Default()
	}
	if resolver == nil {
		resolver = hostdns.Default()
	}
	return &Server{
		Listen:       listen,
		Upstream:     upstream,
		Resolver:     resolver,
		Logger:       logger,
		MaxConns:     DefaultMaxConns,
		DialTimeout:  DefaultDialTimeout,
		KeepAlive:    DefaultKeepAlive,
		DrainTimeout: DefaultDrainTimeout,
		BindRetry:    DefaultBindRetry,
		ResolveTTL:   DefaultResolveTTL,
	}
}

// EnsureListening waits until Listen can bind, then accepts connections.
// Concurrent callers share one bind. A live listener is kept when the same
// interface still owns the listen IP; a new interface index (vmnet bridge
// recreated) rebinds. Non-loopback listen IPs must be on a bridge* interface.
func (s *Server) EnsureListening(ctx context.Context) error {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return fmt.Errorf("proxyfwd: closed")
	}
	if s.ln != nil && listenStillCurrent(s.listenIP, s.ifaceIndex, s.ifaceName) {
		s.mu.Unlock()
		return nil
	}
	old := s.ln
	oldCancel := s.cancel
	s.ln = nil
	s.cancel = nil
	s.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}
	if old != nil {
		_ = old.Close()
	}

	host, port, err := net.SplitHostPort(s.Listen)
	if err != nil {
		return fmt.Errorf("proxyfwd listen: %w", err)
	}
	listenIP, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("proxyfwd listen: %w", err)
	}

	retry := s.BindRetry
	if retry <= 0 {
		retry = DefaultBindRetry
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		bind, ok := ifaceForIP(listenIP)
		if !ok {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retry):
				continue
			}
		}
		if !listenIP.IsLoopback() && !strings.HasPrefix(bind.name, "bridge") {
			return fmt.Errorf("proxyfwd listen %s is on %q; want a Tart vmnet bridge interface", s.Listen, bind.name)
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(listenIP.String(), port))
		if err != nil {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(retry):
				continue
			}
		}
		s.mu.Lock()
		if s.closed {
			s.mu.Unlock()
			_ = ln.Close()
			return fmt.Errorf("proxyfwd: closed")
		}
		serveCtx, cancel := context.WithCancel(context.Background())
		s.ln = ln
		s.listenIP = listenIP
		s.ifaceIndex = bind.index
		s.ifaceName = bind.name
		s.allowed = bind.net
		s.cancel = cancel
		s.mu.Unlock()
		s.Logger.Info("proxy forward listening", "listen", s.Listen, "upstream", s.Upstream, "iface", bind.name)
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			s.acceptLoop(serveCtx, ln)
		}()
		return nil
	}
}

// Close stops accepting and waits for in-flight relays to finish draining.
func (s *Server) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	ln := s.ln
	s.ln = nil
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	var err error
	if ln != nil {
		err = ln.Close()
	}
	s.wg.Wait()
	return err
}

func (s *Server) acceptLoop(ctx context.Context, ln net.Listener) {
	defer func() {
		s.mu.Lock()
		if s.ln == ln {
			s.ln = nil
		}
		s.mu.Unlock()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				time.Sleep(50 * time.Millisecond)
				continue
			}
			s.Logger.Warn("proxy forward accept stopped", "listen", s.Listen, "error", err)
			return
		}
		if !s.allowSrc(conn.RemoteAddr()) {
			s.Logger.Warn("proxy forward rejected source", "remote", conn.RemoteAddr().String(), "listen", s.Listen)
			_ = conn.Close()
			continue
		}
		if int(atomic.LoadInt32(&s.active)) >= s.maxConns() {
			if s.allowLimitLog() {
				s.Logger.Warn("proxy forward max connections", "max", s.maxConns(), "listen", s.Listen)
			}
			_ = conn.Close()
			continue
		}
		atomic.AddInt32(&s.active, 1)
		s.wg.Add(1)
		go func(c net.Conn) {
			defer s.wg.Done()
			defer atomic.AddInt32(&s.active, -1)
			s.relay(ctx, c)
		}(conn)
	}
}

func (s *Server) maxConns() int {
	if s.MaxConns <= 0 {
		return DefaultMaxConns
	}
	return s.MaxConns
}

func (s *Server) allowSrc(addr net.Addr) bool {
	s.mu.Lock()
	allowed := s.allowed
	s.mu.Unlock()
	if allowed == nil {
		return false
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return allowed.Contains(ip)
}

func (s *Server) relay(ctx context.Context, client net.Conn) {
	defer client.Close()
	setKeepAlive(client, s.KeepAlive)

	upConn, err := s.dialUpstream(ctx)
	if err != nil {
		s.Logger.Warn("proxy forward dial upstream failed", "upstream", s.Upstream, "error", err)
		return
	}
	defer upConn.Close()
	setKeepAlive(upConn, s.KeepAlive)

	errc := make(chan error, 2)
	go pipe(client, upConn, errc)
	go pipe(upConn, client, errc)

	drain := s.DrainTimeout
	if drain <= 0 {
		drain = DefaultDrainTimeout
	}
	var first error
	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			return
		case err := <-errc:
			if first == nil {
				first = err
				_ = closeWrite(client)
				_ = closeWrite(upConn)
				timer := time.NewTimer(drain)
				select {
				case <-errc:
					timer.Stop()
					return
				case <-timer.C:
					return
				case <-ctx.Done():
					timer.Stop()
					return
				}
			}
		}
	}
}

func pipe(dst, src net.Conn, errc chan<- error) {
	_, err := io.Copy(dst, src)
	errc <- err
}

func closeWrite(c net.Conn) error {
	type closeWriter interface {
		CloseWrite() error
	}
	if cw, ok := c.(closeWriter); ok {
		return cw.CloseWrite()
	}
	return nil
}

func setKeepAlive(c net.Conn, d time.Duration) {
	if d <= 0 {
		d = DefaultKeepAlive
	}
	if tc, ok := c.(*net.TCPConn); ok {
		_ = tc.SetKeepAlive(true)
		_ = tc.SetKeepAlivePeriod(d)
	}
}

func (s *Server) dialUpstream(ctx context.Context) (net.Conn, error) {
	host, port, err := net.SplitHostPort(s.Upstream)
	if err != nil {
		return nil, err
	}
	timeout := s.DialTimeout
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if ip, err := netip.ParseAddr(host); err == nil {
		return (&net.Dialer{Timeout: timeout, KeepAlive: s.KeepAlive}).DialContext(dctx, "tcp", net.JoinHostPort(ip.String(), port))
	}

	ip, err := s.resolveUpstream(dctx, host)
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{Timeout: timeout, KeepAlive: s.KeepAlive}).DialContext(dctx, "tcp", net.JoinHostPort(ip.String(), port))
	if err != nil {
		s.invalidateResolve()
		return nil, err
	}
	return conn, nil
}

func (s *Server) resolveUpstream(ctx context.Context, host string) (netip.Addr, error) {
	ttl := s.ResolveTTL
	if ttl <= 0 {
		ttl = DefaultResolveTTL
	}
	now := time.Now()

	s.resolveMu.Lock()
	if s.cachedIP.IsValid() && now.Sub(s.cachedAt) < ttl {
		ip := s.cachedIP
		s.resolveMu.Unlock()
		return ip, nil
	}
	if s.inflight != nil {
		call := s.inflight
		s.resolveMu.Unlock()
		select {
		case <-ctx.Done():
			return netip.Addr{}, ctx.Err()
		case <-call.done:
			return call.ip, call.err
		}
	}
	call := &resolveCall{done: make(chan struct{})}
	s.inflight = call
	s.resolveMu.Unlock()

	addrs, err := s.Resolver.LookupHost(ctx, host)
	preferred := hostdns.PreferForHosts(addrs)
	var ip netip.Addr
	if err == nil {
		if len(preferred) == 0 {
			err = fmt.Errorf("no usable addresses for %q", host)
		} else {
			ip = preferred[0]
		}
	}
	call.ip = ip
	call.err = err
	close(call.done)

	s.resolveMu.Lock()
	s.inflight = nil
	if err == nil {
		s.cachedIP = ip
		s.cachedAt = time.Now()
	}
	s.resolveMu.Unlock()
	return ip, err
}

func (s *Server) invalidateResolve() {
	s.resolveMu.Lock()
	s.cachedIP = netip.Addr{}
	s.cachedAt = time.Time{}
	s.resolveMu.Unlock()
}

func (s *Server) allowLimitLog() bool {
	const gap = int64(10 * time.Second)
	now := time.Now().UnixNano()
	prev := s.lastLimitLog.Load()
	if prev != 0 && now-prev < gap {
		return false
	}
	return s.lastLimitLog.CompareAndSwap(prev, now)
}

type ifaceBind struct {
	index int
	name  string
	net   *net.IPNet
}

func listenStillCurrent(ip netip.Addr, index int, name string) bool {
	b, ok := ifaceForIP(ip)
	return ok && b.index == index && b.name == name
}

func ifaceForIP(want netip.Addr) (ifaceBind, bool) {
	if !want.IsValid() {
		return ifaceBind{}, false
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		return ifaceBind{}, false
	}
	for _, iface := range ifaces {
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok || ipnet.IP == nil {
				continue
			}
			got, ok := netip.AddrFromSlice(ipnet.IP)
			if !ok {
				continue
			}
			if got.Unmap() == want.Unmap() {
				return ifaceBind{index: iface.Index, name: iface.Name, net: ipnet}, true
			}
		}
	}
	return ifaceBind{}, false
}

// InterfaceName returns the host interface that currently owns ip.
func InterfaceName(ip netip.Addr) (string, bool) {
	b, ok := ifaceForIP(ip)
	if !ok {
		return "", false
	}
	return b.name, true
}

// ActiveConns returns the number of in-flight relays (for tests).
func (s *Server) ActiveConns() int {
	return int(atomic.LoadInt32(&s.active))
}
