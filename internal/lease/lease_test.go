package lease

import (
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
)

func TestIsStaleDifferentAgent(t *testing.T) {
	session := &AgentSession{ID: "current", PID: 1000}
	old := &Lease{VMName: "utsusemi-abcd", AgentID: "previous", PID: 1000}
	if !IsStale(old, session) {
		t.Fatal("expected stale lease")
	}
}

func TestShouldReclaimRunningGrace(t *testing.T) {
	session := &AgentSession{ID: "current", PID: 1000}
	old := &Lease{
		VMName:    "utsusemi-abcd",
		AgentID:   "previous",
		PID:       1,
		StartedAt: time.Now().UTC().Add(-30 * time.Minute),
	}
	now := time.Now().UTC()
	if !ShouldReclaimRunning(old, session, config.ReclaimGrace, 15*time.Minute, now) {
		t.Fatal("expected grace reclaim")
	}
	if ShouldReclaimRunning(old, session, config.ReclaimSoft, 15*time.Minute, now) {
		t.Fatal("soft should not reclaim running vm")
	}
}

func TestLoadAgentSessionMissing(t *testing.T) {
	reg := NewRegistry(t.TempDir())
	session, err := reg.LoadAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	if session != nil {
		t.Fatalf("session=%+v", session)
	}
}

func TestRegistryWriteRemoveLease(t *testing.T) {
	dir := t.TempDir()
	reg := NewRegistry(dir)
	session, err := reg.BeginAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.WriteLease(session, Lease{VMName: "utsusemi-test", RunnerID: 7}); err != nil {
		t.Fatal(err)
	}
	leases, err := reg.ListLeases()
	if err != nil || len(leases) != 1 {
		t.Fatalf("leases=%v err=%v", leases, err)
	}
	if err := reg.RemoveLease("utsusemi-test"); err != nil {
		t.Fatal(err)
	}
	leases, err = reg.ListLeases()
	if err != nil || len(leases) != 0 {
		t.Fatalf("leases=%v err=%v", leases, err)
	}
}
