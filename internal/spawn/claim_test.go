package spawn

import (
	"testing"
	"time"
)

func TestClaimScannerDetectsRunningJob(t *testing.T) {
	var c claimScanner
	if _, err := c.Write([]byte("2026-10-14 04:00:49Z: Running job: precheck\n")); err != nil {
		t.Fatal(err)
	}
	if !c.Claimed() {
		t.Fatal("expected claimed")
	}
}

func TestClaimScannerDetectsCompleted(t *testing.T) {
	var c claimScanner
	if _, err := c.Write([]byte("Job precheck completed with result: Succeeded\n")); err != nil {
		t.Fatal(err)
	}
	if !c.Claimed() {
		t.Fatal("expected claimed")
	}
}

func TestClaimScannerIgnoresUnrelated(t *testing.T) {
	var c claimScanner
	if _, err := c.Write([]byte("Listening for Jobs\nExiting runner...\n")); err != nil {
		t.Fatal(err)
	}
	if c.Claimed() {
		t.Fatal("expected unclaimed")
	}
	if !c.Listening() {
		t.Fatal("expected listening")
	}
}

func TestClaimScannerListeningCallbackOnce(t *testing.T) {
	var n int
	c := claimScanner{onIdleReady: func() { n++ }}
	if _, err := c.Write([]byte("… Listening for Jobs\nListening for Jobs\n")); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("onIdleReady calls=%d, want 1", n)
	}
	if !c.Listening() {
		t.Fatal("expected listening")
	}
	if c.Claimed() {
		t.Fatal("Listening must not imply claimed")
	}
}

func TestClaimScannerClaimedFiresIdleReady(t *testing.T) {
	var n int
	c := claimScanner{onIdleReady: func() { n++ }}
	if _, err := c.Write([]byte("Running job: precheck\n")); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("onIdleReady calls=%d, want 1", n)
	}
	if !c.Claimed() {
		t.Fatal("expected claimed")
	}
	if c.Listening() {
		t.Fatal("claim without Listening should not set listening")
	}
}

func TestClaimScannerFlushPartialLine(t *testing.T) {
	var c claimScanner
	if _, err := c.Write([]byte("Running job: review")); err != nil {
		t.Fatal(err)
	}
	if c.Claimed() {
		t.Fatal("partial line should wait for flush")
	}
	if err := c.Flush(); err != nil {
		t.Fatal(err)
	}
	if !c.Claimed() {
		t.Fatal("expected claimed after flush")
	}
}

func TestClaimScannerConnectError(t *testing.T) {
	now := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := claimScanner{now: func() time.Time { return now }}
	if _, err := c.Write([]byte("2026-01-02 03:04:05Z: Runner connect error: nodename nor servname provided, or not known (proxy.example:8080). Retrying until reconnected.\n")); err != nil {
		t.Fatal(err)
	}
	since := c.ConnectErrorSince()
	if since != now {
		t.Fatalf("since=%v want %v", since, now)
	}
	later := now.Add(time.Minute)
	c.now = func() time.Time { return later }
	if _, err := c.Write([]byte("another Runner connect error: ignored\n")); err != nil {
		t.Fatal(err)
	}
	if c.ConnectErrorSince() != now {
		t.Fatal("first connect error timestamp must stick")
	}
	if _, err := c.Write([]byte("Runner reconnected.\n")); err != nil {
		t.Fatal(err)
	}
	if !c.ConnectErrorSince().IsZero() {
		t.Fatal("reconnected clears connect error")
	}
}

func TestClaimScannerConnectErrorClearedByListening(t *testing.T) {
	c := claimScanner{now: time.Now}
	if _, err := c.Write([]byte("Runner connect error: boom\nListening for Jobs\n")); err != nil {
		t.Fatal(err)
	}
	if !c.Listening() {
		t.Fatal("expected listening")
	}
	if !c.ConnectErrorSince().IsZero() {
		t.Fatal("Listening clears connect error")
	}
}
