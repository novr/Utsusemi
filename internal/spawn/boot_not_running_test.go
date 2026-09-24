package spawn

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestRecordAndLoadBootNotRunning(t *testing.T) {
	dir := t.TempDir()
	if _, ok := LoadBootNotRunning(dir); ok {
		t.Fatal("expected missing stats")
	}
	if err := RecordBootNotRunning(dir, "vm-a"); err != nil {
		t.Fatal(err)
	}
	stats, ok := LoadBootNotRunning(dir)
	if !ok || stats.Count != 1 || stats.LastVM != "vm-a" {
		t.Fatalf("stats=%+v ok=%v", stats, ok)
	}
	if stats.LastAt.IsZero() {
		t.Fatal("last_at unset")
	}
	prev := stats.LastAt
	time.Sleep(2 * time.Millisecond)
	if err := RecordBootNotRunning(dir, "vm-b"); err != nil {
		t.Fatal(err)
	}
	stats, ok = LoadBootNotRunning(dir)
	if !ok || stats.Count != 2 || stats.LastVM != "vm-b" {
		t.Fatalf("stats=%+v ok=%v", stats, ok)
	}
	if !stats.LastAt.After(prev) {
		t.Fatalf("last_at not updated: %v -> %v", prev, stats.LastAt)
	}
	if _, err := os.Stat(filepath.Join(dir, bootNotRunningFile)); err != nil {
		t.Fatal(err)
	}
}

func TestRecordBootNotRunningConcurrent(t *testing.T) {
	dir := t.TempDir()
	const n = 8
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(i int) {
			defer wg.Done()
			if err := RecordBootNotRunning(dir, fmt.Sprintf("vm-%d", i)); err != nil {
				t.Errorf("record: %v", err)
			}
		}(i)
	}
	wg.Wait()
	stats, ok := LoadBootNotRunning(dir)
	if !ok || stats.Count != n {
		t.Fatalf("stats=%+v ok=%v want count=%d", stats, ok, n)
	}
}
