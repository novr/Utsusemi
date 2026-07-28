package pool

import (
	"context"
	"testing"

	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
)

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
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true), reg)

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
	p := newTestPool(t, testPoolConfig(t), provider.NewTartProvider(exec, true), reg)
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
