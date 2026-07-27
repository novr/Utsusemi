package registrar

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

func TestGitHubPATRegistrarCreateJITOrg(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method %s", r.Method)
		}
		if r.URL.Path != "/orgs/my-org/actions/runners/generate-jitconfig" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"encoded_jit_config": "jit-token",
			"runner":             map[string]any{"id": 42, "name": "utsusemi-abc"},
		})
	}))
	defer server.Close()

	store := keychain.NewMemoryStore()
	_ = store.Set("svc", "acct", "pat")
	reg := &GitHubPATRegistrar{
		client:  server.Client(),
		store:   store,
		service: "svc",
		account: "acct",
	}

	tgt := target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}
	jit, err := reg.createJITWithBase(context.Background(), server.URL, tgt, []string{"self-hosted"}, "utsusemi-abc")
	if err != nil {
		t.Fatal(err)
	}
	if jit.Encoded != "jit-token" || jit.Runner.ID != 42 {
		t.Fatalf("unexpected jit: %+v", jit)
	}
}

func TestGitHubPATRegistrarRetryOnRateLimit(t *testing.T) {
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
	_ = store.Set("svc", "acct", "pat")
	reg := &GitHubPATRegistrar{client: server.Client(), store: store, service: "svc", account: "acct"}
	tgt := target.Target{Type: target.TypeRepo, Owner: "alice", Repo: "app"}
	_, err := reg.createJITWithBase(context.Background(), server.URL, tgt, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if attempts < 2 {
		t.Fatalf("expected retry, attempts=%d", attempts)
	}
}
