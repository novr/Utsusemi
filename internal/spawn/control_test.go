package spawn

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

type gateProvider struct {
	provider.Stub
	mu              sync.Mutex
	execStarted     chan struct{}
	claimOnce       sync.Once
	emitClaim       bool
	emitListening   bool
	emitConnectErr  bool
	connectOnce     sync.Once
	listeningOnce   sync.Once
	cloneDelay      time.Duration
	cloneStarted    chan struct{}
	cloneErr        error
	hostsSetupCalls int
	hostsSetupErr   error
	lastHostsStdin  []byte
	cloneCalls      int
}

func (p *gateProvider) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}

func (p *gateProvider) Clone(ctx context.Context, _, _ string) error {
	p.mu.Lock()
	p.cloneCalls++
	p.mu.Unlock()
	if p.cloneStarted != nil {
		select {
		case <-p.cloneStarted:
		default:
			close(p.cloneStarted)
		}
	}
	if p.cloneErr != nil {
		return p.cloneErr
	}
	if p.cloneDelay > 0 {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(p.cloneDelay):
		}
	}
	return ctx.Err()
}

func (p *gateProvider) ExecStdin(ctx context.Context, _ string, _ string, args []string, stdin []byte, _ map[string]string) error {
	// Hosts setup embeds hosts_setup.sh via bash -c and has no stdout tap.
	if provider.StdoutTap(ctx) == nil && len(args) >= 2 && args[0] == "-c" && strings.Contains(args[1], "utsusemi proxy hosts") {
		p.mu.Lock()
		p.hostsSetupCalls++
		p.lastHostsStdin = append([]byte(nil), stdin...)
		err := p.hostsSetupErr
		p.mu.Unlock()
		return err
	}
	if provider.StdoutTap(ctx) == nil {
		return nil
	}

	p.mu.Lock()
	if p.execStarted != nil {
		select {
		case <-p.execStarted:
		default:
			close(p.execStarted)
		}
	}
	emit := p.emitClaim
	emitListen := p.emitListening
	emitConnect := p.emitConnectErr
	p.mu.Unlock()

	if emitConnect {
		p.connectOnce.Do(func() {
			if w := provider.StdoutTap(ctx); w != nil {
				_, _ = io.WriteString(w, "Runner connect error: nodename nor servname provided, or not known (proxy.example:8080). Retrying until reconnected.\n")
			}
		})
	}
	if emitListen {
		p.listeningOnce.Do(func() {
			if w := provider.StdoutTap(ctx); w != nil {
				_, _ = io.WriteString(w, "Listening for Jobs\n")
			}
		})
	}
	if emit {
		p.claimOnce.Do(func() {
			if w := provider.StdoutTap(ctx); w != nil {
				_, _ = io.WriteString(w, "Running job: test\n")
			}
		})
	}
	<-ctx.Done()
	return ctx.Err()
}

func testSpawner(t *testing.T, p provider.VMProvider) *Spawner {
	t.Helper()
	cfg := &config.Config{
		BaseImage:     "image",
		RunnerVersion: "2.336.0",
		Labels:        []string{"self-hosted"},
		SpawnTimeout:  config.Duration(2 * time.Second),
		JobTimeout:    config.Duration(2 * time.Second),
		MinFreeDiskGB: 1,
		StateDir:      t.TempDir(),
	}
	return New(Options{
		Config:   cfg,
		Target:   target.Target{Type: target.TypeRepo, Owner: "a", Repo: "b"},
		Provider: p,
		Registrar: &fakeRegistrar{jit: registrar.JITConfig{
			Encoded: "jit",
			Runner:  registrar.Runner{ID: 1, Name: "vm"},
		}},
		Logger: slog.Default(),
	})
}

