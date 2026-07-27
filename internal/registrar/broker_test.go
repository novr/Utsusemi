package registrar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, "api-key")
	cfg := &config.Config{
		Registration: config.Registration{
			Mode:      config.ModeOwnApp,
			BrokerURL: server.URL,
		},
	}
	config.ApplyDefaults(cfg)

	reg := NewBrokerRegistrar(store, cfg)
	jit, err := reg.CreateJIT(context.Background(), target.Target{Type: target.TypeRepo, Owner: "a", Repo: "b"}, []string{"self-hosted"}, "utsusemi-x")
	if err != nil {
		t.Fatal(err)
	}
	if jit.Encoded != "jit" {
		t.Fatalf("jit=%q", jit.Encoded)
	}
}
