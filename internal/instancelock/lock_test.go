package instancelock

import (
	"path/filepath"
	"testing"
)

func TestHeld(t *testing.T) {
	path := filepath.Join(t.TempDir(), "utsusemi.lock")
	if Held(path) {
		t.Fatal("expected lock not held")
	}
	lock, err := Acquire(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Release() }()
	if !Held(path) {
		t.Fatal("expected lock held")
	}
}
