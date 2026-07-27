package logging

import "testing"

func TestRedactPAT(t *testing.T) {
	input := "token ghp_abcdefghijklmnopqrstuvwxyz1234567890"
	out := redact(input)
	if out == input {
		t.Fatalf("expected redaction, got %q", out)
	}
}

func TestRedactJWT(t *testing.T) {
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U"
	out := redact(input)
	if out == input {
		t.Fatalf("expected redaction, got %q", out)
	}
}
