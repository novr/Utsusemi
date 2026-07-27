package pool

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

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
func (noopRegistrar) ValidateCredential(ctx context.Context, service, account string) error { return nil }

type trackingRegistrar struct {
	mu      sync.Mutex
	runners []registrar.Runner
	deleted []int64
}

func (r *trackingRegistrar) CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (registrar.JITConfig, error) {
	return registrar.JITConfig{Encoded: "jit", Runner: registrar.Runner{ID: 1, Name: name}}, nil
}
func (r *trackingRegistrar) DeleteRunner(ctx context.Context, tgt target.Target, runnerID int64) error {
	r.mu.Lock()
	r.deleted = append(r.deleted, runnerID)
	r.mu.Unlock()
	return nil
}
func (r *trackingRegistrar) ListRunners(ctx context.Context, tgt target.Target, prefix string) ([]registrar.Runner, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]registrar.Runner, len(r.runners))
	copy(out, r.runners)
	return out, nil
}
func (r *trackingRegistrar) ValidateCredential(ctx context.Context, service, account string) error {
	return nil
}

func testPoolConfig(t *testing.T) *config.Config {
	t.Helper()
	return &config.Config{
		BaseImage:              "image",
		RunnerVersion:          "2.336.0",
		Labels:                 []string{"self-hosted"},
		PoolSize:               1,
		PoolCheckInterval:      config.Duration(20 * time.Millisecond),
		ReconciliationInterval: config.Duration(time.Hour),
		SpawnTimeout:           config.Duration(time.Second),
		JobTimeout:             config.Duration(time.Second),
		MinFreeDiskGB:          1,
		VMNamePrefix:           "utsusemi-",
		StateDir:               t.TempDir(),
		ReclaimPolicy:          config.ReclaimSoft,
		ReclaimGrace:           config.Duration(15 * time.Minute),
	}
}

func newTestPool(t *testing.T, cfg *config.Config, vmProvider provider.VMProvider, reg registrar.RunnerRegistrar) *Pool {
	t.Helper()
	p := New(cfg, target.Target{Type: target.TypeRepo, Owner: "a", Repo: "b"}, vmProvider, reg, slog.Default())
	session, err := p.leases.BeginAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	p.session = session
	return p
}

func TestPoolBackoffOnFailure(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.FailClone = context.Canceled

	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true), noopRegistrar{})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failures == 0 {
		t.Fatal("expected failures")
	}
}

func TestReconcileSkipsRunningVMs(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-dead"] = false
	exec.VMs["utsusemi-live"] = true

	reg := &trackingRegistrar{
		runners: []registrar.Runner{
			{ID: 1, Name: "utsusemi-dead"},
			{ID: 2, Name: "utsusemi-live"},
		},
	}
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true), reg)

	if err := p.reclaim(context.Background(), false); err != nil {
		t.Fatal(err)
	}

	if _, ok := exec.VMs["utsusemi-live"]; !ok {
		t.Fatal("running vm should remain")
	}
	if _, ok := exec.VMs["utsusemi-dead"]; ok {
		t.Fatal("stopped vm should be deleted")
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.deleted) != 1 || reg.deleted[0] != 1 {
		t.Fatalf("deleted=%v", reg.deleted)
	}
}

func TestReconcileSkipsInFlightVMs(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-busy"] = false

	reg := &trackingRegistrar{
		runners: []registrar.Runner{{ID: 9, Name: "utsusemi-busy"}},
	}
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true), reg)
	p.inFlightVMs["utsusemi-busy"] = struct{}{}

	if err := p.reclaim(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if _, ok := exec.VMs["utsusemi-busy"]; !ok {
		t.Fatal("in-flight vm should remain")
	}
	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.deleted) != 0 {
		t.Fatalf("deleted=%v", reg.deleted)
	}
}

func TestStartupHardReclaimDeletesStaleRunningVM(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-old"] = true

	cfg := testPoolConfig(t)
	cfg.ReclaimPolicy = config.ReclaimHard
	p := newTestPool(t, cfg, provider.NewTartProvider(exec, true), noopRegistrar{})

	staleSession := &lease.AgentSession{ID: "old-agent", PID: 1, StartedAt: time.Now().UTC().Add(-time.Hour)}
	if err := p.leases.WriteLease(staleSession, lease.Lease{VMName: "utsusemi-old", RunnerID: 1}); err != nil {
		t.Fatal(err)
	}

	if err := p.startupReclaim(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, ok := exec.VMs["utsusemi-old"]; ok {
		t.Fatal("stale running vm should be deleted")
	}
}
