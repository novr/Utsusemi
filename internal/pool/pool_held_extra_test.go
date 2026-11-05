package pool

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/drainctl"
	"github.com/novr/utsusemi/internal/notify"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
)

func TestReclaimRunsWhileHeld(t *testing.T) {
	cfg := testPoolConfig(t)
	reg := &trackingRegistrar{runners: []registrar.Runner{{ID: 7, Name: cfg.VMNamePrefix + "orphan"}}}
	p := newTestPool(t, cfg, provider.NewStub(2), reg)
	p.mu.Lock()
	p.phase = phaseHeld
	p.mu.Unlock()

	if err := p.reclaim(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	reg.mu.Lock()
	deleted := append([]int64(nil), reg.deleted...)
	reg.mu.Unlock()
	if len(deleted) != 1 || deleted[0] != 7 {
		t.Fatalf("deleted=%v, want orphan runner 7 removed while held", deleted)
	}
}

func TestEvaluateAlertsPoolDrainAllowsDiskOnly(t *testing.T) {
	rec := &recordingNotifier{}
	var nowMu sync.Mutex
	now := time.Unix(1000, 0)
	p := newAlertTestPool(t, rec, time.Minute, func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	})
	p.mu.Lock()
	p.phase = phasePoolDrain
	p.lowDisk = true
	p.mu.Unlock()

	p.evaluateAlerts(context.Background())
	nowMu.Lock()
	now = now.Add(time.Hour)
	nowMu.Unlock()
	p.evaluateAlerts(context.Background())
	if got := rec.codes(); len(got) != 1 || got[0] != notify.CodeDiskBlocked {
		t.Fatalf("codes=%v", got)
	}
}

func TestRejectHeldDrainSurvivesStatusSync(t *testing.T) {
	cfg := testPoolConfig(t)
	p := newTestPool(t, cfg, provider.NewStub(2), noopRegistrar{})
	p.mu.Lock()
	p.phase = phaseHeld
	p.active = 1
	p.mu.Unlock()
	if err := drainctl.WriteStatus(cfg.StateDir, drainctl.Status{
		RequestID: "stop-1",
		AgentID:   "agent",
		State:     drainctl.StateComplete,
		Active:    1,
		Stop:      true,
	}); err != nil {
		t.Fatal(err)
	}

	p.rejectHeldDrainRequest(drainctl.Request{RequestID: "drain-1", AgentID: "agent"})
	p.mu.Lock()
	p.active = 0
	p.mu.Unlock()
	p.syncHeldStatusActive()

	st, ok, err := drainctl.LoadStatus(cfg.StateDir)
	if err != nil || !ok {
		t.Fatalf("status ok=%v err=%v", ok, err)
	}
	if st.Active != 0 || st.RejectedRequestID != "drain-1" || st.RequestID != "stop-1" {
		t.Fatalf("status=%+v", st)
	}
}