func TestSlotControlAbandonIdleUnclaimed(t *testing.T) {
	readyTimeout = 50 * time.Millisecond
	readyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		readyTimeout = 3 * time.Minute
		readyPollInterval = 2 * time.Second
	})

	gp := &gateProvider{
		Stub:        *provider.NewStub(2),
		execStarted: make(chan struct{}),
	}
	spawner := testSpawner(t, gp)
	ctrl := NewSlotControl()

	done := make(chan error, 1)
	go func() {
		_, err := spawner.Run(context.Background(), "utsusemi-idle", ctrl, nil)
		done <- err
	}()

	select {
	case <-gp.execStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("exec did not start")
	}
	ctrl.AbandonIdle()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want canceled, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("spawn did not stop after abandon")
	}
}

func TestSlotControlAbandonIdleIgnoresClaimed(t *testing.T) {
	readyTimeout = 50 * time.Millisecond
	readyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		readyTimeout = 3 * time.Minute
		readyPollInterval = 2 * time.Second
	})

	gp := &gateProvider{
		Stub:        *provider.NewStub(2),
		execStarted: make(chan struct{}),
		emitClaim:   true,
	}
	spawner := testSpawner(t, gp)
	ctrl := NewSlotControl()

	done := make(chan error, 1)
	go func() {
		_, err := spawner.Run(context.Background(), "utsusemi-claimed", ctrl, nil)
		done <- err
	}()

	select {
	case <-gp.execStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("exec did not start")
	}
	time.Sleep(20 * time.Millisecond)
	ctrl.AbandonIdle()

	select {
	case err := <-done:
		t.Fatalf("abandon should not stop claimed job, got %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	ctrl.Force()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want canceled after force, got %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("force did not stop claimed job")
	}
}

func TestSlotControlAbandonDuringClone(t *testing.T) {
	gp := &gateProvider{
		Stub:         *provider.NewStub(2),
		cloneDelay:   time.Hour,
		cloneStarted: make(chan struct{}),
	}
	spawner := testSpawner(t, gp)
	ctrl := NewSlotControl()

	done := make(chan error, 1)
	go func() {
		_, err := spawner.Run(context.Background(), "utsusemi-clone", ctrl, nil)
		done <- err
	}()

	select {
	case <-gp.cloneStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("clone did not start")
	}
	ctrl.AbandonIdle()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("clone abandon timed out")
	}
}

func TestIdleReadyOnListening(t *testing.T) {
	readyTimeout = 50 * time.Millisecond
	readyPollInterval = 5 * time.Millisecond
	listeningGrace = time.Hour
	t.Cleanup(func() {
		readyTimeout = 3 * time.Minute
		readyPollInterval = 2 * time.Second
		listeningGrace = 5 * time.Minute
	})

	gp := &gateProvider{
		Stub:          *provider.NewStub(2),
		execStarted:   make(chan struct{}),
		emitListening: true,
	}
	spawner := testSpawner(t, gp)
	ctrl := NewSlotControl()

	idleCh := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := spawner.Run(context.Background(), "utsusemi-listen", ctrl, func() { close(idleCh) })
		done <- err
	}()

	select {
	case <-idleCh:
	case <-time.After(2 * time.Second):
		t.Fatal("onIdleReady not called after Listening")
	}
	ctrl.Force()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("spawn did not stop")
	}
}

func TestIdleReadyOnListeningGrace(t *testing.T) {
	readyTimeout = 50 * time.Millisecond
	readyPollInterval = 5 * time.Millisecond
	listeningGrace = 40 * time.Millisecond
	t.Cleanup(func() {
		readyTimeout = 3 * time.Minute
		readyPollInterval = 2 * time.Second
		listeningGrace = 5 * time.Minute
	})

	gp := &gateProvider{
		Stub:        *provider.NewStub(2),
		execStarted: make(chan struct{}),
	}
	spawner := testSpawner(t, gp)
	ctrl := NewSlotControl()

	var calls int
	var mu sync.Mutex
	idleCh := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := spawner.Run(context.Background(), "utsusemi-grace", ctrl, func() {
			mu.Lock()
			calls++
			if calls == 1 {
				close(idleCh)
			}
			mu.Unlock()
		})
		done <- err
	}()

	select {
	case <-gp.execStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("exec did not start")
	}
	select {
	case <-idleCh:
	case <-time.After(2 * time.Second):
		t.Fatal("onIdleReady not called after listeningGrace")
	}
	ctrl.Force()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("spawn did not stop")
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Fatalf("onIdleReady calls=%d, want 1", n)
	}
}

