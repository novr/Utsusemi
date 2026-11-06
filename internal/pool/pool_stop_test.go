package pool

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/drainctl"
	"github.com/novr/utsusemi/internal/notify"
	"github.com/novr/utsusemi/internal/provider"
)

func TestPoolStopEmptyHolds(t *testing.T) {
	prev := poolDrainControlInterval
	poolDrainControlInterval = 20 * time.Millisecond
	t.Cleanup(func() { poolDrainControlInterval = prev })

	cfg := testPoolConfig(t)
	cfg.PoolSize = 0
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
		Stop:        true,
		Force:       true,
		Deadline:    time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	st := waitDrainTerminal(t, cfg.StateDir, reqID, 2*time.Second)
	if st.State != drainctl.StateComplete || !st.Stop {
		t.Fatalf("want complete stop, got %+v", st)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		p.mu.Lock()
		ph := p.phase
		p.mu.Unlock()
		if ph == phaseHeld {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	p.mu.Lock()
	ph := p.phase
	p.mu.Unlock()
	if ph != phaseHeld {
		t.Fatalf("phase=%v, want phaseHeld", ph)
	}

	p.tick(p.runCtx)
	p.mu.Lock()
	active := p.active
	ph = p.phase
	p.mu.Unlock()
	if active != 0 || ph != phaseHeld {
		t.Fatalf("tick must not spawn while held: active=%d phase=%v", active, ph)
	}
}

func TestPoolStopRejectsDrainWhileHeld(t *testing.T) {
	prev := poolDrainControlInterval
	poolDrainControlInterval = 20 * time.Millisecond
	t.Cleanup(func() { poolDrainControlInterval = prev })

	cfg := testPoolConfig(t)
	cfg.PoolSize = 0
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})
	startTestPoolRun(t, p)
	session := waitAgentSession(t, cfg.StateDir)

	stopID, _ := drainctl.NewRequestID()
	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   stopID,
		AgentID:     session.ID,
		RequestedAt: time.Now().UTC(),
		Stop:        true,
		Force:       true,
		Deadline:    time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	_ = waitDrainTerminal(t, cfg.StateDir, stopID, 2*time.Second)

	drainID, _ := drainctl.NewRequestID()
	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   drainID,
		AgentID:     session.ID,
		RequestedAt: time.Now().UTC(),
		Force:       true,
		Deadline:    time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		st, ok, _ := drainctl.LoadStatus(cfg.StateDir)
		if ok && st.RejectedRequestID == drainID {
			if st.RejectedError != drainctl.ErrHeld.Error() {
				t.Fatalf("want held error, got %q", st.RejectedError)
			}
			if !st.Stop || st.State != drainctl.StateComplete {
				t.Fatalf("held status lost: %+v", st)
			}
			p.mu.Lock()
			ph := p.phase
			p.mu.Unlock()
			if ph != phaseHeld {
				t.Fatalf("phase=%v after rejected drain", ph)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("drain was not rejected while held")
}

func TestPoolStopReStopCompletes(t *testing.T) {
	prev := poolDrainControlInterval
	poolDrainControlInterval = 20 * time.Millisecond
	t.Cleanup(func() { poolDrainControlInterval = prev })

	cfg := testPoolConfig(t)
	cfg.PoolSize = 0
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})
	startTestPoolRun(t, p)
	session := waitAgentSession(t, cfg.StateDir)

	first, _ := drainctl.NewRequestID()
	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   first,
		AgentID:     session.ID,
		RequestedAt: time.Now().UTC(),
		Stop:        true,
		Force:       true,
		Deadline:    time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	_ = waitDrainTerminal(t, cfg.StateDir, first, 2*time.Second)

	second, _ := drainctl.NewRequestID()
	if err := drainctl.WriteRequest(cfg.StateDir, drainctl.Request{
		RequestID:   second,
		AgentID:     session.ID,
		RequestedAt: time.Now().UTC(),
		Stop:        true,
		Force:       true,
		Deadline:    time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	st := waitDrainTerminal(t, cfg.StateDir, second, 2*time.Second)
	if st.State != drainctl.StateComplete || !st.Stop {
		t.Fatalf("re-stop: %+v", st)
	}
}

func TestPoolStopZeroDeadlineSkipsForceOnTick(t *testing.T) {
	cfg := testPoolConfig(t)
	cfg.PoolSize = 0
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})

	p.mu.Lock()
	p.phase = phasePoolDrain
	p.poolDrainState = &poolDrainState{
		requestID: "stop-wait",
		agentID:   "agent",
		stop:      true,
	}
	p.active = 1
	p.mu.Unlock()

	p.controlTick()

	p.mu.Lock()
	st := p.poolDrainState
	ph := p.phase
	p.mu.Unlock()
	if ph != phasePoolDrain {
		t.Fatalf("phase=%v, want phasePoolDrain", ph)
	}
	if st == nil || st.forced {
		t.Fatalf("zero deadline must not Force: %+v", st)
	}
}

