package doctor

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/notify"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/runnercache"
	"github.com/novr/utsusemi/internal/runnerrelease"
	"github.com/novr/utsusemi/internal/spawn"
)

func TestCollectFailsWithoutProvider(t *testing.T) {
	r := Collect(context.Background(), Input{Cfg: &config.Config{}})
	if FailedCount(r) == 0 {
		t.Fatal("expected failure")
	}
}

func TestCheckRunnerVersionMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := spawn.SaveLastSpawn(dir, spawn.LastSpawn{
		At:            time.Now().UTC(),
		RunnerVersion: "2.335.0",
		CloneMs:       1,
		Success:       true,
	}); err != nil {
		t.Fatal(err)
	}
	release := releaseClient(t, "2.336.0")
	checks := recordChecks(func(add checkFn) {
		checkRunnerVersion(context.Background(), &config.Config{RunnerVersion: "2.336.0", StateDir: dir}, release, add)
	})
	if len(checks) != 1 || checks[0].Status != StatusWarn {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestCheckRunnerVersionOlderThanLatest(t *testing.T) {
	release := releaseClient(t, "2.337.0")
	checks := recordChecks(func(add checkFn) {
		checkRunnerVersion(context.Background(), &config.Config{RunnerVersion: "2.300.0", StateDir: t.TempDir()}, release, add)
	})
	if len(checks) != 1 || checks[0].Status != StatusFail || checks[0].Name != "runner_version" {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestCheckRunnerVersionSkipsLatestOnNetworkError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)
	release := &runnerrelease.Client{HTTPClient: srv.Client(), URL: srv.URL}
	checks := recordChecks(func(add checkFn) {
		checkRunnerVersion(context.Background(), &config.Config{RunnerVersion: "2.300.0", StateDir: t.TempDir()}, release, add)
	})
	if len(checks) != 1 || checks[0].Status != StatusOK {
		t.Fatalf("network error must skip latest comparison; checks=%+v", checks)
	}
}

func TestCheckRunnerVersionOlderTakesPrecedenceOverMismatch(t *testing.T) {
	dir := t.TempDir()
	if err := spawn.SaveLastSpawn(dir, spawn.LastSpawn{
		At:            time.Now().UTC(),
		RunnerVersion: "2.299.0",
		CloneMs:       1,
		Success:       true,
	}); err != nil {
		t.Fatal(err)
	}
	release := releaseClient(t, "2.337.0")
	checks := recordChecks(func(add checkFn) {
		checkRunnerVersion(context.Background(), &config.Config{RunnerVersion: "2.300.0", StateDir: dir}, release, add)
	})
	if len(checks) != 1 || checks[0].Status != StatusFail {
		t.Fatalf("checks=%+v", checks)
	}
}

func TestCheckRunnerCache(t *testing.T) {
	dir := t.TempDir()
	p := provider.NewTartProvider(provider.NewFakeExecutor(), false, nil)
	missing := recordChecks(func(add checkFn) {
		checkRunnerCache(&config.Config{StateDir: dir, RunnerVersion: "2.337.0"}, p, add)
	})
	if len(missing) != 1 || missing[0].Status != StatusWarn || missing[0].Name != "runner_cache" {
		t.Fatalf("missing=%+v", missing)
	}
	if missing[0].Message != "missing host tarball; bootstrap may use image install or curl" {
		t.Fatalf("message=%q", missing[0].Message)
	}

	dest := runnercache.TarballPath(dir, "osx-arm64", "2.337.0")
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dest, []byte("tarball"), 0o644); err != nil {
		t.Fatal(err)
	}
	ok := recordChecks(func(add checkFn) {
		checkRunnerCache(&config.Config{StateDir: dir, RunnerVersion: "2.337.0"}, p, add)
	})
	if len(ok) != 1 || ok[0].Status != StatusOK {
		t.Fatalf("ok=%+v", ok)
	}
}

func TestCheckLoopbackBroker(t *testing.T) {
	checks := recordChecks(func(add checkFn) {
		checkLoopbackBroker(context.Background(), &config.Config{
			Registration: config.Registration{
				Mode:      config.ModeHostedApp,
				BrokerURL: "http://127.0.0.1:1",
			},
		}, add)
	})
	if len(checks) != 1 || checks[0].Status != StatusFail || checks[0].Name != "broker" {
		t.Fatalf("checks=%+v", checks)
	}
	hosted := recordChecks(func(add checkFn) {
		checkLoopbackBroker(context.Background(), &config.Config{
			Registration: config.Registration{
				Mode:      config.ModeHostedApp,
				BrokerURL: config.DefaultHostedAppBrokerURL,
			},
		}, add)
	})
	if len(hosted) != 0 {
		t.Fatalf("hosted=%+v", hosted)
	}
}

func TestCheckMounts(t *testing.T) {
	existing := t.TempDir()
	tests := []struct {
		name       string
		cfg        *config.Config
		wantChecks int
		wantStatus Status
	}{
		{name: "unset", cfg: &config.Config{}, wantChecks: 0},
		{name: "missing", cfg: &config.Config{Mounts: []string{"/nonexistent-utsusemi-mount"}}, wantChecks: 1, wantStatus: StatusFail},
		{name: "ok", cfg: &config.Config{Mounts: []string{existing}}, wantChecks: 1, wantStatus: StatusOK},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			checks := recordChecks(func(add checkFn) {
				checkMounts(tc.cfg, add)
			})
			if len(checks) != tc.wantChecks {
				t.Fatalf("checks=%+v", checks)
			}
			if tc.wantChecks > 0 && checks[0].Status != tc.wantStatus {
				t.Fatalf("checks=%+v", checks)
			}
		})
	}
}

func TestCheckAlerts(t *testing.T) {
	t.Setenv(notify.EnvWebhookURL, "")
	store := keychain.NewMemoryStore()
	checks := recordChecks(func(add checkFn) {
		checkAlerts(store, add)
	})
	if len(checks) != 1 || checks[0].Status != StatusOK || checks[0].Message != "not configured" {
		t.Fatalf("checks=%+v", checks)
	}
	_ = store.Set(notify.AlertService, notify.AlertAccount, "https://example.com/hook")
	checks = recordChecks(func(add checkFn) {
		checkAlerts(store, add)
	})
	if len(checks) != 1 || checks[0].Message != "configured" {
		t.Fatalf("checks=%+v", checks)
	}
}

type checkFn func(string, Status, string)

func recordChecks(fn func(checkFn)) []Check {
	var checks []Check
	fn(func(name string, status Status, msg string) {
		checks = append(checks, Check{Name: name, Status: status, Message: msg})
	})
	return checks
}

func releaseClient(t *testing.T, version string) *runnerrelease.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v" + version})
	}))
	t.Cleanup(srv.Close)
	return &runnerrelease.Client{HTTPClient: srv.Client(), URL: srv.URL}
}
