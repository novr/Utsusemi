package pool

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/spawn"
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
	var out []registrar.Runner
	for _, runner := range r.runners {
		if prefix == "" || strings.HasPrefix(runner.Name, prefix) {
			out = append(out, runner)
		}
	}
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
	// Override effectivePrefix with bare VMNamePrefix so test VM names don't
	// depend on the hostname of the machine running the tests.
	p.effectivePrefix = cfg.VMNamePrefix
	session, err := p.leases.BeginAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	p.session = session
	return p
}

// newTestPoolWithPrefix creates a pool whose effectivePrefix is set explicitly.
// Use this when testing multi-host scenarios where different prefixes matter.
func newTestPoolWithPrefix(t *testing.T, cfg *config.Config, vmProvider provider.VMProvider, reg registrar.RunnerRegistrar, prefix string) *Pool {
	t.Helper()
	p := New(cfg, target.Target{Type: target.TypeRepo, Owner: "a", Repo: "b"}, vmProvider, reg, slog.Default())
	p.effectivePrefix = prefix
	session, err := p.leases.BeginAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	p.session = session
	return p
}

func TestRecordShortExitAppliesBackoff(t *testing.T) {
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(provider.NewFakeExecutor(), false), noopRegistrar{})

	if err := p.recordShortExit("vm-1", spawn.Result{JobMs: 28_000, TotalMs: 90_000}); err != nil {
		t.Fatalf("recordShortExit: %v", err)
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.shortExits != 1 {
		t.Fatalf("shortExits = %d, want 1", p.shortExits)
	}
	if backoff := time.Until(p.backoffUntil); backoff < 29*time.Second {
		t.Fatalf("backoff = %v, want at least 29s", backoff)
	}
}

func TestRecordShortExitStopsAfterMaxConsecutive(t *testing.T) {
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(provider.NewFakeExecutor(), false), noopRegistrar{})

	var err error
	for i := 0; i < maxConsecutiveShortExits; i++ {
		err = p.recordShortExit("vm-1", spawn.Result{JobMs: 28_000, TotalMs: 90_000})
	}
	if err == nil {
		t.Fatal("expected stop error after max consecutive short exits")
	}
}

func TestHandleSpawnSuccessResetsOnLongJob(t *testing.T) {
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(provider.NewFakeExecutor(), false), noopRegistrar{})

	p.mu.Lock()
	p.shortExits = 3
	p.backoffUntil = time.Now().Add(time.Minute)
	p.mu.Unlock()

	p.handleSpawnSuccess("vm-1", spawn.Result{JobMs: 120_000, TotalMs: 180_000})

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.shortExits != 0 {
		t.Fatalf("shortExits = %d, want 0 after long job", p.shortExits)
	}
	if !p.backoffUntil.IsZero() {
		t.Fatalf("backoffUntil = %v, want zero", p.backoffUntil)
	}
}

func TestHandleSpawnSuccessLeavesCountersForMediumJob(t *testing.T) {
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(provider.NewFakeExecutor(), false), noopRegistrar{})

	p.mu.Lock()
	p.shortExits = 2
	p.backoffUntil = time.Now().Add(time.Minute)
	p.mu.Unlock()

	p.handleSpawnSuccess("vm-1", spawn.Result{JobMs: 45_000, TotalMs: 105_000})

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.shortExits != 2 {
		t.Fatalf("shortExits = %d, want unchanged", p.shortExits)
	}
	if p.backoffUntil.IsZero() {
		t.Fatal("backoffUntil should remain set")
	}
}

func TestHandleSpawnSuccessIgnoresMissingJobTiming(t *testing.T) {
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(provider.NewFakeExecutor(), false), noopRegistrar{})

	p.mu.Lock()
	p.shortExits = 2
	p.mu.Unlock()

	p.handleSpawnSuccess("vm-1", spawn.Result{})

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.shortExits != 2 {
		t.Fatalf("shortExits = %d, want unchanged", p.shortExits)
	}
}

func TestHandleSpawnSuccessStopsAgentAfterMaxShortExits(t *testing.T) {
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(provider.NewFakeExecutor(), false), noopRegistrar{})

	p.mu.Lock()
	p.shortExits = maxConsecutiveShortExits - 1
	p.mu.Unlock()

	p.handleSpawnSuccess("vm-1", spawn.Result{JobMs: 28_000, TotalMs: 90_000})

	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.shutdown {
		t.Fatal("expected shutdown flag")
	}
	if p.fatalErr == nil {
		t.Fatal("expected fatal error")
	}
	select {
	case err := <-p.fatalCh:
		if err == nil {
			t.Fatal("expected fatal error on channel")
		}
	default:
		t.Fatal("expected fatal error on channel")
	}
}