func TestFinishPoolDrainDuringShutdownDoesNotReturnToRun(t *testing.T) {
	cfg := testPoolConfig(t)
	cfg.PoolSize = 0
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})

	p.mu.Lock()
	p.phase = phasePoolDrain
	p.poolDrainState = &poolDrainState{
		requestID: "x",
		agentID:   "a",
		stop:      false,
	}
	p.setPhase(phaseShutdown)
	p.mu.Unlock()

	p.finishPoolDrain("")

	p.mu.Lock()
	ph := p.phase
	st := p.poolDrainState
	p.mu.Unlock()
	if ph != phaseShutdown {
		t.Fatalf("phase=%v, want shutdown", ph)
	}
	if st != nil {
		t.Fatal("drain state should be cleared")
	}
}

func TestFinishStopForceGraceStaysHeld(t *testing.T) {
	cfg := testPoolConfig(t)
	cfg.PoolSize = 1
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})
	p.runCtx = context.Background()

	p.mu.Lock()
	p.phase = phasePoolDrain
	p.poolDrainState = &poolDrainState{
		requestID: "stop-stuck",
		agentID:   "agent",
		stop:      true,
		forced:    true,
	}
	p.active = 1
	p.mu.Unlock()

	p.finishPoolDrain("force reclaim timed out with runners still active")

	p.mu.Lock()
	ph := p.phase
	p.mu.Unlock()
	if ph != phaseHeld {
		t.Fatalf("phase=%v, want phaseHeld", ph)
	}
	st, ok, err := drainctl.LoadStatus(cfg.StateDir)
	if err != nil || !ok {
		t.Fatalf("status ok=%v err=%v", ok, err)
	}
	if st.State != drainctl.StateError || !st.Stop || st.Active != 1 {
		t.Fatalf("status=%+v", st)
	}

	p.mu.Lock()
	p.active = 0
	p.mu.Unlock()
	p.syncHeldStatusActive()
	st, ok, err = drainctl.LoadStatus(cfg.StateDir)
	if err != nil || !ok || st.Active != 0 || st.RequestID != "stop-stuck" {
		t.Fatalf("synced status ok=%v err=%v st=%+v", ok, err, st)
	}

	p.tick(p.runCtx)
	p.mu.Lock()
	active := p.active
	ph = p.phase
	p.mu.Unlock()
	if active != 0 || ph != phaseHeld {
		t.Fatalf("tick spawned while held: active=%d phase=%v", active, ph)
	}
}

func TestEvaluateAlertsHeldAllowsDisk(t *testing.T) {
	rec := &recordingNotifier{}
	var nowMu sync.Mutex
	now := time.Unix(1000, 0)
	p := newAlertTestPool(t, rec, time.Minute, func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	})
	p.mu.Lock()
	p.phase = phaseHeld
	p.lowDisk = true
	p.mu.Unlock()

	p.evaluateAlerts(context.Background())
	if len(rec.codes()) != 0 {
		t.Fatalf("too early: %v", rec.codes())
	}
	nowMu.Lock()
	now = now.Add(time.Minute)
	nowMu.Unlock()
	p.evaluateAlerts(context.Background())
	if got := rec.codes(); len(got) != 1 || got[0] != notify.CodeDiskBlocked {
		t.Fatalf("codes=%v", got)
	}
}
