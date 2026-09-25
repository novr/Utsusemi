package notify

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/keychain"
)

type recordingNotifier struct {
	mu     sync.Mutex
	events []Event
	err    error
}

func (r *recordingNotifier) Alert(ctx context.Context, e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return r.err
}

func (r *recordingNotifier) codes() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.events))
	for i, e := range r.events {
		out[i] = e.Code
	}
	return out
}

func TestWebhookAlertPostsJSON(t *testing.T) {
	var got webhookPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("content-type=%q", r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &got); err != nil {
			t.Errorf("unmarshal: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := NewWebhook(srv.URL)
	err := w.Alert(context.Background(), Event{
		Code:    CodePoolStuck,
		Message: "empty",
		HostID:  "host1",
		Target:  "org/acme",
		Detail:  "clone failed",
		At:      time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Code != CodePoolStuck || got.HostID != "host1" || got.Detail != "clone failed" {
		t.Fatalf("payload=%+v", got)
	}
	wantText := "[pool_stuck] host=host1 target=org/acme empty: clone failed"
	if got.Text != wantText {
		t.Fatalf("text=%q want %q", got.Text, wantText)
	}
}

func TestWebhookAlertNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := NewWebhook(srv.URL).Alert(context.Background(), Event{Code: CodeAgentFatal}); err == nil {
		t.Fatal("expected error")
	}
}

func TestWebhookAlertRespectsTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(3 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	w := NewWebhook(srv.URL)
	start := time.Now()
	err := w.Alert(context.Background(), Event{Code: CodeAgentFatal})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected timeout error")
	}
	if elapsed > PostTimeout+time.Second {
		t.Fatalf("elapsed=%s, want around %s", elapsed, PostTimeout)
	}
}

func TestValidateWebhookURL(t *testing.T) {
	if err := ValidateWebhookURL("https://hooks.example/x"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWebhookURL("http://127.0.0.1/hook"); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{"", "ftp://x", "not-a-url", "https://"} {
		if err := ValidateWebhookURL(raw); err == nil {
			t.Fatalf("expected error for %q", raw)
		}
	}
}

func TestResolveWebhookURLEnvOverridesKeychain(t *testing.T) {
	t.Setenv(EnvWebhookURL, "https://example.com/from-env")
	store := keychain.NewMemoryStore()
	_ = store.Set(AlertService, AlertAccount, "https://example.com/from-keychain")
	u, err := ResolveWebhookURL(store)
	if err != nil || u != "https://example.com/from-env" {
		t.Fatalf("url=%q err=%v", u, err)
	}
}

func TestResolveWebhookURLKeychain(t *testing.T) {
	t.Setenv(EnvWebhookURL, "")
	store := keychain.NewMemoryStore()
	_ = store.Set(AlertService, AlertAccount, "https://example.com/kc")
	u, err := ResolveWebhookURL(store)
	if err != nil || u != "https://example.com/kc" {
		t.Fatalf("url=%q err=%v", u, err)
	}
}

func TestResolveWebhookURLMissing(t *testing.T) {
	t.Setenv(EnvWebhookURL, "")
	u, err := ResolveWebhookURL(keychain.NewMemoryStore())
	if err != nil || u != "" {
		t.Fatalf("url=%q err=%v", u, err)
	}
}

func TestMonitorPoolStuckDedupeAndClear(t *testing.T) {
	rec := &recordingNotifier{}
	var now atomic.Value
	now.Store(time.Unix(0, 0))
	m := NewMonitor(MonitorOptions{
		Notifier:   rec,
		StuckAfter: time.Minute,
		HostID:     "h",
		Target:     "t",
		Now:        func() time.Time { return now.Load().(time.Time) },
	})

	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LastSpawnErr: "boom"})
	if len(rec.codes()) != 0 {
		t.Fatalf("too early: %v", rec.codes())
	}

	now.Store(time.Unix(0, 0).Add(time.Minute))
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LastSpawnErr: "boom"})
	if got := rec.codes(); len(got) != 1 || got[0] != CodePoolStuck {
		t.Fatalf("codes=%v", got)
	}

	now.Store(time.Unix(0, 0).Add(2 * time.Minute))
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LastSpawnErr: "boom"})
	if len(rec.codes()) != 1 {
		t.Fatalf("dedupe failed: %v", rec.codes())
	}

	m.Clear(CodePoolStuck)
	now.Store(time.Unix(0, 0).Add(2 * time.Minute))
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1})
	now.Store(time.Unix(0, 0).Add(3 * time.Minute))
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1})
	if len(rec.codes()) != 2 {
		t.Fatalf("after clear want 2, got %v", rec.codes())
	}
}

