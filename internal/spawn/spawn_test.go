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
		Provider: provider.NewTartProvider(exec, true),
		Registrar: &fakeRegistrar{jit: registrar.JITConfig{
			Encoded: "jit-token",
			Runner:  registrar.Runner{ID: 1, Name: "utsusemi-test"},
		}},
	})

	done := make(chan struct{})
	go func() {
		_ = spawner.Run(context.Background(), "utsusemi-test")
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

func TestBootstrapForwardsStdinJITToRunner(t *testing.T) {
	home := t.TempDir()
	recorded := filepath.Join(home, "args.txt")
	stub := "#!/bin/bash\nprintf '%s\\n' \"$@\" > " + recorded + "\n"
	if err := os.WriteFile(filepath.Join(home, "run.sh"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = append(os.Environ(), "RUNNER_VERSION=2.336.0", "RUNNER_HOME="+home)
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

func TestBootstrapRequiresJITOnStdin(t *testing.T) {
	home := t.TempDir()
	cmd := exec.Command("bash", "-c", bootstrapScript)
	cmd.Env = append(os.Environ(), "RUNNER_VERSION=2.336.0", "RUNNER_HOME="+home)
	cmd.Stdin = strings.NewReader("")
	if err := cmd.Run(); err == nil {
		t.Fatal("expected failure when stdin is empty")
	}
}