func TestIdleReadyNotCalledOnEarlyCloneFailure(t *testing.T) {
	gp := &gateProvider{
		Stub:     *provider.NewStub(2),
		cloneErr: errors.New("clone failed"),
	}
	spawner := testSpawner(t, gp)

	var calls int
	_, err := spawner.Run(context.Background(), "utsusemi-fail", nil, func() { calls++ })
	if err == nil {
		t.Fatal("expected clone error")
	}
	if calls != 0 {
		t.Fatalf("onIdleReady calls=%d, want 0 (pool releases warming on Run return)", calls)
	}
}

type trackingDeleteRegistrar struct {
	fakeRegistrar
	mu      sync.Mutex
	deletes []int64
	jitErr  error
}

func (r *trackingDeleteRegistrar) CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (registrar.JITConfig, error) {
	if r.jitErr != nil {
		return registrar.JITConfig{}, r.jitErr
	}
	return r.fakeRegistrar.CreateJIT(ctx, tgt, labels, name)
}

func (r *trackingDeleteRegistrar) DeleteRunner(_ context.Context, _ target.Target, runnerID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deletes = append(r.deletes, runnerID)
	return nil
}

func (r *trackingDeleteRegistrar) deleteCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.deletes)
}

type mapHostResolver map[string][]netip.Addr

func (m mapHostResolver) LookupHost(_ context.Context, host string) ([]netip.Addr, error) {
	addrs, ok := m[host]
	if !ok {
		return nil, errors.New("not found")
	}
	return addrs, nil
}

