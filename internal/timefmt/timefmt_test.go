package timefmt

import (
	"testing"
	"time"
)

func TestAgeSubMinute(t *testing.T) {
	if got := Age(30 * time.Second); got != "30s" {
		t.Fatalf("got %q", got)
	}
}

func TestExpiresInExpired(t *testing.T) {
	if got := ExpiresIn(-time.Minute); got != "expired" {
		t.Fatalf("got %q", got)
	}
}
