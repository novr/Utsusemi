package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/notify"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
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

type syncFailProvider struct {
	provider.Stub
}

func (p *syncFailProvider) SyncImage(context.Context, string) error {
	return errors.New("pull failed")
}

type noopRegistrar struct{}

func (noopRegistrar) CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (registrar.JITConfig, error) {
	return registrar.JITConfig{Encoded: "jit", Runner: registrar.Runner{ID: 1, Name: name}}, nil
}
func (noopRegistrar) DeleteRunner(ctx context.Context, tgt target.Target, runnerID int64) error {
	return nil
}
func (noopRegistrar) ListRunners(ctx context.Context, tgt target.Target, prefix string) ([]registrar.Runner, error) {
	return nil, nil
}
func (noopRegistrar) ValidateCredential(ctx context.Context, service, account string) error {
	return nil
}

func TestShouldAlertFatal(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if shouldAlertFatal(ctx, errors.New("x")) {
		t.Fatal("canceled ctx should not alert")
	}
	if shouldAlertFatal(context.Background(), context.Canceled) {
		t.Fatal("canceled err should not alert")
	}
	if !shouldAlertFatal(context.Background(), errors.New("fatal")) {
		t.Fatal("expected alert")
	}
	if shouldAlertFatal(context.Background(), nil) {
		t.Fatal("nil should not alert")
	}
}

func TestRunAlertCause(t *testing.T) {
	fatal := errors.New("short-exit limit")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := runAlertCause(ctx, errors.Join(fatal, context.Canceled), fatal); got != fatal {
		t.Fatalf("fatal during drain: got %v", got)
	}
	if got := runAlertCause(ctx, errors.New("purge failed"), nil); got != nil {
		t.Fatalf("signal+purge should not alert: %v", got)
	}
	joined := errors.Join(errors.New("unauthorized"), context.Canceled)
	if got := runAlertCause(context.Background(), joined, nil); got == nil {
		t.Fatal("live ctx unauthorized should alert")
	}
}

func TestRunAlertsOnSyncImageFailure(t *testing.T) {
	rec := &recordingNotifier{}
	cfg := &config.Config{
		BaseImage:     "img",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
		StateDir:      t.TempDir(),
		VMNamePrefix:  "utsusemi-",
		MinFreeDiskGB: 1,
	}
	ag, err := New(Options{
		Config:    cfg,
		Target:    target.Target{Type: target.TypeOrg, Org: "acme"},
		Provider:  &syncFailProvider{Stub: *provider.NewStub(2)},
		Registrar: noopRegistrar{},
		Notifier:  rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	err = ag.Run(context.Background())
	if err == nil {
		t.Fatal("expected sync error")
	}
	if got := rec.codes(); len(got) != 1 || got[0] != notify.CodeAgentFatal {
		t.Fatalf("codes=%v err=%v", got, err)
	}
}

func TestRunSkipsAlertOnSyncImageCancel(t *testing.T) {
	rec := &recordingNotifier{}
	cfg := &config.Config{
		BaseImage:     "img",
		RunnerVersion: "2.336.0",
		PoolSize:      1,
		StateDir:      t.TempDir(),
		VMNamePrefix:  "utsusemi-",
		MinFreeDiskGB: 1,
	}
	ag, err := New(Options{
		Config:    cfg,
		Target:    target.Target{Type: target.TypeOrg, Org: "acme"},
		Provider:  &syncCancelProvider{Stub: *provider.NewStub(2)},
		Registrar: noopRegistrar{},
		Notifier:  rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = ag.Run(ctx)
	if len(rec.codes()) != 0 {
		t.Fatalf("unexpected alert on sync cancel: %v", rec.codes())
	}
}

type syncCancelProvider struct {
	provider.Stub
}

func (p *syncCancelProvider) SyncImage(ctx context.Context, _ string) error {
	return ctx.Err()
}

func TestRunSkipsAlertOnCancel(t *testing.T) {
	rec := &recordingNotifier{}
	hour := config.Duration(time.Hour)
	cfg := &config.Config{
		BaseImage:              "img",
		RunnerVersion:          "2.336.0",
		PoolSize:               1,
		StateDir:               t.TempDir(),
		VMNamePrefix:           "utsusemi-",
		MinFreeDiskGB:          1,
		PoolCheckInterval:      hour,
		ReconciliationInterval: hour,
		SpawnTimeout:           hour,
		JobTimeout:             hour,
		ReclaimPolicy:          config.ReclaimSoft,
	}
	ag, err := New(Options{
		Config:    cfg,
		Target:    target.Target{Type: target.TypeOrg, Org: "acme"},
		Provider:  provider.NewStub(2),
		Registrar: noopRegistrar{},
		Notifier:  rec,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = ag.Run(ctx)
	if len(rec.codes()) != 0 {
		t.Fatalf("unexpected alert on cancel: %v", rec.codes())
	}
}
