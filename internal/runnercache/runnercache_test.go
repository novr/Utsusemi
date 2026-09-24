package runnercache

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTarballName(t *testing.T) {
	got := TarballName("osx-arm64", "v2.337.0")
	if got != "actions-runner-osx-arm64-2.337.0.tar.gz" {
		t.Fatalf("got %q", got)
	}
}

func TestMountEntryAndWithRunnerCacheMount(t *testing.T) {
	state := t.TempDir()
	entry := MountEntry(state)
	wantPrefix := MountName + ":"
	if !strings.HasPrefix(entry, wantPrefix) || !strings.HasSuffix(entry, ":ro") {
		t.Fatalf("entry=%q", entry)
	}
	if !strings.Contains(entry, Dir(state)) && !strings.Contains(entry, "runner-cache") {
		t.Fatalf("entry missing cache dir: %q", entry)
	}

	out := WithRunnerCacheMount(state, []string{"/other:ro"})
	if len(out) != 2 || out[0] != entry || out[1] != "/other:ro" {
		t.Fatalf("out=%v", out)
	}
	if st, err := os.Stat(Dir(state)); err != nil || !st.IsDir() {
		t.Fatalf("WithRunnerCacheMount should create cache dir: %v", err)
	}
	if got := WithRunnerCacheMount("", []string{"/a"}); len(got) != 1 || got[0] != "/a" {
		t.Fatalf("empty stateDir: %v", got)
	}
}

func TestEnsureDownloadsAndSkipsExisting(t *testing.T) {
	state := t.TempDir()
	const body = "runner-bytes"
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.Contains(r.URL.Path, "/v2.337.0/actions-runner-osx-arm64-2.337.0.tar.gz") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	opts := Options{HTTPClient: srv.Client(), BaseURL: srv.URL}
	if err := Ensure(context.Background(), state, "2.337.0", "osx-arm64", opts); err != nil {
		t.Fatal(err)
	}
	dest := TarballPath(state, "osx-arm64", "2.337.0")
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != body {
		t.Fatalf("content=%q", got)
	}
	if hits != 1 {
		t.Fatalf("hits=%d", hits)
	}

	if err := Ensure(context.Background(), state, "2.337.0", "osx-arm64", opts); err != nil {
		t.Fatal(err)
	}
	if hits != 1 {
		t.Fatalf("second Ensure should skip download; hits=%d", hits)
	}
}

func TestEnsurePrunesOtherVersions(t *testing.T) {
	state := t.TempDir()
	dir := Dir(state)
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

	if err := Ensure(context.Background(), state, "2.337.0", "osx-arm64", Options{
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old tarball still present: %v", err)
	}
}

func TestEnsureHTTPError(t *testing.T) {
	state := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusNotFound)
	}))
	defer srv.Close()

	err := Ensure(context.Background(), state, "9.9.9", "osx-arm64", Options{
		HTTPClient: srv.Client(),
		BaseURL:    srv.URL,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