func TestMonitorDiskBlocksPoolStuck(t *testing.T) {
	rec := &recordingNotifier{}
	var now atomic.Value
	now.Store(time.Unix(0, 0))
	m := NewMonitor(MonitorOptions{
		Notifier:   rec,
		StuckAfter: time.Minute,
		Now:        func() time.Time { return now.Load().(time.Time) },
	})

	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LowDisk: true})
	now.Store(time.Unix(0, 0).Add(time.Minute))
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LowDisk: true})
	if got := rec.codes(); len(got) != 1 || got[0] != CodeDiskBlocked {
		t.Fatalf("codes=%v", got)
	}
}

func TestMonitorAlertFatalOnce(t *testing.T) {
	rec := &recordingNotifier{}
	m := NewMonitor(MonitorOptions{Notifier: rec})
	m.AlertFatal(context.Background(), errors.New("unauthorized"))
	m.AlertFatal(context.Background(), errors.New("unauthorized again"))
	if got := rec.codes(); len(got) != 1 || got[0] != CodeAgentFatal {
		t.Fatalf("codes=%v", got)
	}
}

func TestMonitorMarksFiredEvenWhenWebhookFails(t *testing.T) {
	rec := &recordingNotifier{err: errors.New("down")}
	m := NewMonitor(MonitorOptions{Notifier: rec})
	m.AlertFatal(context.Background(), errors.New("fatal"))
	m.AlertFatal(context.Background(), errors.New("fatal"))
	if len(rec.codes()) != 1 {
		t.Fatalf("codes=%v", rec.codes())
	}
}

func TestPoolStuckFiresAcrossFailedSpawnAttempts(t *testing.T) {
	rec := &recordingNotifier{}
	var now atomic.Value
	now.Store(time.Unix(0, 0))
	m := NewMonitor(MonitorOptions{
		Notifier:   rec,
		StuckAfter: time.Minute,
		Now:        func() time.Time { return now.Load().(time.Time) },
	})

	// active flickers during failure loops; must not refresh lastGoodAt
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LastSpawnErr: "boom"})
	now.Store(time.Unix(0, 0).Add(30 * time.Second))
	m.Evaluate(context.Background(), PoolState{Active: 1, PoolSize: 1, LastSpawnErr: "boom"})
	now.Store(time.Unix(0, 0).Add(45 * time.Second))
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LastSpawnErr: "boom"})
	if len(rec.codes()) != 0 {
		t.Fatalf("should not fire yet: %v", rec.codes())
	}
	now.Store(time.Unix(0, 0).Add(time.Minute))
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LastSpawnErr: "boom"})
	if got := rec.codes(); len(got) != 1 || got[0] != CodePoolStuck {
		t.Fatalf("codes=%v", got)
	}
}

func TestPoolStuckDoesNotFireWhileInFlight(t *testing.T) {
	rec := &recordingNotifier{}
	var now atomic.Value
	now.Store(time.Unix(0, 0))
	m := NewMonitor(MonitorOptions{
		Notifier:   rec,
		StuckAfter: time.Minute,
		Now:        func() time.Time { return now.Load().(time.Time) },
	})
	m.Evaluate(context.Background(), PoolState{Active: 1, PoolSize: 1})
	now.Store(time.Unix(0, 0).Add(2 * time.Minute))
	m.Evaluate(context.Background(), PoolState{Active: 1, PoolSize: 1})
	if len(rec.codes()) != 0 {
		t.Fatalf("in-flight should not alert: %v", rec.codes())
	}
	m.Evaluate(context.Background(), PoolState{Active: 0, PoolSize: 1, LastSpawnErr: "late fail"})
	if got := rec.codes(); len(got) != 1 || got[0] != CodePoolStuck {
		t.Fatalf("codes=%v", got)
	}
}
