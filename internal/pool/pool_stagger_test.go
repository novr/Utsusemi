package pool

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/spawn"
)

type staggerGateProvider struct {
	provider.Stub
	mu          sync.Mutex
	clones      int
	execs       int
	failFirst   bool
	firstFailed bool
	firstExec   chan struct{}
	secondClone chan struct{}
	allowListen chan struct{}
}

func (p *staggerGateProvider) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}

func (p *staggerGateProvider) Clone(ctx context.Context, _, _ string) error {
	p.mu.Lock()
	p.clones++
	n := p.clones
	fail := p.failFirst && !p.firstFailed
	if fail {
		p.firstFailed = true
	}
	secondCh := p.secondClone
	p.mu.Unlock()
	if n == 2 && secondCh != nil {
		select {
		case <-secondCh:
		default:
			close(secondCh)
		}
	}
	if fail {
		return errors.New("clone failed")
	}
	return ctx.Err()
}

func (p *staggerGateProvider) ExecStdin(ctx context.Context, _ string, _ string, _ []string, _ []byte, _ map[string]string) error {
	p.mu.Lock()
	p.execs++
	n := p.execs
	firstCh := p.firstExec
	allow := p.allowListen
	p.mu.Unlock()

	if n == 1 && firstCh != nil {
		select {
		case <-firstCh:
		default:
			close(firstCh)
		}
	}

	if allow != nil {
		select {
		case <-allow:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	if w := provider.StdoutTap(ctx); w != nil {
		_, _ = io.WriteString(w, "Listening for Jobs\n")
	}
	<-ctx.Done()
	return ctx.Err()
}

func (p *staggerGateProvider) cloneCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clones
}

func TestPoolStaggersSecondSpawnUntilListening(t *testing.T) {
	prevGrace := spawn.SetListeningGraceForTest(time.Hour)
	t.Cleanup(func() { spawn.SetListeningGraceForTest(prevGrace) })

	gp := &staggerGateProvider{
		Stub:        *provider.NewStub(2),
		firstExec:   make(chan struct{}),
		secondClone: make(chan struct{}),
		allowListen: make(chan struct{}),
	}
	cfg := testPoolConfig(t)
	cfg.PoolSize = 2
	cfg.PoolCheckInterval = config.Duration(time.Hour) // second fill from idle-ready tick
	cfg.JobTimeout = config.Duration(5 * time.Second)
	cfg.SpawnTimeout = config.Duration(5 * time.Second)
	p := newTestPool(t, cfg, gp, noopRegistrar{})
	startTestPoolRun(t, p)

	select {
	case <-gp.firstExec:
	case <-time.After(3 * time.Second):
		t.Fatal("first exec did not start")
	}

	// While first is pre-Listening, further ticks must not clone a second VM.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		if gp.cloneCount() > 1 {
			t.Fatalf("second clone before Listening, clones=%d", gp.cloneCount())
		}
		time.Sleep(20 * time.Millisecond)
	}

	close(gp.allowListen)

	select {
	case <-gp.secondClone:
	case <-time.After(3 * time.Second):
		t.Fatalf("second clone did not start after Listening, clones=%d", gp.cloneCount())
	}
}

func TestPoolIdleReadyOnClaimWithoutListening(t *testing.T) {
	prevGrace := spawn.SetListeningGraceForTest(time.Hour)
	t.Cleanup(func() { spawn.SetListeningGraceForTest(prevGrace) })

	gp := &claimOnlyGateProvider{
		Stub:        *provider.NewStub(2),
		firstExec:   make(chan struct{}),
		secondClone: make(chan struct{}),
	}
	cfg := testPoolConfig(t)
	cfg.PoolSize = 2
	cfg.PoolCheckInterval = config.Duration(time.Hour) // refill must not wait on interval
	cfg.JobTimeout = config.Duration(5 * time.Second)
	cfg.SpawnTimeout = config.Duration(5 * time.Second)
	p := newTestPool(t, cfg, gp, noopRegistrar{})
	startTestPoolRun(t, p)

	select {
	case <-gp.firstExec:
	case <-time.After(3 * time.Second):
		t.Fatal("first exec did not start")
	}
	select {
	case <-gp.secondClone:
	case <-time.After(3 * time.Second):
		t.Fatalf("second clone after claim marker, clones=%d", gp.cloneCount())
	}
}

type claimOnlyGateProvider struct {
	provider.Stub
	mu          sync.Mutex
	clones      int
	execs       int
	firstExec   chan struct{}
	secondClone chan struct{}
}

func (p *claimOnlyGateProvider) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}

func (p *claimOnlyGateProvider) Clone(ctx context.Context, _, _ string) error {
	p.mu.Lock()
	p.clones++
	n := p.clones
	secondCh := p.secondClone
	p.mu.Unlock()
	if n == 2 && secondCh != nil {
		select {
		case <-secondCh:
		default:
			close(secondCh)
		}
	}
	return ctx.Err()
}

func (p *claimOnlyGateProvider) ExecStdin(ctx context.Context, _ string, _ string, _ []string, _ []byte, _ map[string]string) error {
	p.mu.Lock()
	p.execs++
	n := p.execs
	firstCh := p.firstExec
	p.mu.Unlock()
	if n == 1 && firstCh != nil {
		select {
		case <-firstCh:
		default:
			close(firstCh)
		}
	}
	// Claim without Listening — still ends cold start for fill gating.
	if w := provider.StdoutTap(ctx); w != nil {
		_, _ = io.WriteString(w, "Running job: instant\n")
	}
	<-ctx.Done()
	return ctx.Err()
}

func (p *claimOnlyGateProvider) cloneCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.clones
}

func TestPoolReleasesWarmingOnSpawnFailure(t *testing.T) {
	prevGrace := spawn.SetListeningGraceForTest(time.Hour)
	t.Cleanup(func() { spawn.SetListeningGraceForTest(prevGrace) })

	gp := &staggerGateProvider{
		Stub:        *provider.NewStub(2),
		failFirst:   true,
		firstExec:   make(chan struct{}),
		secondClone: make(chan struct{}),
		allowListen: make(chan struct{}),
	}
	// Second spawn should be allowed to listen immediately.
	close(gp.allowListen)

	cfg := testPoolConfig(t)
	cfg.PoolSize = 2
	cfg.PoolCheckInterval = config.Duration(20 * time.Millisecond)
	cfg.JobTimeout = config.Duration(5 * time.Second)
	cfg.SpawnTimeout = config.Duration(5 * time.Second)
	p := newTestPool(t, cfg, gp, noopRegistrar{})
	startTestPoolRun(t, p)

	// recordFailure applies failures*10s backoff after the first clone error.
	select {
	case <-gp.secondClone:
	case <-time.After(15 * time.Second):
		t.Fatalf("expected clone after first failure, clones=%d", gp.cloneCount())
	}
	select {
	case <-gp.firstExec:
	case <-time.After(3 * time.Second):
		t.Fatal("successful spawn exec did not start")
	}
}
