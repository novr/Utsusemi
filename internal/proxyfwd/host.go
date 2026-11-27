package proxyfwd

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/novr/utsusemi/internal/hostdns"
	"github.com/novr/utsusemi/internal/provider"
)

const (
	DefaultMaxConns     = 512
	DefaultDialTimeout  = 10 * time.Second
	DefaultKeepAlive    = 30 * time.Second
	DefaultDrainTimeout = 30 * time.Second
	DefaultResolveTTL   = 30 * time.Second
	DefaultReadyTimeout = 15 * time.Second
)

// guestLaunchScript waits briefly for the read-only VirtioFS binary mount,
// copies the binary onto the guest disk (matching action_cache), and execs
// `utsusemi proxy-guest --listen <addr>`. Errors go to stderr only so stdout
// stays clean for binary mux frames.
const guestLaunchScript = `mount_dir="$1"
listen="$2"
for _ in 1 2 3 4 5 6 7 8 9 10 11 12 13 14 15 16 17 18 19 20; do
  [ -f "${mount_dir}/utsusemi" ] && break
  sleep 0.25
done
if [ ! -f "${mount_dir}/utsusemi" ]; then
  echo "proxy-guest: missing ${mount_dir}/utsusemi" >&2
  exit 1
fi
dir="${HOME}/utsusemi-proxy-forward"
mkdir -p "$dir"
cp "${mount_dir}/utsusemi" "${dir}/utsusemi"
chmod +x "${dir}/utsusemi"
xattr -d com.apple.quarantine "${dir}/utsusemi" 2>/dev/null || true
exec "${dir}/utsusemi" proxy-guest --listen "$listen"`

