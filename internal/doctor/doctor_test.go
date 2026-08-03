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
	var checks []Check
	add := func(name string, status Status, msg string) {
		checks = append(checks, Check{Name: name, Status: status, Message: msg})
	}
	checkRunnerVersion(&config.Config{RunnerVersion: "2.336.0", StateDir: dir}, add)
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestCheckMountsMissingPath(t *testing.T) {
	var checks []Check
	add := func(name string, status Status, msg string) {
		checks = append(checks, Check{Name: name, Status: status, Message: msg})
	}
	checkMounts(&config.Config{Mounts: []string{"/nonexistent-utsusemi-mount"}}, add)
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestCheckMountsOmitsWhenUnset(t *testing.T) {
	var checks []Check
	add := func(name string, status Status, msg string) {
		checks = append(checks, Check{Name: name, Status: status, Message: msg})
	}
	checkMounts(&config.Config{}, add)
	if len(checks) != 0 {
		t.Fatalf("checks=%+v", checks)
	}
}
