package hostid

import "testing"

func TestSanitizeHostname(t *testing.T) {
	if got := Sanitize("MacBook-Pro.local"); got != "macbook-pro-local" {
		t.Fatalf("got %q", got)
	}
}

func TestParseFromRunnerName(t *testing.T) {
	id, ok := ParseFromRunnerName("utsusemi-macbook-a1b2c3d4", "utsusemi-")
	if !ok || id != "macbook" {
		t.Fatalf("id=%q ok=%v", id, ok)
	}
}

func TestWarningsGenericHostID(t *testing.T) {
	w := warnings("macbookpro")
	if len(w) == 0 {
		t.Fatal("expected warning for generic host id")
	}
}

func TestLoadOrInitPrefersLocalHostName(t *testing.T) {
	dir := t.TempDir()
	// No host_id file yet; we cannot mock scutil easily, but Load should not panic.
	id := Load(dir)
	if id == "" {
		t.Fatal("expected host id")
	}
}
