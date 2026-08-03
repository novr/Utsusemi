package spawn

import (
	"testing"
	"time"
)

func TestSaveLoadLastSpawn(t *testing.T) {
	dir := t.TempDir()
	at := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	want := LastSpawn{
		At:            at,
		VMName:        "utsusemi-test-a1b2c3d4",
		RunnerVersion: "2.336.0",
		CloneMs:       1000,
		BootMs:        2000,
		RegisterMs:    500,
		JobMs:         3000,
		Success:       true,
	}
	if err := SaveLastSpawn(dir, want); err != nil {
		t.Fatal(err)
	}
	got, ok := LoadLastSpawn(dir)
	if !ok {
		t.Fatal("expected file")
	}
	if got.RunnerVersion != want.RunnerVersion || got.CloneMs != want.CloneMs {
		t.Fatalf("got %+v", got)
	}
	if got.ColdStartMs != 3500 || got.TotalMs != 6500 {
		t.Fatalf("cold_start=%d total=%d", got.ColdStartMs, got.TotalMs)
	}
}

func TestSaveLastSpawnSkipsFailed(t *testing.T) {
	dir := t.TempDir()
	if err := SaveLastSpawn(dir, LastSpawn{Success: false, CloneMs: 100}); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadLastSpawn(dir); ok {
		t.Fatal("failed spawn should not be persisted")
	}
}

func TestRunnerVersionSnapshot(t *testing.T) {
	dir := t.TempDir()
	snap := LoadRunnerVersionSnapshot("2.336.0", dir)
	if snap.HasMetrics || snap.Mismatch() {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	if err := SaveLastSpawn(dir, LastSpawn{
		RunnerVersion: "2.335.0",
		CloneMs:       1,
		Success:       true,
	}); err != nil {
		t.Fatal(err)
	}
	snap = LoadRunnerVersionSnapshot("2.336.0", dir)
	if !snap.HasMetrics || !snap.Mismatch() {
		t.Fatalf("expected mismatch: %+v", snap)
	}
}