func TestHandleSpawnSuccessRecordsShortExit(t *testing.T) {
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(provider.NewFakeExecutor(), false), noopRegistrar{})

	p.handleSpawnSuccess("vm-1", spawn.Result{JobMs: 28_000, TotalMs: 90_000})

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.shortExits != 1 {
		t.Fatalf("shortExits = %d, want 1", p.shortExits)
	}
}

func TestIsUnclaimedJobExit(t *testing.T) {
	tests := []struct {
		result spawn.Result
		want   bool
	}{
		{spawn.Result{JobMs: 28_000, TotalMs: 90_000}, true},
		{spawn.Result{JobMs: 37_999, TotalMs: 120_000}, true},
		{spawn.Result{JobMs: 21_999, TotalMs: 90_000}, false},
		{spawn.Result{JobMs: 38_000, TotalMs: 120_000}, false},
		{spawn.Result{JobMs: 45_000, TotalMs: 105_000}, false},
		{spawn.Result{JobMs: 120_000, TotalMs: 180_000}, false},
		{spawn.Result{JobMs: 0, TotalMs: 90_000}, false},
	}
	for _, tt := range tests {
		if got := isUnclaimedJobExit(tt.result); got != tt.want {
			t.Errorf("isUnclaimedJobExit(%+v) = %v, want %v", tt.result, got, tt.want)
		}
	}
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

// TestReclaimSkipsOtherHostRunners verifies that when two hosts share the same
// GitHub org/repo with the same vm_name_prefix, reclaim on Host A only removes
// runners whose names start with Host A's effective prefix and leaves Host B's
// runners untouched.
func TestReclaimSkipsOtherHostRunners(t *testing.T) {
	const (
		prefixA = "utsusemi-host-a-"
		prefixB = "utsusemi-host-b-"
	)

	// VM layer: Host A has one stopped VM; Host B's VM is also present on the
	// same tart host (shared NAS scenario), but it is stopped as well.
	exec := provider.NewFakeExecutor()
	exec.VMs[prefixA+"dead"] = false // Host A's stale VM
	exec.VMs[prefixB+"dead"] = false // Host B's stale VM — must NOT be touched

	// Runner layer: both hosts' orphaned runners are visible via the GitHub API.
	reg := &trackingRegistrar{
		runners: []registrar.Runner{
			{ID: 10, Name: prefixA + "dead"},
			{ID: 20, Name: prefixB + "dead"},
		},
	}

	cfg := testPoolConfig(t)
	// Use Host A's effective prefix explicitly.
	p := newTestPoolWithPrefix(t, cfg, provider.NewTartProvider(exec, true), reg, prefixA)

	if err := p.reclaim(context.Background(), false); err != nil {
		t.Fatal(err)
	}

	// Host A's VM must be deleted.
	if _, ok := exec.VMs[prefixA+"dead"]; ok {
		t.Errorf("host-A VM should have been reclaimed")
	}
	// Host B's VM must be untouched.
	if _, ok := exec.VMs[prefixB+"dead"]; !ok {
		t.Errorf("host-B VM should NOT have been reclaimed by host-A's reclaim")
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	// Only runner 10 (Host A's) must be deleted.
	if len(reg.deleted) != 1 || reg.deleted[0] != 10 {
		t.Errorf("expected only runner 10 deleted, got deleted=%v", reg.deleted)
	}
}

// TestDrainAndWaitPurgesResidualVMs is the regression test for #27: VMs that
// outlived the drain window (grace-period VMs from a previous session) must be
// cleaned up by drainAndWait after in-flight goroutines finish.
func TestDrainAndWaitPurgesResidualVMs(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-residual"] = true // running VM under effectivePrefix, no lease

	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, false), noopRegistrar{})

	if err := p.drainAndWait(context.Background()); err != nil {
		t.Fatalf("drainAndWait: %v", err)
	}
	if _, ok := exec.VMs["utsusemi-residual"]; ok {
		t.Fatal("residual VM should have been purged by drainAndWait")
	}
}

// TestDrainAndWaitReturnsErrorOnPurgeFailure verifies that drainAndWait
// propagates a purge error so that utsusemi run exits non-zero when shutdown
// cleanup fails (e.g. GitHub 502 that exhausts all retries).
func TestDrainAndWaitReturnsErrorOnPurgeFailure(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-stuck"] = false // stopped VM, runner delete will fail

	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, false), &failDeleteRegistrar{
		runners: []registrar.Runner{{ID: 1, Name: "utsusemi-stuck"}},
	})

	err := p.drainAndWait(context.Background())
	if err == nil {
		t.Fatal("drainAndWait should return an error when shutdown purge fails")
	}
}
