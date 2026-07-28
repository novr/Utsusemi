package version

import "testing"

func TestStringDefault(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	Version = ""
	if got := String(); got != "dev" {
		t.Fatalf("String() = %q, want dev", got)
	}

	Version = "0.1.0"
	if got := String(); got != "0.1.0" {
		t.Fatalf("String() = %q, want 0.1.0", got)
	}
}
