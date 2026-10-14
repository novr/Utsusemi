package listing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

type fakeProvider struct {
	vms []provider.VM
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
func (f *fakeProvider) ListManaged(context.Context, string) ([]provider.VM, error) {
	return f.vms, nil
}
func (f *fakeProvider) FreeDiskGB(context.Context) (float64, error) {
	return 0, nil
}

type fakeRegistrar struct {
	runners []registrar.Runner
}

func (f *fakeRegistrar) CreateJIT(context.Context, target.Target, []string, string) (registrar.JITConfig, error) {
	return registrar.JITConfig{}, nil
}
func (f *fakeRegistrar) DeleteRunner(context.Context, target.Target, int64) error {
	return nil
}
func (f *fakeRegistrar) ListRunners(context.Context, target.Target, string) ([]registrar.Runner, error) {
	return f.runners, nil
}
func (f *fakeRegistrar) ValidateCredential(context.Context, string, string) error {
	return nil
}

func TestCollectScopeAllJSON(t *testing.T) {
	cfg := &config.Config{VMNamePrefix: "utsusemi-"}
	config.ApplyDefaults(cfg)
	dir := t.TempDir()
	reg := lease.NewRegistry(dir)
	session, err := reg.BeginAgentSession()
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.WriteLease(session, lease.Lease{
		VMName:        "utsusemi-a",
		RunnerID:      1,
		BaseImage:     "ghcr.io/example/image:1",
		RunnerVersion: "2.337.0",
	}); err != nil {
		t.Fatal(err)
	}

	report, err := Collect(context.Background(), Input{
		Cfg:       cfg,
		Target:    target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1},
		Provider:  &fakeProvider{vms: []provider.VM{{Name: "utsusemi-a", Running: true}}},
		Registrar: &fakeRegistrar{runners: []registrar.Runner{{ID: 1, Name: "utsusemi-a"}}},
		Leases:    reg,
		Scope:     ScopeAll,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.VMs[0].BaseImage != "ghcr.io/example/image:1" || report.VMs[0].RunnerVersion != "2.337.0" {
		t.Fatalf("vm meta=%+v", report.VMs[0])
	}
	if report.Runners[0].BaseImage != "ghcr.io/example/image:1" || report.Runners[0].RunnerVersion != "2.337.0" {
		t.Fatalf("runner meta=%+v", report.Runners[0])
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, key := range []string{`"vms"`, `"runners"`, `"base_image"`, `"runner_version"`} {
		if !strings.Contains(body, key) {
			t.Fatalf("json missing %s: %s", key, body)
		}
	}
}

func TestCollectWithoutLeaseOmitsMeta(t *testing.T) {
	cfg := &config.Config{VMNamePrefix: "utsusemi-"}
	config.ApplyDefaults(cfg)
	report, err := Collect(context.Background(), Input{
		Cfg:       cfg,
		Target:    target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1},
		Provider:  &fakeProvider{vms: []provider.VM{{Name: "utsusemi-orphan", Running: false}}},
		Registrar: &fakeRegistrar{},
		Scope:     ScopeVMs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.VMs[0].BaseImage != "" || report.VMs[0].RunnerVersion != "" {
		t.Fatalf("expected empty meta, got %+v", report.VMs[0])
	}
}

func TestCollectRunnersOnlyOmitsVMs(t *testing.T) {
	cfg := &config.Config{VMNamePrefix: "utsusemi-"}
	config.ApplyDefaults(cfg)
	report, err := Collect(context.Background(), Input{
		Cfg:       cfg,
		Target:    target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1},
		Provider:  &fakeProvider{},
		Registrar: &fakeRegistrar{},
		Scope:     ScopeRunners,
	})
	if err != nil {
		t.Fatal(err)
	}
	if report.VMs != nil {
		t.Fatalf("vms=%v", report.VMs)
	}
	out := FormatText(report)
	if strings.Contains(out, "vms:") {
		t.Fatalf("unexpected vms section: %s", out)
	}
}

func TestFormatTextEmpty(t *testing.T) {
	out := FormatText(Report{
		VMs:     []VM{},
		Runners: []Runner{},
	})
	if !strings.Contains(out, "vms:\n  (none)") || !strings.Contains(out, "runners:\n  (none)") {
		t.Fatalf("output=%q", out)
	}
}

func TestFormatTextIncludesMeta(t *testing.T) {
	out := FormatText(Report{
		VMs: []VM{{
			Name:          "utsusemi-a",
			Running:       true,
			BaseImage:     "ghcr.io/example/image:1",
			RunnerVersion: "2.337.0",
		}},
	})
	if !strings.Contains(out, "utsusemi-a (running) base_image=ghcr.io/example/image:1 runner_version=2.337.0") {
		t.Fatalf("output=%q", out)
	}
}
