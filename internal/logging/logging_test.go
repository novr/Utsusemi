package logging

import (
	"strings"
	"testing"
)

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

func TestRedactRefreshTokenJSON(t *testing.T) {
	input := `{"v":1,"host_jwt":"eyJ.a.b","refresh_token":"r1.secret"}`
	out := redact(input)
	if strings.Contains(out, "r1.secret") {
		t.Fatalf("expected redaction, got %q", out)
	}
}

func TestRedactHostJWTFieldJSON(t *testing.T) {
	input := `{"v":1,"host_jwt":"eyJhbGciOiJFUzI1NiJ9.payload.sig","refresh_token":"[REDACTED]"}`
	out := redact(input)
	if strings.Contains(out, "payload.sig") {
		t.Fatalf("expected redaction, got %q", out)
	}
}
