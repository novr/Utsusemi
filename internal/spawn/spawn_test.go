package spawn

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

type fakeRegistrar struct {
	jit registrar.JITConfig
}

func (f *fakeRegistrar) CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (registrar.JITConfig, error) {
	return f.jit, nil
}

func (f *fakeRegistrar) DeleteRunner(ctx context.Context, tgt target.Target, runnerID int64) error {
	return nil
}

func (f *fakeRegistrar) ListRunners(ctx context.Context, tgt target.Target, prefix string) ([]registrar.Runner, error) {
	return nil, nil
}

func (f *fakeRegistrar) ValidateCredential(ctx context.Context, service, account string) error {
	return nil
}

type healthCheckProvider struct {
	provider.Stub
	mu       sync.Mutex
	failLeft int
	calls    int
	err      error
}

func (p *healthCheckProvider) HealthCheck(context.Context, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls++
	if p.failLeft > 0 {
		p.failLeft--
		if p.err != nil {
			return p.err
		}
		return fmt.Errorf("not ready")
	}
	return nil
}

func (p *healthCheckProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type deleteTrackingProvider struct {
	provider.Stub
	mu          sync.Mutex
	deleteFails int
	deleteCalls int
	stopCalls   int
}

func (p *deleteTrackingProvider) Stop(context.Context, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopCalls++
	return fmt.Errorf("stop ignored")
}

func (p *deleteTrackingProvider) Delete(context.Context, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deleteCalls++
	if p.deleteFails > 0 {
		p.deleteFails--
		return fmt.Errorf("delete busy")
	}
	return nil
}

func TestWaitUntilReadySucceedsAfterFailures(t *testing.T) {
	restoreReadyTunables(t)
	readyPollInterval = 5 * time.Millisecond

	p := &healthCheckProvider{failLeft: 2}
	log := slog.Default()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := waitUntilReady(ctx, log, p, "vm"); err != nil {
		t.Fatal(err)
	}
	if n := p.callCount(); n < 3 {
		t.Fatalf("HealthCheck calls=%d, want >= 3", n)
	}
}

func TestWaitUntilReadyTimesOut(t *testing.T) {
	restoreReadyTunables(t)
	readyPollInterval = 5 * time.Millisecond
	readyTimeout = 40 * time.Millisecond
	readyAttemptBudget = 20 * time.Millisecond
	readyWarnEvery = time.Hour

	p := &healthCheckProvider{failLeft: 100, err: fmt.Errorf("grpc down")}
	log := slog.Default()
	err := waitUntilReady(context.Background(), log, p, "vm")
	if err == nil {
		t.Fatal("expected timeout")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want DeadlineExceeded, got %v", err)
	}
	if !strings.Contains(err.Error(), "grpc down") {
		t.Fatalf("want guest error wrapped, got %v", err)
	}
}

func TestWaitUntilReadyCapsHungHealthCheck(t *testing.T) {
	restoreReadyTunables(t)
	readyTimeout = 200 * time.Millisecond
	readyAttemptBudget = 40 * time.Millisecond
	readyPollInterval = 5 * time.Millisecond
	readyWarnEvery = time.Hour

	p := &hungHealthProvider{}
	err := waitUntilReady(context.Background(), slog.Default(), p, "vm")
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("want deadline, got %v", err)
	}
	if n := p.callCount(); n < 2 {
		t.Fatalf("HealthCheck calls=%d, want >= 2 (per-attempt budget)", n)
	}
}

func TestStopAndDeleteBestEffortRetries(t *testing.T) {
	restoreTeardownTunables(t)
	teardownRetryWait = time.Millisecond
	teardownAttemptBud = time.Second

	p := &deleteTrackingProvider{deleteFails: 2}
	stopAndDeleteBestEffort(slog.Default(), p, "vm")
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.stopCalls != 3 {
		t.Fatalf("stopCalls=%d, want 3 (retry with delete)", p.stopCalls)
	}
	if p.deleteCalls != 3 {
		t.Fatalf("deleteCalls=%d, want 3", p.deleteCalls)
	}
	if p.deleteFails != 0 {
		t.Fatalf("deleteFails left=%d", p.deleteFails)
	}
}

