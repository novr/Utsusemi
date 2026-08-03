package pool

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

type failDeleteRegistrar struct {
	runners []registrar.Runner
}

func (r *failDeleteRegistrar) CreateJIT(_ context.Context, _ target.Target, _ []string, name string) (registrar.JITConfig, error) {
	return registrar.JITConfig{Encoded: "jit", Runner: registrar.Runner{ID: 1, Name: name}}, nil
}
func (r *failDeleteRegistrar) DeleteRunner(_ context.Context, _ target.Target, _ int64) error {
	return fmt.Errorf("api 502: github api error")
}
func (r *failDeleteRegistrar) ListRunners(_ context.Context, _ target.Target, _ string) ([]registrar.Runner, error) {
	return r.runners, nil
}
func (r *failDeleteRegistrar) ValidateCredential(_ context.Context, _, _ string) error { return nil }

func TestPurgeAllDryRun(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-a"] = true
	exec.VMs["utsusemi-b"] = false

	reg := &trackingRegistrar{
		runners: []registrar.Runner{
			{ID: 1, Name: "utsusemi-a"},
			{ID: 2, Name: "utsusemi-b"},
		},
	}
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true, nil), reg)

	vms, runnerIDs, err := p.PurgeAll(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if len(vms) != 2 || len(runnerIDs) != 2 {
		t.Fatalf("vms=%d runnerIDs=%d", len(vms), len(runnerIDs))
	}
	if len(exec.VMs) != 2 {
		t.Fatalf("vms should remain during dry-run: %#v", exec.VMs)
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.deleted) != 0 {
		t.Fatalf("deleted=%v", reg.deleted)
	}
}

func TestPurgeAllDeletesManagedResources(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-a"] = true
	exec.VMs["utsusemi-b"] = false

	reg := &trackingRegistrar{
		runners: []registrar.Runner{
			{ID: 1, Name: "utsusemi-a"},
			{ID: 2, Name: "utsusemi-b"},
		},
	}
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true, nil), reg)
	if err := p.leases.WriteLease(p.session, lease.Lease{VMName: "utsusemi-a", RunnerID: 1}); err != nil {
		t.Fatal(err)
	}

	vms, runnerIDs, err := p.PurgeAll(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(vms) != 2 || len(runnerIDs) != 2 {
		t.Fatalf("vms=%d runnerIDs=%d", len(vms), len(runnerIDs))
	}
	if len(exec.VMs) != 0 {
		t.Fatalf("expected all vms deleted: %#v", exec.VMs)
	}

	reg.mu.Lock()
	defer reg.mu.Unlock()
	if len(reg.deleted) != 2 {
		t.Fatalf("deleted=%v", reg.deleted)
	}

	leaseMap, err := p.leases.LeaseMap()
	if err != nil {
		t.Fatal(err)
	}
	if len(leaseMap) != 0 {
		t.Fatalf("leases=%v", leaseMap)
	}
}

func TestPurgeAllReturnsErrorOnRunnerDeleteFailure(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-a"] = false

	reg := &failDeleteRegistrar{
		runners: []registrar.Runner{{ID: 1, Name: "utsusemi-a"}},
	}
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true, nil), reg)

	vms, runnerIDs, err := p.PurgeAll(context.Background(), false)
	if err == nil {
		t.Fatal("expected error when runner deletion fails, got nil")
	}
	if len(vms) != 1 {
		t.Fatalf("deleted vm count = %d, want 1", len(vms))
	}
	if len(runnerIDs) != 0 {
		t.Fatalf("deleted runner count = %d, want 0", len(runnerIDs))
	}
	if len(exec.VMs) != 0 {
		t.Fatalf("expected vm to be deleted: %#v", exec.VMs)
	}
}

func TestPurgeAllReturnsErrorOnStopFailure(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.VMs["utsusemi-a"] = true
	exec.FailNext["tart stop utsusemi-a"] = fmt.Errorf("tart stop failed")

	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true, nil), noopRegistrar{})

	vms, _, err := p.PurgeAll(context.Background(), false)
	if err == nil {
		t.Fatal("expected error when vm stop fails, got nil")
	}
	if len(vms) != 0 {
		t.Fatalf("deleted vm count = %d, want 0", len(vms))
	}
	if _, ok := exec.VMs["utsusemi-a"]; !ok {
		t.Fatal("vm should remain when stop fails")
	}
}

func TestPurgeAllReturnsErrorOnClearLeasesFailure(t *testing.T) {
	exec := provider.NewFakeExecutor()
	cfg := testPoolConfig(t)
	p := newTestPool(t, cfg, provider.NewTartProvider(exec, true, nil), noopRegistrar{})
	if err := p.leases.WriteLease(p.session, lease.Lease{VMName: "utsusemi-a", RunnerID: 1}); err != nil {
		t.Fatal(err)
	}

	leaseDir := filepath.Join(cfg.StateDir, "leases")
	if err := os.Chmod(leaseDir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(leaseDir, 0o755) })

	_, _, err := p.PurgeAll(context.Background(), false)
	if err == nil {
		t.Fatal("expected error when clearing leases fails, got nil")
	}
}
