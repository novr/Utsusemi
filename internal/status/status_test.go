package status

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/target"
)

type fakeProvider struct {
	vms    []provider.VM
	freeGB float64
}

func (f *fakeProvider) Available() error { return nil }
func (f *fakeProvider) SyncImage(context.Context, string) error {
	return nil
}
func (f *fakeProvider) Clone(context.Context, string, string) error { return nil }
func (f *fakeProvider) Start(context.Context, string) error         { return nil }
func (f *fakeProvider) ExecStdin(context.Context, string, string, []string, []byte, map[string]string) error {
	return nil
}
func (f *fakeProvider) Stop(context.Context, string) error   { return nil }
func (f *fakeProvider) Delete(context.Context, string) error { return nil }
func (f *fakeProvider) HealthCheck(context.Context, string) error {
	return nil
}
func (f *fakeProvider) IsRunning(context.Context, string) (bool, error) {
	return false, nil
}
func (f *fakeProvider) Capabilities() provider.Capabilities {
	return provider.Capabilities{MaxConcurrent: 2}
}
func (f *fakeProvider) ListManaged(_ context.Context, prefix string) ([]provider.VM, error) {
	if prefix == "" {
		return f.vms, nil
	}
	out := make([]provider.VM, 0, len(f.vms))
	for _, vm := range f.vms {
		if strings.HasPrefix(vm.Name, prefix) {
			out = append(out, vm)
		}
	}
	return out, nil
}
func (f *fakeProvider) FreeDiskGB(context.Context) (float64, error) {
	return f.freeGB, nil
}

func testConfig(stateDir string) *config.Config {
	cfg := &config.Config{
		Target:        config.TargetYAML("my-org", "", 1),
		Registration:  config.Registration{Mode: config.ModeGitHubPAT},
		PoolSize:      4,
		MinFreeDiskGB: 50,
		VMNamePrefix:  "utsusemi-",
		StateDir:      stateDir,
	}
	config.ApplyDefaults(cfg)
	return cfg
}

func testTarget() target.Target {
	return target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}
}

func testInput(cfg *config.Config, p provider.VMProvider) Input {
	return Input{
		Cfg:      cfg,
		Target:   testTarget(),
		Provider: p,
		Store:    keychain.NewMemoryStore(),
	}
}

func TestCollectNoAgent(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	report, err := Collect(context.Background(), testInput(cfg, &fakeProvider{
		vms:    []provider.VM{{Name: "utsusemi-a", Running: false}},
		freeGB: 100,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if report.Agent.State != AgentStopped {
		t.Fatalf("agent state=%s", report.Agent.State)
	}
	if len(report.Jobs) != 0 {
		t.Fatalf("jobs=%v", report.Jobs)
	}
	if report.VMs.Total != 1 {
		t.Fatalf("vms=%+v", report.VMs)
	}
}

func TestCollectRunningWithLeases(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "utsusemi.lock")
	lock, err := instancelock.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	reg := lease.NewRegistry(dir)
	session, err := reg.BeginAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"utsusemi-a1b2", "utsusemi-c3d4"} {
		if err := reg.WriteLease(session, lease.Lease{
			VMName:    name,
			RunnerID:  int64(42 + i),
			StartedAt: time.Now().UTC().Add(-10 * time.Minute),
		}); err != nil {
			t.Fatal(err)
		}
	}

	cfg := testConfig(dir)
	report, err := Collect(context.Background(), testInput(cfg, &fakeProvider{freeGB: 100}))
	if err != nil {
		t.Fatal(err)
	}
	if report.Agent.State != AgentRunning {
		t.Fatalf("agent state=%s", report.Agent.State)
	}
	if len(report.Jobs) != 2 {
		t.Fatalf("jobs=%v", report.Jobs)
	}
}

func TestCollectStaleAgentWithLease(t *testing.T) {
	dir := t.TempDir()
	reg := lease.NewRegistry(dir)
	session := &lease.AgentSession{
		ID:        "deadbeef",
		PID:       99999999,
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}
	data, err := json.Marshal(session)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := reg.WriteLease(session, lease.Lease{
		VMName:    "utsusemi-old",
		RunnerID:  99,
		StartedAt: time.Now().UTC().Add(-2 * time.Hour),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(dir)
	report, err := Collect(context.Background(), testInput(cfg, &fakeProvider{
		vms:    []provider.VM{{Name: "utsusemi-old", Running: true}},
		freeGB: 100,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if report.Agent.State != AgentStale {
		t.Fatalf("agent state=%s", report.Agent.State)
	}
	if len(report.Jobs) != 1 || !report.Jobs[0].Stale {
		t.Fatalf("jobs=%v", report.Jobs)
	}
	if len(report.Warming) != 0 {
		t.Fatalf("warming=%v", report.Warming)
	}
}

func TestCollectStoppedWithLivePID(t *testing.T) {
	dir := t.TempDir()
	reg := lease.NewRegistry(dir)
	if _, err := reg.BeginAgentSession(); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(dir)
	report, err := Collect(context.Background(), testInput(cfg, &fakeProvider{freeGB: 100}))
	if err != nil {
		t.Fatal(err)
	}
	if report.Agent.State != AgentStopped {
		t.Fatalf("agent state=%s", report.Agent.State)
	}
	if report.Agent.Warning == "" {
		t.Fatal("expected warning for leftover agent.json")
	}
}

func TestCollectWarmingVM(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "utsusemi.lock")
	lock, err := instancelock.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	reg := lease.NewRegistry(dir)
	session, err := reg.BeginAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.WriteLease(session, lease.Lease{
		VMName:    "utsusemi-busy",
		RunnerID:  1,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(dir)
	report, err := Collect(context.Background(), testInput(cfg, &fakeProvider{
		vms: []provider.VM{
			{Name: "utsusemi-busy", Running: true},
			{Name: "utsusemi-warm", Running: true},
		},
		freeGB: 100,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warming) != 1 || report.Warming[0] != "utsusemi-warm" {
		t.Fatalf("warming=%v", report.Warming)
	}
}

func TestCollectOrphanVMNotWarming(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "utsusemi.lock")
	lock, err := instancelock.Acquire(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()

	reg := lease.NewRegistry(dir)
	if _, err := reg.BeginAgentSession(); err != nil {
		t.Fatal(err)
	}
	oldSession := &lease.AgentSession{ID: "previous-agent", PID: 1, StartedAt: time.Now().UTC()}
	if err := reg.WriteLease(oldSession, lease.Lease{
		VMName:    "utsusemi-orphan",
		RunnerID:  9,
		StartedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(dir)
	report, err := Collect(context.Background(), testInput(cfg, &fakeProvider{
		vms:    []provider.VM{{Name: "utsusemi-orphan", Running: true}},
		freeGB: 100,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warming) != 0 {
		t.Fatalf("warming=%v, want orphan lease VM excluded", report.Warming)
	}
}

func TestCollectHostedCredentialMissing(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	cfg.Registration.Mode = config.ModeHostedApp
	report, err := Collect(context.Background(), testInput(cfg, &fakeProvider{freeGB: 100}))
	if err != nil {
		t.Fatal(err)
	}
	if report.Credential.Present {
		t.Fatalf("credential=%+v", report.Credential)
	}
}

func TestCollectLowDisk(t *testing.T) {
	dir := t.TempDir()
	cfg := testConfig(dir)
	report, err := Collect(context.Background(), testInput(cfg, &fakeProvider{freeGB: 10}))
	if err != nil {
		t.Fatal(err)
	}
	if report.Health.Status != "low disk" {
		t.Fatalf("health=%+v", report.Health)
	}
}