func TestWaitUntilReadyGivesUpWhenNotRunning(t *testing.T) {
	restoreReadyTunables(t)
	readyPollInterval = 5 * time.Millisecond
	notRunningBudget = 40 * time.Millisecond
	readyTimeout = time.Minute
	readyWarnEvery = time.Hour

	p := &healthCheckProvider{failLeft: 100, err: fmt.Errorf("vm x is not running")}
	err := waitUntilReady(context.Background(), slog.Default(), p, "vm")
	if err == nil {
		t.Fatal("expected not-running failure")
	}
	if !strings.Contains(err.Error(), "not running") {
		t.Fatalf("error=%v", err)
	}
}

func TestStopAndDeleteBestEffortGoneIsOK(t *testing.T) {
	restoreTeardownTunables(t)
	teardownAttemptBud = time.Second
	p := &goneDeleteProvider{}
	stopAndDeleteBestEffort(slog.Default(), p, "vm")
	if p.deleteCalls != 1 {
		t.Fatalf("deleteCalls=%d, want 1 (no retry on gone)", p.deleteCalls)
	}
}

func restoreReadyTunables(t *testing.T) {
	t.Helper()
	origTimeout, origPoll, origBudget, origWarn, origNotRun := readyTimeout, readyPollInterval, readyAttemptBudget, readyWarnEvery, notRunningBudget
	t.Cleanup(func() {
		readyTimeout = origTimeout
		readyPollInterval = origPoll
		readyAttemptBudget = origBudget
		readyWarnEvery = origWarn
		notRunningBudget = origNotRun
	})
}

func restoreTeardownTunables(t *testing.T) {
	t.Helper()
	origAttempts, origWait, origRunner, origBud := teardownAttempts, teardownRetryWait, deleteRunnerTries, teardownAttemptBud
	t.Cleanup(func() {
		teardownAttempts = origAttempts
		teardownRetryWait = origWait
		deleteRunnerTries = origRunner
		teardownAttemptBud = origBud
	})
}

type hungHealthProvider struct {
	provider.Stub
	mu    sync.Mutex
	calls int
}

