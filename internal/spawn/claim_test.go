package spawn

import (
	"testing"
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
