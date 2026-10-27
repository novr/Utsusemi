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

	p := &healthCheckProvider{failLeft: 100, err: fmt.Errorf("%w: x", provider.ErrNotRunning)}
	err := waitUntilReady(context.Background(), slog.Default(), p, "vm")
	if err == nil {
		t.Fatal("expected not-running failure")
	}
	if !errors.Is(err, provider.ErrNotRunning) {
		t.Fatalf("error=%v", err)
	}
	if !errors.Is(err, ErrBootNotRunningBudget) {
		t.Fatalf("expected ErrBootNotRunningBudget, got %v", err)
	}
}

func TestErrWaitReadyWrap(t *testing.T) {
	inner := fmt.Errorf("%w: vm", ErrBootNotRunningBudget)
	err := fmt.Errorf("%w: %w", ErrWaitReady, inner)
	if !errors.Is(err, ErrWaitReady) {
		t.Fatalf("Is ErrWaitReady: %v", err)
	}
	if !errors.Is(err, ErrBootNotRunningBudget) {
		t.Fatalf("Is ErrBootNotRunningBudget: %v", err)
	}
	if got := err.Error(); got != "wait for vm ready: boot_not_running_budget: vm" {
		t.Fatalf("Error()=%q", got)
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
		_, _ = spawner.Run(context.Background(), "utsusemi-test", nil, nil)
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
	base := os.Environ()
	env := make([]string, 0, len(base)+len(extra)+3)
	for _, entry := range base {
		if strings.HasPrefix(entry, "UTSUSEMI_SYSTEM_PROXY_") {
			continue
		}
		env = append(env, entry)
	}
	env = append(env,
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

func TestBootstrapUsesHostCacheWithoutCurl(t *testing.T) {
	home := t.TempDir()
	cache := t.TempDir()
	stub := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "2.335.0")

	tarball := filepath.Join(cache, "actions-runner-osx-arm64-2.336.0.tar.gz")
	if err := os.WriteFile(tarball, []byte("unused"), 0o644); err != nil {
		t.Fatal(err)
	}

	fakebin := t.TempDir()
	fakeCurl := "#!/bin/bash\necho must not curl >&2\nexit 9\n"
	if err := os.WriteFile(filepath.Join(fakebin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeTar := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(fakebin, "tar"), []byte(fakeTar), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home,
		"PATH="+fakebin+":"+os.Getenv("PATH"),
		"RUNNER_CACHE_DIR="+cache,
	)
	cmd.Stdin = strings.NewReader("encoded-jit")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "using host cache") {
		t.Errorf("expected host cache log, got: %s", out)
	}
	if strings.Contains(string(out), "downloading from GitHub") {
		t.Errorf("unexpected GitHub download: %s", out)
	}
	got, err := os.ReadFile(filepath.Join(home, ".runner-version"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(got)) != "2.336.0" {
		t.Fatalf(".runner-version = %q", got)
	}
}

func TestBootstrapDownloadsWhenCacheMissing(t *testing.T) {
	home := t.TempDir()
	stub := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "2.335.0")

	fakebin := t.TempDir()
	fakeCurl := "#!/bin/bash\ntouch actions-runner.tar.gz\n"
	if err := os.WriteFile(filepath.Join(fakebin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeTar := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(fakebin, "tar"), []byte(fakeTar), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home,
		"PATH="+fakebin+":"+os.Getenv("PATH"),
		"RUNNER_CACHE_DIR="+t.TempDir()+"/missing-cache",
	)
	cmd.Stdin = strings.NewReader("encoded-jit")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}
	if !strings.Contains(string(out), "downloading from GitHub") {
		t.Errorf("expected GitHub download log, got: %s", out)
	}
}

// Empty cache dir must not burn the virtiofs wait budget before curling.
func TestBootstrapEmptyCacheDirDoesNotStall(t *testing.T) {
	home := t.TempDir()
	cache := t.TempDir() // exists, but no tarball
	stub := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "2.335.0")

	fakebin := t.TempDir()
	fakeCurl := "#!/bin/bash\ntouch actions-runner.tar.gz\n"
	if err := os.WriteFile(filepath.Join(fakebin, "curl"), []byte(fakeCurl), 0o755); err != nil {
		t.Fatal(err)
	}
	fakeTar := "#!/bin/bash\n"
	if err := os.WriteFile(filepath.Join(fakebin, "tar"), []byte(fakeTar), 0o755); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home,
		"PATH="+fakebin+":"+os.Getenv("PATH"),
		"RUNNER_CACHE_DIR="+cache,
	)
	cmd.Stdin = strings.NewReader("encoded-jit")
	out, err := cmd.CombinedOutput()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}
	if elapsed >= 2*time.Second {
		t.Fatalf("empty cache dir stalled for %s (virtiofs wait should not apply)", elapsed)
	}
	if !strings.Contains(string(out), "downloading from GitHub") {
		t.Errorf("expected GitHub download, got: %s", out)
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

func TestBootstrapSystemProxyScriptAvoidsHostPaths(t *testing.T) {
	for _, forbidden := range []string{"/usr/sbin/networksetup", "setautoproxystate"} {
		if strings.Contains(bootstrapScript, forbidden) {
			t.Fatalf("bootstrap.sh contains %s", forbidden)
		}
	}
	if !strings.Contains(bootstrapScript, `route_bin="/sbin/route"`) {
		t.Fatal("bootstrap.sh must fall back to /sbin/route when route is not on PATH")
	}
}

func TestBootstrapSystemProxyUnsetDoesNotCallSudo(t *testing.T) {
	home := t.TempDir()
	writeRunnerListenerStub(t, home, "2.336.0")
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte("#!/bin/bash\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	writeFake(t, bin, "sudo", `#!/bin/bash
echo sudo called >&2
exit 9
`)
	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = bootstrapTestEnv(home, "PATH="+bin+":"+os.Getenv("PATH"))
	cmd.Stdin = strings.NewReader("encoded-jit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}
}

func TestBootstrapSystemProxyAppliesDefaultRoute(t *testing.T) {
	home := t.TempDir()
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, "foo.local"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "2.336.0")
	envOut := filepath.Join(home, "env.txt")
	argsOut := filepath.Join(home, "args.txt")
	runStub := fmt.Sprintf(`#!/bin/bash
env | sort > %s
printf '%%s\n' "$@" > %s
`, shellQuote(envOut), shellQuote(argsOut))
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(runStub), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(work, "calls.txt")
	order := filepath.Join(work, "order.txt")
	orderBody := `An asterisk (*) denotes that a network service is disabled.
(1) Extra
(Hardware Port: Extra, Device: en01)

(2) *Thunderbolt Bridge
(Hardware Port: Thunderbolt Bridge, Device: en0)

(3) Wi-Fi
(Hardware Port: Wi-Fi, Device: en1)
`
	if err := os.WriteFile(order, []byte(orderBody), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	writeFake(t, bin, "route", proxyRouteScript)
	writeFake(t, bin, "sudo", proxySudoScript)
	writeFake(t, bin, "networksetup", proxyNetworksetupScript)

	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Dir = work
	cmd.Env = bootstrapTestEnv(home,
		"PATH="+bin+":"+os.Getenv("PATH"),
		"PROXY_LOG="+logPath,
		"ORDER_FILE="+order,
		"HTTP_PROXY=http://proxy.example:8080",
		"UTSUSEMI_SYSTEM_PROXY_HTTP_HOST=proxy.example",
		"UTSUSEMI_SYSTEM_PROXY_HTTP_PORT=8080",
		"UTSUSEMI_SYSTEM_PROXY_HTTPS_HOST=proxy.example",
		"UTSUSEMI_SYSTEM_PROXY_HTTPS_PORT=8080",
		"UTSUSEMI_SYSTEM_PROXY_BYPASS=localhost,127.0.0.1,<local>,*.local",
	)
	cmd.Stdin = strings.NewReader("encoded-jit")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bootstrap failed: %v: %s", err, out)
	}
	calls := readProxyCalls(t, logPath)
	want := [][]string{
		{"-listnetworkserviceorder"},
		{"-setproxyautodiscovery", "Thunderbolt Bridge", "off"},
		{"-setwebproxy", "Thunderbolt Bridge", "proxy.example", "8080", "off"},
		{"-setwebproxystate", "Thunderbolt Bridge", "on"},
		{"-setsecurewebproxy", "Thunderbolt Bridge", "proxy.example", "8080", "off"},
		{"-setsecurewebproxystate", "Thunderbolt Bridge", "on"},
		{"-setproxybypassdomains", "Thunderbolt Bridge", "localhost", "127.0.0.1", "<local>", "*.local"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls=%q", calls)
	}
	for i := range want {
		if strings.Join(calls[i], "\n") != strings.Join(want[i], "\n") {
			t.Fatalf("call %d = %q, want %q", i, calls[i], want[i])
		}
	}
	joined := strings.Join(flatten(calls), "\n")
	if strings.Contains(joined, "Wi-Fi") || strings.Contains(joined, "Extra") || strings.Contains(joined, "foo.local") {
		t.Fatalf("unexpected target or glob: %s", joined)
	}
	envText, err := os.ReadFile(envOut)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(envText), "UTSUSEMI_SYSTEM_PROXY_") {
		t.Fatalf("control vars reached run.sh:\n%s", envText)
	}
	if !strings.Contains(string(envText), "HTTP_PROXY=http://proxy.example:8080") {
		t.Fatalf("http_proxy missing from run.sh:\n%s", envText)
	}
	args, err := os.ReadFile(argsOut)
	if err != nil {
		t.Fatal(err)
	}
	if string(args) != "--jitconfig\nencoded-jit\n" {
		t.Fatalf("run.sh args = %q", args)
	}
}

func TestBootstrapSystemProxyOmitsBypassWhenUnset(t *testing.T) {
	logPath := runSystemProxyBootstrap(t, systemProxyOpts{})
	calls := readProxyCalls(t, logPath)
	want := [][]string{
		{"-listnetworkserviceorder"},
		{"-setproxyautodiscovery", "Ethernet", "off"},
		{"-setwebproxy", "Ethernet", "proxy.example", "8080", "off"},
		{"-setwebproxystate", "Ethernet", "on"},
	}
	if len(calls) != len(want) {
		t.Fatalf("calls=%q", calls)
	}
	for i := range want {
		if strings.Join(calls[i], "\n") != strings.Join(want[i], "\n") {
			t.Fatalf("call %d = %q, want %q", i, calls[i], want[i])
		}
	}
}

func TestBootstrapSystemProxyNoRoute(t *testing.T) {
	_, err := runSystemProxyBootstrapErr(t, systemProxyOpts{emptyRoute: true})
	if err == nil {
		t.Fatal("expected failure without a default route")
	}
}

func TestBootstrapSystemProxyNoService(t *testing.T) {
	_, err := runSystemProxyBootstrapErr(t, systemProxyOpts{order: `(1) Wi-Fi
(Hardware Port: Wi-Fi, Device: en1)
`})
	if err == nil {
		t.Fatal("expected failure when the default route has no service")
	}
}

func TestBootstrapSystemProxyNetworksetupFailure(t *testing.T) {
	_, err := runSystemProxyBootstrapErr(t, systemProxyOpts{failOn: "-setwebproxy"})
	if err == nil {
		t.Fatal("expected failure when networksetup fails")
	}
}

func TestBootstrapSystemProxySudoFailure(t *testing.T) {
	_, err := runSystemProxyBootstrapErr(t, systemProxyOpts{sudoFail: true})
	if err == nil {
		t.Fatal("expected failure when sudo -n fails")
	}
}

type systemProxyOpts struct {
	emptyRoute bool
	order      string
	failOn     string
	sudoFail   bool
	bypass     bool
}

func runSystemProxyBootstrap(t *testing.T, opts systemProxyOpts) string {
	t.Helper()
	logPath, err := runSystemProxyBootstrapErr(t, opts)
	if err != nil {
		t.Fatalf("bootstrap failed: %v", err)
	}
	return logPath
}

func runSystemProxyBootstrapErr(t *testing.T, opts systemProxyOpts) (string, error) {
	t.Helper()
	home := t.TempDir()
	work := t.TempDir()
	argsOut := filepath.Join(home, "args.txt")
	runStub := fmt.Sprintf(`#!/bin/bash
printf '%%s\n' "$@" > %s
`, shellQuote(argsOut))
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(runStub), 0o755); err != nil {
		t.Fatal(err)
	}
	writeRunnerListenerStub(t, home, "2.336.0")
	order := opts.order
	if order == "" {
		order = `(1) Ethernet
(Hardware Port: Ethernet, Device: en0)
(2) Wi-Fi
(Hardware Port: Wi-Fi, Device: en1)
`
	}
	orderPath := filepath.Join(work, "order.txt")
	if err := os.WriteFile(orderPath, []byte(order), 0o644); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(work, "calls.txt")
	bin := t.TempDir()
	route := proxyRouteScript
	if opts.emptyRoute {
		route = proxyRouteEmptyScript
	}
	writeFake(t, bin, "route", route)
	sudo := proxySudoScript
	if opts.sudoFail {
		sudo = proxySudoFailScript
	}
	writeFake(t, bin, "sudo", sudo)
	writeFake(t, bin, "networksetup", proxyNetworksetupScript)
	extra := []string{
		"PATH=" + bin + ":" + os.Getenv("PATH"),
		"PROXY_LOG=" + logPath,
		"ORDER_FILE=" + orderPath,
		"UTSUSEMI_SYSTEM_PROXY_HTTP_HOST=proxy.example",
		"UTSUSEMI_SYSTEM_PROXY_HTTP_PORT=8080",
	}
	if opts.failOn != "" {
		extra = append(extra, "FAIL_ON="+opts.failOn)
	}
	if opts.bypass {
		extra = append(extra, "UTSUSEMI_SYSTEM_PROXY_BYPASS=localhost")
	}
	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Dir = work
	cmd.Env = bootstrapTestEnv(home, extra...)
	cmd.Stdin = strings.NewReader("encoded-jit")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, statErr := os.Stat(argsOut); statErr == nil {
			t.Fatalf("run.sh ran after bootstrap error: %v: %s", err, out)
		}
		return logPath, err
	}
	return logPath, nil
}

const proxyRouteScript = `#!/bin/bash
if [ "$1" = -n ] && [ "$2" = get ] && [ "$3" = default ]; then
  echo 'interface: en0'
  exit 0
fi
echo unexpected route >&2
exit 1
`

const proxyRouteEmptyScript = `#!/bin/bash
if [ "$1" = -n ] && [ "$2" = get ] && [ "$3" = default ]; then
  exit 0
fi
exit 1
`

const proxySudoScript = `#!/bin/bash
if [ "${1:-}" != -n ]; then
  echo 'sudo without -n' >&2
  exit 3
fi
shift
"$@"
`

const proxySudoFailScript = `#!/bin/bash
exit 3
`

const proxyNetworksetupScript = `#!/bin/bash
{
  printf '%s\n' "$@"
  echo ---
} >> "$PROXY_LOG"
if [ "$1" = -listnetworkserviceorder ]; then
  cat "$ORDER_FILE"
  exit 0
fi
if [ -n "${FAIL_ON:-}" ] && [ "$1" = "$FAIL_ON" ]; then
  exit 9
fi
exit 0
`

func writeFake(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
}

func shellQuote(path string) string {
	return "'" + strings.ReplaceAll(path, "'", "'\\''") + "'"
}

func readProxyCalls(t *testing.T, path string) [][]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var calls [][]string
	var cur []string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "---" {
			calls = append(calls, cur)
			cur = nil
			continue
		}
		if line == "" {
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		calls = append(calls, cur)
	}
	return calls
}

func flatten(calls [][]string) []string {
	var out []string
	for _, call := range calls {
		out = append(out, call...)
	}
	return out
}
