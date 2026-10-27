package pull

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/runnercache"
)

func TestRunnerLatestResolvesWithoutPrune(t *testing.T) {
	state := t.TempDir()
	dir := runnercache.Dir(state)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "actions-runner-osx-arm64-2.336.0.tar.gz")
	if err := os.WriteFile(old, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("new"))
	}))
	defer srv.Close()

	var stderr bytes.Buffer
	err := Runner(context.Background(), RunnerOptions{
		VersionArg:          "latest",
		StateDir:            state,
		Arch:                "osx-arm64",
		ConfigRunnerVersion: "2.336.0",
		Stderr:              &stderr,
		Latest: func(context.Context) (string, error) {
			return "v2.337.0", nil
		},
		Cache: runnercache.Options{HTTPClient: srv.Client(), BaseURL: srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(runnercache.TarballPath(state, "osx-arm64", "2.337.0"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("content=%q", got)
	}
	if _, err := os.Stat(old); err != nil {
		t.Fatalf("old tarball: %v", err)
	}
	if !strings.Contains(stderr.String(), "runner version 2.337.0") {
		t.Fatalf("stderr=%q", stderr.String())
	}
	if !strings.Contains(stderr.String(), "next start still uses runner_version 2.336.0") {
		t.Fatalf("stderr=%q", stderr.String())
	}
}

func TestRunnerLeavesExistingTarball(t *testing.T) {
	state := t.TempDir()
	dest := runnercache.TarballPath(state, "osx-arm64", "2.337.0")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("kept"), 0o644); err != nil {
		t.Fatal(err)
	}
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("replaced"))
	}))
	defer srv.Close()

	err := Runner(context.Background(), RunnerOptions{
		VersionArg:          "v2.337.0",
		StateDir:            state,
		Arch:                "osx-arm64",
		ConfigRunnerVersion: "2.337.0",
		Cache:               runnercache.Options{HTTPClient: srv.Client(), BaseURL: srv.URL},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "kept" || hits != 0 {
		t.Fatalf("content=%q hits=%d", got, hits)
	}
}

func TestRunnerRejectsPathVersion(t *testing.T) {
	for _, version := range []string{"../2.1.0", "2.1.0/extra", "..", "latest/1", ""} {
		err := Runner(context.Background(), RunnerOptions{
			VersionArg: version,
			StateDir:   t.TempDir(),
			Latest: func(context.Context) (string, error) {
				t.Fatal("latest should not be called")
				return "", nil
			},
		})
		if err == nil {
			t.Fatalf("version %q: expected error", version)
		}
	}
}
