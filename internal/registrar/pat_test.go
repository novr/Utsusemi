package registrar

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
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
		api:     &httpClient{client: server.Client()},
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

func TestJITConfigRequestRepoGroup(t *testing.T) {
	path, body, err := jitConfigRequest(target.Target{Type: target.TypeRepo, Owner: "alice", Repo: "app"}, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if path != "/repos/alice/app/actions/runners/generate-jitconfig" {
		t.Fatalf("path %s", path)
	}
	if body["runner_group_id"] != int64(1) {
		t.Fatalf("default group=%v", body["runner_group_id"])
	}

	_, body, err = jitConfigRequest(target.Target{Type: target.TypeRepo, Owner: "alice", Repo: "app", RunnerGroupID: 2}, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if body["runner_group_id"] != int64(2) {
		t.Fatalf("explicit group=%v", body["runner_group_id"])
	}
}

func TestJITConfigRequestOrgRequiresGroup(t *testing.T) {
	_, _, err := jitConfigRequest(target.Target{Type: target.TypeOrg, Org: "my-org"}, []string{"self-hosted"}, "n")
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestGitHubPATRegistrarCreateJITRepoIncludesRunnerGroup(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/repos/alice/app/actions/runners/generate-jitconfig" {
			t.Fatalf("path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["runner_group_id"] != float64(1) {
			t.Fatalf("body=%v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"encoded_jit_config": "jit",
			"runner":             map[string]any{"id": 7, "name": "n"},
		})
	}))
	defer server.Close()

	store := keychain.NewMemoryStore()
	_ = store.Set("svc", "acct", "pat")
	reg := &GitHubPATRegistrar{api: &httpClient{client: server.Client()}, store: store, service: "svc", account: "acct"}
	tgt := target.Target{Type: target.TypeRepo, Owner: "alice", Repo: "app"}
	jit, err := reg.createJITWithBase(context.Background(), server.URL, tgt, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if jit.Encoded != "jit" || jit.Runner.ID != 7 {
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
	reg := &GitHubPATRegistrar{api: &httpClient{client: server.Client()}, store: store, service: "svc", account: "acct"}
	tgt := target.Target{Type: target.TypeRepo, Owner: "alice", Repo: "app"}
	_, err := reg.createJITWithBase(context.Background(), server.URL, tgt, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if attempts < 2 {
		t.Fatalf("expected retry, attempts=%d", attempts)
	}
}

func TestGitHubPATRegistrarDeleteRunnerNotFoundOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Fatalf("method %s", r.Method)
		}
		if r.URL.Path != "/repos/alice/app/actions/runners/9" {
			t.Fatalf("path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"message":"Not Found"}`))
	}))
	defer server.Close()

	store := keychain.NewMemoryStore()
	_ = store.Set("svc", "acct", "pat")
	reg := &GitHubPATRegistrar{
		api:     &httpClient{client: server.Client(), baseURL: server.URL},
		store:   store,
		service: "svc",
		account: "acct",
	}
	err := reg.DeleteRunner(context.Background(), target.Target{Type: target.TypeRepo, Owner: "alice", Repo: "app"}, 9)
	if err != nil {
		t.Fatal(err)
	}
}

func TestGitHubPATValidateCredentialMissing(t *testing.T) {
	reg := NewGitHubPATRegistrar(keychain.NewMemoryStore(), "repo-a", "utsusemi")
	err := reg.ValidateCredential(context.Background(), "ignored", "ignored")
	if !errors.Is(err, keychain.ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	msg := err.Error()
	if !strings.Contains(msg, `"repo-a"`) || !strings.Contains(msg, "utsusemi configure token") || strings.Contains(msg, "credential not found") {
		t.Fatalf("msg=%q", msg)
	}
}

type keychainErrStore struct {
	err error
}

func (s keychainErrStore) Get(string, string) (string, error) { return "", s.err }
func (s keychainErrStore) Set(string, string, string) error   { return nil }
func (s keychainErrStore) Delete(string, string) error        { return nil }

func TestGitHubPATValidateCredentialKeepsOtherKeychainErrors(t *testing.T) {
	locked := errors.New("keychain get: locked")
	reg := NewGitHubPATRegistrar(keychainErrStore{err: locked}, "repo-a", "utsusemi")
	err := reg.ValidateCredential(context.Background(), "", "")
	if err == nil || errors.Is(err, keychain.ErrNotFound) || strings.Contains(err.Error(), "configure token") {
		t.Fatalf("err=%v", err)
	}
	if !errors.Is(err, locked) {
		t.Fatalf("err=%v", err)
	}
}