func (p *hungHealthProvider) HealthCheck(ctx context.Context, _ string) error {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (p *hungHealthProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

type goneDeleteProvider struct {
	provider.Stub
	mu          sync.Mutex
	deleteCalls int
	stopCalls   int
}

func (p *goneDeleteProvider) Stop(context.Context, string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stopCalls++
	return nil
}

func (p *goneDeleteProvider) Delete(_ context.Context, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.deleteCalls++
	return fmt.Errorf("VM \"%s\" does not exist", name)
}

func TestSpawnPassesJITOnStdin(t *testing.T) {
	exec := provider.NewFakeExecutor()
	spawner := New(Options{
		Config: &config.Config{
			BaseImage:     "image",
			RunnerVersion: "2.336.0",
			Labels:        []string{"self-hosted"},
			SpawnTimeout:  config.Duration(30 * time.Second),
			JobTimeout:    config.Duration(30 * time.Second),
			MinFreeDiskGB: 1,
		},
		Target:   target.Target{Type: target.TypeRepo, Owner: "alice", Repo: "app"},
		Provider: provider.NewTartProvider(exec, true, nil),
		Registrar: &fakeRegistrar{jit: registrar.JITConfig{
			Encoded: "jit-token",
			Runner:  registrar.Runner{ID: 1, Name: "utsusemi-test"},
		}},
	})

	done := make(chan struct{})
	go func() {
		_, _ = spawner.Run(context.Background(), "utsusemi-test")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}

	var found bool
	for _, call := range exec.Calls {
		if len(call.Args) >= 2 && call.Args[0] == "exec" && string(call.Stdin) == "jit-token" {
			if call.Args[len(call.Args)-2] != "-c" {
				t.Fatalf("expected bash -c, args=%v", call.Args)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("exec call with jit stdin not found")
	}
}

func TestSpawnUsesKeychainRegistrarInterface(_ *testing.T) {
	_ = keychain.NewMemoryStore()
}

func bootstrapTestEnv(home string, extra ...string) []string {
	env := append(os.Environ(),
		"RUNNER_VERSION=2.336.0",
		"RUNNER_ARCH=osx-arm64",
		"RUNNER_HOME="+home,
	)
	return append(env, extra...)
}

func writeRunnerListenerStub(t *testing.T, home, version string) {
	t.Helper()
	binDir := filepath.Join(home, "bin")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := "#!/bin/bash\nif [ \"$1\" = \"--version\" ]; then\n  echo " + version + "\nfi\n"
	if err := os.WriteFile(filepath.Join(binDir, "Runner.Listener"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
}

func installFailingCurl(t *testing.T) string {
	t.Helper()
	fakebin := t.TempDir()
	fakeCurl := "#!/bin/bash\necho bootstrap must not invoke curl >&2\nexit 9\n"
	if err := os.WriteFile(filepath.Join(fakebin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	return fakebin
}

func TestBootstrapForwardsStdinJITToRunner(t *testing.T) {
	home := t.TempDir()
	recorded := filepath.Join(home, "args.txt")
	stub := "#!/bin/bash\nprintf '%s\\n' \"$@\" > " + recorded + "\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "2.336.0")

	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home)
	cmd.Stdin = strings.NewReader("encoded-jit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}

	got, err := os.ReadFile(recorded)
	if err != nil {
		t.Fatal(err)
	}
	if want := "--jitconfig\nencoded-jit\n"; string(got) != want {
		t.Fatalf("run.sh args = %q, want %q", string(got), want)
	}
}

// TestBootstrapSkipsDownloadWhenInstalled verifies stock image layout
// (Runner.Listener present, no .runner-version sentinel) skips download.
func TestBootstrapSkipsDownloadWhenInstalled(t *testing.T) {
	home := t.TempDir()
	stub := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "2.336.0")

	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home, "PATH="+installFailingCurl(t)+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader("encoded-jit")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "skipping download") {
		t.Errorf("expected 'skipping download' in output, got: %s", out)
	}
	if _, err := os.Stat(filepath.Join(home, ".runner-version")); err == nil {
		t.Error("expected no .runner-version when skip is driven by Runner.Listener")
	}
}

func TestBootstrapSkipsDownloadWhenSentinelMatches(t *testing.T) {
	home := t.TempDir()
	stub := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".runner-version"), []byte("2.336.0"), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home)
	cmd.Stdin = strings.NewReader("encoded-jit")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "skipping download") {
		t.Errorf("expected 'skipping download' in output, got: %s", out)
	}
}

func TestBootstrapSkipsDownloadWithVersionPrefix(t *testing.T) {
	home := t.TempDir()
	stub := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "v2.336.0")

	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home, "PATH="+installFailingCurl(t)+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader("encoded-jit")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "skipping download") {
		t.Errorf("expected 'skipping download' in output, got: %s", out)
	}
}

// TestBootstrapRedownloadsOnVersionMismatch verifies that a stale runner binary
// causes bootstrap to re-install and write .runner-version.
func TestBootstrapRedownloadsOnVersionMismatch(t *testing.T) {
	home := t.TempDir()

	stub := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "2.335.0")

	// Provide fake curl and tar so the download step succeeds without network.
	fakebin := t.TempDir()
	fakeCurl := "#!/bin/bash\ntouch actions-runner.tar.gz\n"
	if err := os.WriteFile(filepath.Join(fakebin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeTar := "#!/bin/bash\n" // existing run.sh remains; tar is a no-op
	if err := os.WriteFile(filepath.Join(fakebin, "tar"), []byte(fakeTar), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home, "PATH="+fakebin+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader("encoded-jit")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}

	// .runner-version must be updated to the new version.
	got, err := os.ReadFile(filepath.Join(home, ".runner-version"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "2.336.0" {
		t.Fatalf(".runner-version = %q, want %q", string(got), "2.336.0")
	}
	if !strings.Contains(string(out), "installing runner") {
		t.Errorf("expected 'installing runner' in output, got: %s", out)
	}
}

func TestBootstrapRequiresJITOnStdin(t *testing.T) {
	home := t.TempDir()
	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home)
	cmd.Stdin = strings.NewReader("")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected failure when stdin is empty")
	}
}
