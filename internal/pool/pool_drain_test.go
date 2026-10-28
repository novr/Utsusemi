package pool

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/drainctl"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/runtimeconfig"
	"github.com/novr/utsusemi/internal/spawn"
)

type drainGateProvider struct {
	provider.Stub
	mu          sync.Mutex
	execStarted chan struct{}
	execCount   int
	emitClaim   bool
	claimOnce   sync.Once
}

func (p *drainGateProvider) IsRunning(context.Context, string) (bool, error) {
	return true, nil
}

func (p *drainGateProvider) ExecStdin(ctx context.Context, _ string, _ string, _ []string, _ []byte, _ map[string]string) error {
	p.mu.Lock()
	p.execCount++
	if p.execStarted != nil {
		select {
		case <-p.execStarted:
		default:
			close(p.execStarted)
		}
	}
	emit := p.emitClaim
	p.mu.Unlock()
	if emit {
		p.claimOnce.Do(func() {
			if w := provider.StdoutTap(ctx); w != nil {
				_, _ = io.WriteString(w, "Running job: drain-test\n")
			}
		})
	}
	<-ctx.Done()
	return ctx.Err()
}

func (p *drainGateProvider) calls() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.execCount
}

func TestPoolDrainEmptyCompletes(t *testing.T) {
	prev := poolDrainControlInterval
	poolDrainControlInterval = 20 * time.Millisecond
	t.Cleanup(func() { poolDrainControlInterval = prev })

	cfg := testPoolConfig(t)
	cfg.PoolSize = 0 // no spawns
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})

	startTestPoolRun(t, p)

	session := waitAgentSession(t, cfg.StateDir)

	reqID, err := drainctl.NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   reqID,
		AgentID:     session.ID,
		RequestedAt: time.Now().UTC(),
		Deadline:    time.Now().UTC(),
		Force:       true,
	}); err != nil {
		t.Fatal(err)
	}

	waitDrainTerminal(t, cfg.StateDir, reqID, 2*time.Second)
}

func TestPoolDrainAbandonsUnclaimedAndRefills(t *testing.T) {
	prev := poolDrainControlInterval
	poolDrainControlInterval = 20 * time.Millisecond
	t.Cleanup(func() { poolDrainControlInterval = prev })

	// Shrink ready wait for faster spawn.
	// spawn package tunables are not imported; Stub HealthCheck is instant.

	gp := &drainGateProvider{
		Stub:        *provider.NewStub(2),
		execStarted: make(chan struct{}),
	}
	cfg := testPoolConfig(t)
	cfg.PoolSize = 1
	// Long interval: refill must come from post-drain tick, not the check ticker.
	cfg.PoolCheckInterval = config.Duration(time.Hour)
	cfg.JobTimeout = config.Duration(5 * time.Second)
	cfg.SpawnTimeout = config.Duration(5 * time.Second)
	p := newTestPool(t, cfg, gp, noopRegistrar{})

	startTestPoolRun(t, p)

	select {
	case <-gp.execStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("spawn exec did not start")
	}
	session := waitAgentSession(t, cfg.StateDir)

	reqID, err := drainctl.NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   reqID,
		AgentID:     session.ID,
		RequestedAt: time.Now().UTC(),
		Deadline:    time.Now().UTC().Add(time.Minute),
		Force:       false,
	}); err != nil {
		t.Fatal(err)
	}

	st := waitDrainTerminal(t, cfg.StateDir, reqID, 3*time.Second)
	if st.State != drainctl.StateComplete {
		t.Fatalf("state=%s err=%s", st.State, st.Error)
	}

	// After drain, pool should spawn again without waiting for pool_check_interval.
	deadline := time.Now().Add(3 * time.Second)
	for gp.calls() < 2 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if gp.calls() < 2 {
		t.Fatalf("expected refill spawn, execCount=%d", gp.calls())
	}
}