func TestListenerConnectErrorFailsSpawn(t *testing.T) {
	readyTimeout = 50 * time.Millisecond
	readyPollInterval = 5 * time.Millisecond
	connectErrorGrace = 40 * time.Millisecond
	connectErrorPoll = 10 * time.Millisecond
	listeningGrace = time.Hour
	t.Cleanup(func() {
		readyTimeout = 3 * time.Minute
		readyPollInterval = 2 * time.Second
		connectErrorGrace = 2 * time.Minute
		connectErrorPoll = 250 * time.Millisecond
		listeningGrace = 5 * time.Minute
	})

	gp := &gateProvider{
		Stub:           *provider.NewStub(2),
		execStarted:    make(chan struct{}),
		emitConnectErr: true,
	}
	reg := &trackingDeleteRegistrar{fakeRegistrar: fakeRegistrar{jit: registrar.JITConfig{
		Encoded: "jit",
		Runner:  registrar.Runner{ID: 42, Name: "vm"},
	}}}
	spawner := testSpawner(t, gp)
	spawner.opts.Registrar = reg

	var idleCalls int
	done := make(chan error, 1)
	go func() {
		_, err := spawner.Run(context.Background(), "utsusemi-connect", nil, func() { idleCalls++ })
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, ErrListenerNotConnected) {
			t.Fatalf("want ErrListenerNotConnected, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("spawn did not fail on connect error")
	}
	if idleCalls != 0 {
		t.Fatalf("idle-ready calls=%d, want 0", idleCalls)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if reg.deleteCount() >= 1 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("DeleteRunner calls=%d, want >=1", reg.deleteCount())
}

func TestResolveProxyOnHostInjectsHostsBeforeJIT(t *testing.T) {
	readyTimeout = 50 * time.Millisecond
	readyPollInterval = 5 * time.Millisecond
	listeningGrace = time.Hour
	t.Cleanup(func() {
		readyTimeout = 3 * time.Minute
		readyPollInterval = 2 * time.Second
		listeningGrace = 5 * time.Minute
	})

	gp := &gateProvider{
		Stub:          *provider.NewStub(2),
		execStarted:   make(chan struct{}),
		emitListening: true,
	}
	spawner := testSpawner(t, gp)
	spawner.opts.Config.ResolveProxyOnHost = true
	spawner.opts.Config.Env = map[string]string{
		"https_proxy": "https://user:pass@proxy.example:8443",
	}
	spawner.opts.Resolver = mapHostResolver{
		"proxy.example": {netip.MustParseAddr("10.0.0.5"), netip.MustParseAddr("2001:db8::1")},
	}

	ctrl := NewSlotControl()
	done := make(chan error, 1)
	go func() {
		_, err := spawner.Run(context.Background(), "utsusemi-hosts", ctrl, nil)
		done <- err
	}()

	select {
	case <-gp.execStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("job exec did not start")
	}
	gp.mu.Lock()
	stdin := string(gp.lastHostsStdin)
	calls := gp.hostsSetupCalls
	gp.mu.Unlock()
	if calls != 1 {
		t.Fatalf("hosts setup calls=%d", calls)
	}
	if !strings.Contains(stdin, "10.0.0.5 proxy.example") {
		t.Fatalf("hosts stdin=%q", stdin)
	}
	if strings.Contains(stdin, "2001:db8::1") {
		t.Fatalf("should prefer IPv4: %q", stdin)
	}
	if strings.Contains(stdin, "user:pass") {
		t.Fatal("must not put credentials in hosts stdin")
	}
	ctrl.Force()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("spawn did not stop")
	}
}

func TestResolveProxyOnHostFailureSkipsClone(t *testing.T) {
	gp := &gateProvider{Stub: *provider.NewStub(2)}
	spawner := testSpawner(t, gp)
	spawner.opts.Config.ResolveProxyOnHost = true
	spawner.opts.Config.Env = map[string]string{"http_proxy": "http://missing.example:8080"}
	spawner.opts.Resolver = mapHostResolver{}

	_, err := spawner.Run(context.Background(), "utsusemi-dns-fail", nil, nil)
	if err == nil {
		t.Fatal("expected resolve error")
	}
	gp.mu.Lock()
	clones := gp.cloneCalls
	gp.mu.Unlock()
	if clones != 0 {
		t.Fatalf("clone calls=%d, want 0", clones)
	}
}

func TestHostsSetupFailureSkipsJIT(t *testing.T) {
	readyTimeout = 50 * time.Millisecond
	readyPollInterval = 5 * time.Millisecond
	t.Cleanup(func() {
		readyTimeout = 3 * time.Minute
		readyPollInterval = 2 * time.Second
	})

	gp := &gateProvider{
		Stub:          *provider.NewStub(2),
		hostsSetupErr: errors.New("sudo failed"),
	}
	reg := &trackingDeleteRegistrar{fakeRegistrar: fakeRegistrar{jit: registrar.JITConfig{
		Encoded: "jit",
		Runner:  registrar.Runner{ID: 7, Name: "vm"},
	}}}
	spawner := testSpawner(t, gp)
	spawner.opts.Registrar = reg
	spawner.opts.Config.ResolveProxyOnHost = true
	spawner.opts.Config.Env = map[string]string{"http_proxy": "http://proxy.example:8080"}
	spawner.opts.Resolver = mapHostResolver{
		"proxy.example": {netip.MustParseAddr("10.0.0.5")},
	}

	_, err := spawner.Run(context.Background(), "utsusemi-hosts-fail", nil, nil)
	if err == nil {
		t.Fatal("expected hosts setup error")
	}
	if reg.deleteCount() != 0 {
		t.Fatalf("JIT should not run; deletes=%d", reg.deleteCount())
	}
}
