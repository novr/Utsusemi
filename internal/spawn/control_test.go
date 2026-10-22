package spawn

import (
	"context"
	"errors"
	"io"
	"log/slog"
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
	mu             sync.Mutex
	execStarted    chan struct{}
	claimOnce      sync.Once
	emitClaim      bool
	emitListening  bool
	listeningOnce  sync.Once
	cloneDelay     time.Duration
	cloneStarted   chan struct{}
	cloneErr       error
}

func (p *gateProvider) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}

func (p *gateProvider) Clone(ctx context.Context, _, _ string) error {
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

func (p *gateProvider) ExecStdin(ctx context.Context, _ string, _ string, _ []string, _ []byte, _ map[string]string) error {
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
	p.mu.Unlock()

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
