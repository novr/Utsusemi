package pool

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/notify"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/target"
)

type recordingNotifier struct {
	mu     sync.Mutex
	events []notify.Event
}

func (r *recordingNotifier) Alert(ctx context.Context, e notify.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recordingNotifier) codes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Code
	}
	return out
}

func newAlertTestPool(t *testing.T, rec *recordingNotifier, stuckAfter time.Duration, now func() time.Time) *Pool {
	t.Helper()
	cfg := testPoolConfig(t)
	p := NewWithOptions(Options{
		Config:     cfg,
		Target:     target.Target{Type: target.TypeRepo, Owner: "a", Repo: "b"},
		Provider:   provider.NewStub(2),
		Registrar:  noopRegistrar{},
		Notifier:   rec,
		StuckAfter: stuckAfter,
		Now:        now,
	})
	p.effectivePrefix = cfg.VMNamePrefix
	return p
}

func TestEvaluateAlertsPoolStuck(t *testing.T) {
	rec := &recordingNotifier{}
	var nowMu sync.Mutex
	now := time.Unix(1000, 0)
	p := newAlertTestPool(t, rec, time.Minute, func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	})

	p.recordFailure(context.Canceled)
	p.evaluateAlerts(context.Background())
	if len(rec.codes()) != 0 {
		t.Fatalf("too early: %v", rec.codes())
	}

	nowMu.Lock()
	now = now.Add(time.Minute)
	nowMu.Unlock()
	p.evaluateAlerts(context.Background())
	if got := rec.codes(); len(got) != 1 || got[0] != notify.CodePoolStuck {
		t.Fatalf("codes=%v", got)
	}
	rec.mu.Lock()
	detail := rec.events[0].Detail
	rec.mu.Unlock()
	if detail == "" {
		t.Fatal("expected lastSpawnErr in detail")
	}

	p.evaluateAlerts(context.Background())
	if len(rec.codes()) != 1 {
		t.Fatalf("dedupe failed: %v", rec.codes())
	}

	p.resetSpawnBackoff()
	nowMu.Lock()
	now = now.Add(time.Minute)
	nowMu.Unlock()
	p.evaluateAlerts(context.Background())
	nowMu.Lock()
	now = now.Add(time.Minute)
	nowMu.Unlock()
	p.evaluateAlerts(context.Background())
	if len(rec.codes()) != 2 {
		t.Fatalf("after recover want 2, got %v", rec.codes())
	}
}

func TestEvaluateAlertsDiskBlocksStuck(t *testing.T) {
	rec := &recordingNotifier{}
	var nowMu sync.Mutex
	now := time.Unix(1000, 0)
	p := newAlertTestPool(t, rec, time.Minute, func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	})

	p.mu.Lock()
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

func TestEvaluateAlertsSkipsSingleFailure(t *testing.T) {
	rec := &recordingNotifier{}
	p := newAlertTestPool(t, rec, time.Hour, nil)
	p.recordFailure(context.Canceled)
	p.evaluateAlerts(context.Background())
	if len(rec.codes()) != 0 {
		t.Fatalf("unexpected alert: %v", rec.codes())
	}
}

func TestEvaluateAlertsSkipsDuringDrain(t *testing.T) {
	rec := &recordingNotifier{}
	var nowMu sync.Mutex
	now := time.Unix(1000, 0)
	p := newAlertTestPool(t, rec, time.Minute, func() time.Time {
		nowMu.Lock()
		defer nowMu.Unlock()
		return now
	})
	p.BeginShutdown()
	p.mu.Lock()
	p.drain = true
	p.mu.Unlock()
	nowMu.Lock()
	now = now.Add(time.Hour)
	nowMu.Unlock()
	p.evaluateAlerts(context.Background())
	if len(rec.codes()) != 0 {
		t.Fatalf("unexpected: %v", rec.codes())
	}
}