func TestPoolDrainRejectsBusy(t *testing.T) {
	prev := poolDrainControlInterval
	poolDrainControlInterval = 20 * time.Millisecond
	t.Cleanup(func() { poolDrainControlInterval = prev })

	cfg := testPoolConfig(t)
	cfg.PoolSize = 0
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})

	startTestPoolRun(t, p)
	session := waitAgentSession(t, cfg.StateDir)

	// Hold poolDrain open with a fake in-flight slot so a second request is busy.
	p.mu.Lock()
	p.poolDrain = true
	p.poolDrainState = &poolDrainState{
		requestID: "first",
		agentID:   session.ID,
		deadline:  time.Now().UTC().Add(time.Minute),
	}
	p.active = 1
	p.slotControls["held"] = spawn.NewSlotControl()
	p.mu.Unlock()

	req2, err := drainctl.NewRequestID()
	if err != nil {
		t.Fatal(err)
	}
	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   req2,
		AgentID:     session.ID,
		RequestedAt: time.Now().UTC(),
		Deadline:    time.Now().UTC(),
		Force:       true,
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, ok, _ := drainctl.LoadStatus(cfg.StateDir)
		if ok && st.RejectedRequestID == req2 {
			if st.RejectedError != drainctl.ErrBusy.Error() {
				t.Fatalf("want busy, got %q", st.RejectedError)
			}
			if st.RequestID != "first" || st.State != drainctl.StateRunning {
				t.Fatalf("active drain status lost: %+v", st)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("second drain was not rejected")
}

func TestPoolDrainDiscardsStaleRequest(t *testing.T) {
	prev := poolDrainControlInterval
	poolDrainControlInterval = 20 * time.Millisecond
	t.Cleanup(func() { poolDrainControlInterval = prev })

	cfg := testPoolConfig(t)
	cfg.PoolSize = 0
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})

	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   "stale",
		AgentID:     "other-agent",
		RequestedAt: time.Now().UTC().Add(-time.Hour),
		Deadline:    time.Now().UTC(),
		Force:       true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := drainctl.WriteStatus(cfg.StateDir, drainctl.Status{
		RequestID: "stale",
		AgentID:   "other-agent",
		State:     drainctl.StateRunning,
		Active:    1,
	}); err != nil {
		t.Fatal(err)
	}

	startTestPoolRun(t, p)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		_, reqErr := drainctl.LoadRequest(cfg.StateDir)
		_, statusOK, _ := drainctl.LoadStatus(cfg.StateDir)
		if errors.Is(reqErr, drainctl.ErrNoRequest) && !statusOK {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("stale request/status not cleared")
}

func TestPoolDrainForceGraceFinishesWhenStuck(t *testing.T) {
	prevInterval := poolDrainControlInterval
	prevGrace := poolDrainForceGrace
	poolDrainControlInterval = 20 * time.Millisecond
	poolDrainForceGrace = 50 * time.Millisecond
	t.Cleanup(func() {
		poolDrainControlInterval = prevInterval
		poolDrainForceGrace = prevGrace
	})

	cfg := testPoolConfig(t)
	cfg.PoolSize = 0
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})

	startTestPoolRun(t, p)
	session := waitAgentSession(t, cfg.StateDir)

	// Simulate a wedged in-flight slot that ignores Force.
	p.mu.Lock()
	p.poolDrain = true
	p.poolDrainState = &poolDrainState{
		requestID: "stuck",
		agentID:   session.ID,
		deadline:  time.Now().UTC(),
		force:     true,
		forced:    true,
		forcedAt:  time.Now().Add(-time.Minute),
	}
	p.active = 1
	p.mu.Unlock()

	st := waitDrainTerminal(t, cfg.StateDir, "stuck", 2*time.Second)
	if st.State != drainctl.StateError {
		t.Fatalf("want error, got %+v", st)
	}
	if st.Error == "" {
		t.Fatal("expected timeout error message")
	}
	p.mu.Lock()
	draining := p.poolDrain
	p.mu.Unlock()
	if draining {
		t.Fatal("poolDrain should be cleared after force grace")
	}
}

func TestPoolDrainForceClaimed(t *testing.T) {
	prev := poolDrainControlInterval
	poolDrainControlInterval = 20 * time.Millisecond
	t.Cleanup(func() { poolDrainControlInterval = prev })

	gp := &drainGateProvider{
		Stub:        *provider.NewStub(2),
		execStarted: make(chan struct{}),
		emitClaim:   true,
	}
	cfg := testPoolConfig(t)
	cfg.PoolSize = 1
	cfg.JobTimeout = config.Duration(10 * time.Second)
	p := newTestPool(t, cfg, gp, noopRegistrar{})

	startTestPoolRun(t, p)

	select {
	case <-gp.execStarted:
	case <-time.After(3 * time.Second):
		t.Fatal("exec did not start")
	}
	session := waitAgentSession(t, cfg.StateDir)
	time.Sleep(30 * time.Millisecond)

	reqID, _ := drainctl.NewRequestID()
	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   reqID,
		AgentID:     session.ID,
		RequestedAt: time.Now().UTC(),
		Deadline:    time.Now().UTC(),
		Force:       true,
	}); err != nil {
		t.Fatal(err)
	}

	st := waitDrainTerminal(t, cfg.StateDir, reqID, 3*time.Second)
	if st.State != drainctl.StateComplete {
		t.Fatalf("state=%s err=%s", st.State, st.Error)
	}
	if !st.Forced {
		t.Fatal("expected forced=true")
	}
}

func waitAgentSession(t *testing.T, stateDir string) *lease.AgentSession {
	t.Helper()
	reg := lease.NewRegistry(stateDir)
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		session, err := reg.LoadAgentSession()
		if err != nil {
			t.Fatal(err)
		}
		snap, ok := runtimeconfig.Load(stateDir)
		// runtime_config is written only by Pool.Run (not newTestPool).
		if session != nil && ok && snap.AgentID == session.ID {
			return session
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("agent session not ready")
	return nil
}

func waitDrainTerminal(t *testing.T, stateDir, reqID string, timeout time.Duration) drainctl.Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		st, ok, err := drainctl.LoadStatus(stateDir)
		if err != nil {
			t.Fatal(err)
		}
		if ok && drainctl.MatchesRequest(st, reqID) && drainctl.IsTerminal(st) {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("drain %s did not reach terminal state", reqID)
	return drainctl.Status{}
}
