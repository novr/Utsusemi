package logging

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewWritesAgentLog(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agent.log")
	logger, err := New(Options{LogFile: path})
	if err != nil {
		t.Fatal(err)
	}
	logger.Info("hello agent")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.Contains(body, "hello agent") {
		t.Fatalf("log body: %s", body)
	}
	if !strings.Contains(body, `"level":"INFO"`) {
		t.Fatalf("expected JSON log line: %s", body)
	}
}

func TestNewStdoutOnlyWithoutFile(t *testing.T) {
	dir := t.TempDir()
	if _, err := New(Options{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "agent.log")); !os.IsNotExist(err) {
		t.Fatalf("expected no agent log file, stat err=%v", err)
	}
}

func TestResolveLogFileDefault(t *testing.T) {
	path, err := ResolveLogFile("", "/tmp/state")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/tmp/state/agent.log" {
		t.Fatalf("got %q", path)
	}
}

func TestResolveLogFileExplicit(t *testing.T) {
	path, err := ResolveLogFile("/var/log/custom.log", "/tmp/state")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/var/log/custom.log" {
		t.Fatalf("got %q", path)
	}
}

func TestResolveLogFileRequiresStateDir(t *testing.T) {
	_, err := ResolveLogFile("-", "")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestOpenLogFileRejectsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := openLogFile(dir)
	if err == nil {
		t.Fatal("expected error for directory path")
	}
}

func TestNewFailsWhenLogPathIsDirectory(t *testing.T) {
	dir := t.TempDir()
	_, err := New(Options{LogFile: dir})
	if err == nil {
		t.Fatal("expected error")
	}
}