// Forwarder opens per-VM tunnels over `tart exec -i` and dials Upstream from
// the host for each guest connection. The host never opens a listening socket.
type Forwarder struct {
	Listen   string
	Upstream string
	Resolver hostdns.Resolver
	Logger   *slog.Logger

	MaxConns     int
	DialTimeout  time.Duration
	KeepAlive    time.Duration
	DrainTimeout time.Duration
	ResolveTTL   time.Duration
	ReadyTimeout time.Duration

	lastLimitLog atomic.Int64
	lastDialLog  atomic.Int64

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

// New builds a Forwarder for guest loopback Listen → host-dialed Upstream.
func New(listen, upstream string, resolver hostdns.Resolver, logger *slog.Logger) *Forwarder {
	if logger == nil {
		logger = slog.Default()
	}
	if resolver == nil {
		resolver = hostdns.Default()
	}
	return &Forwarder{
		Listen:       listen,
		Upstream:     upstream,
		Resolver:     resolver,
		Logger:       logger,
		MaxConns:     DefaultMaxConns,
		DialTimeout:  DefaultDialTimeout,
		KeepAlive:    DefaultKeepAlive,
		DrainTimeout: DefaultDrainTimeout,
		ResolveTTL:   DefaultResolveTTL,
		ReadyTimeout: DefaultReadyTimeout,
	}
}

// Start launches `utsusemi proxy-guest` inside vmName via provider.OpenExecPipe,
// waits until the guest reports READY, and returns an io.Closer that stops the
// tunnel when the VM finishes. readyCtx bounds startup only; the returned
// tunnel lives until Close() or the guest exits.
func (f *Forwarder) Start(readyCtx context.Context, p provider.VMProvider, vmName string) (io.Closer, error) {
	pipeProvider, ok := p.(provider.ExecPipeProvider)
	if !ok {
		return nil, fmt.Errorf("proxyfwd: provider does not support interactive exec pipes")
	}
	tunnelCtx, tunnelCancel := context.WithCancel(context.Background())
	pipe, err := pipeProvider.OpenExecPipe(
		tunnelCtx,
		vmName,
		"bash",
		[]string{"-c", guestLaunchScript, "_", GuestMountDir(), f.Listen},
	)
	if err != nil {
		tunnelCancel()
		return nil, fmt.Errorf("proxyfwd open guest pipe: %w", err)
	}

	tunnel, err := f.servePipe(readyCtx, tunnelCtx, tunnelCancel, pipe, vmName)
	if err != nil {
		stderr := pipe.Stderr()
		tunnelCancel()
		_ = pipe.Close()
		if stderr != "" {
			return nil, fmt.Errorf("%w (guest stderr: %s)", err, stderr)
		}
		return nil, err
	}
	return tunnel, nil
}

// ServePipe attaches the host side of a tunnel to an arbitrary bidirectional
// stream (used by Start and in-memory unit tests).
func (f *Forwarder) ServePipe(readyCtx context.Context, rw io.ReadWriteCloser) (io.Closer, error) {
	tunnelCtx, tunnelCancel := context.WithCancel(context.Background())
	tunnel, err := f.servePipe(readyCtx, tunnelCtx, tunnelCancel, rw, "")
	if err != nil {
		tunnelCancel()
		_ = rw.Close()
		return nil, err
	}
	return tunnel, nil
}

type hostTunnel struct {
	sess      *muxSession
	cancel    context.CancelFunc
	closing   atomic.Bool
	closeOnce sync.Once
	closeErr  error
}

func (t *hostTunnel) Close() error {
	t.closeOnce.Do(func() {
		t.closing.Store(true)
		t.cancel()
		t.closeErr = t.sess.Close()
		t.sess.Wait()
	})
	return t.closeErr
}

func (f *Forwarder) servePipe(readyCtx, tunnelCtx context.Context, tunnelCancel context.CancelFunc, rw io.ReadWriteCloser, vmName string) (io.Closer, error) {
	// MaxConns is per tunnel (one VM), so one guest cannot exhaust the others.
	var active int32
	sess := newMuxSession(rw, func(st *muxStream) {
		if int(atomic.AddInt32(&active, 1)) > f.maxConns() {
			atomic.AddInt32(&active, -1)
			if allowRateLimited(&f.lastLimitLog, 10*time.Second) {
				f.Logger.Warn("proxy forward max connections", "max", f.maxConns(), "listen", f.Listen)
			}
			st.reject("max connections reached")
			return
		}
		defer atomic.AddInt32(&active, -1)
		f.handleStream(tunnelCtx, st)
	})

	timeout := f.ReadyTimeout
	if timeout <= 0 {
		timeout = DefaultReadyTimeout
	}
	waitCtx, waitCancel := context.WithTimeout(readyCtx, timeout)
	bound, err := sess.waitReady(waitCtx)
	waitCancel()
	if err != nil {
		_ = sess.Close()
		sess.Wait()
		return nil, fmt.Errorf("proxyfwd wait for guest ready: %w", err)
	}

	attrs := []any{"listen", bound, "upstream", f.Upstream}
	if vmName != "" {
		attrs = append(attrs, "vm", vmName)
	}
	f.Logger.Info("proxy forward tunnel ready", attrs...)
	t := &hostTunnel{sess: sess, cancel: tunnelCancel}
	go func() {
		<-sess.Done()
		if !t.closing.Load() {
			f.Logger.Warn("proxy forward tunnel closed unexpectedly; guest traffic via listen will fail", append([]any{"error", sess.err()}, attrs[2:]...)...)
		}
	}()
	return t, nil
}

func (f *Forwarder) handleStream(ctx context.Context, st *muxStream) {
	defer st.Close()
	upConn, err := f.dialUpstream(ctx)
	if err != nil {
		if allowRateLimited(&f.lastDialLog, 10*time.Second) {
			f.Logger.Warn("proxy forward dial upstream failed", "upstream", f.Upstream, "error", err)
		}
		st.reject("dial upstream failed")
		return
	}
	defer upConn.Close()
	setKeepAlive(upConn, f.KeepAlive)

	if err := st.accept(); err != nil {
		return
	}
	relayBiDir(ctx, upConn, st, f.DrainTimeout)
}

func (f *Forwarder) maxConns() int {
	if f.MaxConns <= 0 {
		return DefaultMaxConns
	}
	return f.MaxConns
}

func (f *Forwarder) dialUpstream(ctx context.Context) (net.Conn, error) {
	host, port, err := net.SplitHostPort(f.Upstream)
	if err != nil {
		return nil, err
	}
	timeout := f.DialTimeout
	if timeout <= 0 {
		timeout = DefaultDialTimeout
	}
	dctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if ip, err := netip.ParseAddr(host); err == nil {
		return (&net.Dialer{Timeout: timeout, KeepAlive: f.KeepAlive}).DialContext(dctx, "tcp", net.JoinHostPort(ip.Unmap().String(), port))
	}

	ip, err := f.resolveUpstream(dctx, host)
	if err != nil {
		return nil, err
	}
	conn, err := (&net.Dialer{Timeout: timeout, KeepAlive: f.KeepAlive}).DialContext(dctx, "tcp", net.JoinHostPort(ip.Unmap().String(), port))
	if err != nil {
		f.invalidateResolve()
		return nil, err
	}
	return conn, nil
}

func (f *Forwarder) resolveUpstream(ctx context.Context, host string) (netip.Addr, error) {
	ttl := f.ResolveTTL
	if ttl <= 0 {
		ttl = DefaultResolveTTL
	}
	now := time.Now()

	f.resolveMu.Lock()
	if f.cachedIP.IsValid() && now.Sub(f.cachedAt) < ttl {
		ip := f.cachedIP
		f.resolveMu.Unlock()
		return ip, nil
	}
	if f.inflight != nil {
		call := f.inflight
		f.resolveMu.Unlock()
		select {
		case <-ctx.Done():
			return netip.Addr{}, ctx.Err()
		case <-call.done:
			return call.ip, call.err
		}
	}
	call := &resolveCall{done: make(chan struct{})}
	f.inflight = call
	f.resolveMu.Unlock()

	addrs, err := f.Resolver.LookupHost(ctx, host)
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

	f.resolveMu.Lock()
	f.inflight = nil
	if err == nil {
		f.cachedIP = ip
		f.cachedAt = time.Now()
	}
	f.resolveMu.Unlock()
	return ip, err
}

func (f *Forwarder) invalidateResolve() {
	f.resolveMu.Lock()
	f.cachedIP = netip.Addr{}
	f.cachedAt = time.Time{}
	f.resolveMu.Unlock()
}

func allowRateLimited(last *atomic.Int64, gap time.Duration) bool {
	now := time.Now().UnixNano()
	prev := last.Load()
	if prev != 0 && now-prev < int64(gap) {
		return false
	}
	return last.CompareAndSwap(prev, now)
}

type relayHalfCloser interface {
	io.ReadWriteCloser
	CloseWrite() error
}

type copyResult struct {
	aToB bool
	err  error
}

// relayBiDir copies bytes between a and b, preserving half-close on clean EOF
// and tearing down promptly on error or context cancellation.
func relayBiDir(ctx context.Context, a io.ReadWriteCloser, b relayHalfCloser, drain time.Duration) {
	if drain <= 0 {
		drain = DefaultDrainTimeout
	}
	res := make(chan copyResult, 2)
	go func() {
		_, err := io.Copy(b, a)
		res <- copyResult{aToB: true, err: err}
	}()
	go func() {
		_, err := io.Copy(a, b)
		res <- copyResult{aToB: false, err: err}
	}()

	var first copyResult
	select {
	case <-ctx.Done():
		_ = a.Close()
		_ = b.Close()
		<-res
		<-res
		return
	case first = <-res:
	}

	if first.err != nil {
		_ = a.Close()
		_ = b.Close()
		<-res
		return
	}

	if first.aToB {
		_ = b.CloseWrite()
	} else {
		_ = closeWrite(a)
	}

	timer := time.NewTimer(drain)
	defer timer.Stop()
	select {
	case <-res:
		return
	case <-timer.C:
		_ = a.Close()
		_ = b.Close()
		<-res
		return
	case <-ctx.Done():
		_ = a.Close()
		_ = b.Close()
		<-res
		return
	}
}

func closeWrite(c io.Closer) error {
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
