package doctor

import (
	"context"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/spawn"
)

func TestCollectFailsWithoutProvider(t *testing.T) {
	r := Collect(context.Background(), Input{Cfg: &config.Config{}})
	if FailedCount(r) == 0 {
		t.Fatal("expected failure")
	}
}

func TestCheckRunnerVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := spawn.SaveLastSpawn(dir, spawn.LastSpawn{
		At:            time.Now().UTC(),
		RunnerVersion: "2.335.0",
		CloneMs:       1,
		Success:       true,
	}); err != nil {
		t.Fatal(err)
	}
	checks := recordChecks(func(add checkFn) {
		checkRunnerVersion(&config.Config{RunnerVersion: "2.336.0", StateDir: dir}, add)
	})
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestCheckMounts(t *testing.T) {
	existing := t.TempDir()
	tests := []struct {
		name       string
		cfg        *config.Config
		wantChecks int
		wantStatus Status
	}{
		{name: "unset", cfg: &config.Config{}, wantChecks: 0},
		{name: "missing", cfg: &config.Config{Mounts: []string{"/nonexistent-utsusemi-mount"}}, wantChecks: 1, wantStatus: StatusWarn},
		{name: "ok", cfg: &config.Config{Mounts: []string{existing}}, wantChecks: 1, wantStatus: StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checks := recordChecks(func(add checkFn) {
				checkMounts(tc.cfg, add)
			})
			if len(checks) != tc.wantChecks {
				t.Fatalf("checks=%+v", checks)
			}
			if tc.wantChecks > 0 && checks[0].Status != tc.wantStatus {
				t.Fatalf("checks=%+v", checks)
			}
		})
	}
}

type checkFn func(string, Status, string)

func recordChecks(fn func(checkFn)) []Check {
	var checks []Check
	fn(func(name string, status Status, msg string) {
		checks = append(checks, Check{Name: name, Status: status, Message: msg})
	})
	return checks
}
