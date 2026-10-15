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
