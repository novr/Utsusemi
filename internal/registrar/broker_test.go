package registrar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

func TestBrokerRegistrarJIT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/jitconfig" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"encoded_jit_config": "jit",
			"runner":             map[string]any{"id": 9, "name": "utsusemi-x"},
		})
	}))
	defer server.Close()

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, "host-jwt")
	cfg := &config.Config{
		Registration: config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: server.URL,
		},
	}
	config.ApplyDefaults(cfg)

	reg := NewBrokerRegistrar(store, cfg)
	jit, err := reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "utsusemi-x")
	if err != nil {
		t.Fatal(err)
	}
	if jit.Encoded != "jit" {
		t.Fatalf("jit=%q", jit.Encoded)
	}
}

func TestBrokerRegistrarListRunners(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/runners/list" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"runners": []map[string]any{{"id": 3, "name": "utsusemi-abc"}},
		})
	}))
	defer server.Close()

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, "host-jwt")
	cfg := &config.Config{
		Registration: config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: server.URL,
		},
	}
	config.ApplyDefaults(cfg)

	reg := NewBrokerRegistrar(store, cfg)
	runners, err := reg.ListRunners(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, "utsusemi-")
	if err != nil {
		t.Fatal(err)
	}
	if len(runners) != 1 || runners[0].ID != 3 {
		t.Fatalf("runners=%+v", runners)
	}
}

func TestBrokerRegistrarRetryOnRateLimit(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"encoded_jit_config": "jit",
			"runner":             map[string]any{"id": 1, "name": "n"},
		})
	}))
	defer server.Close()

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, "host-jwt")
	cfg := &config.Config{
		Registration: config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: server.URL,
		},
	}
	config.ApplyDefaults(cfg)

	reg := NewBrokerRegistrar(store, cfg)
	_, err := reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if attempts < 2 {
		t.Fatalf("expected retry, attempts=%d", attempts)
	}
}

func TestBrokerRegistrarUnauthorizedSuggestsReregister(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("unauthorized"))
	}))
	defer server.Close()

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, "expired-jwt")
	cfg := &config.Config{
		Registration: config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: server.URL,
		},
	}
	config.ApplyDefaults(cfg)

	reg := NewBrokerRegistrar(store, cfg)
	_, err := reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "utsusemi configure app") {
		t.Fatalf("error=%v", err)
	}
}
