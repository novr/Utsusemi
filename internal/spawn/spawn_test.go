package spawn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
		if len(call.Args) >= 2 && call.Args[0] == "exec" {
			if string(call.Stdin) != "jit-token" {
				t.Fatalf("stdin = %q", string(call.Stdin))
			}
			if call.Args[len(call.Args)-2] != "-c" {
				t.Fatalf("expected bash -c, args=%v", call.Args)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("exec call not found")
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
