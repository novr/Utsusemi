package hostcredential

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

func TestManagerEnsureFreshWhenValid(t *testing.T) {
	jwt := testJWT(time.Now().Add(30 * 24 * time.Hour))
	bundle, err := NewBundle(jwt, "refresh-old", "octocat")
	if err != nil {
		t.Fatal(err)
	}

	store := keychain.NewMemoryStore()
	_ = store.Set("svc", "acct", bundle)
	oauthCalls := 0
	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oauthCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "a", "refresh_token": "b"})
	}))
	defer oauth.Close()

	mgr := NewManager(ManagerOptions{
		Store:      store,
		Service:    "svc",
		Account:    "acct",
		BrokerURL:  "https://broker.example",
		LockPath:   t.TempDir() + "/credential.refresh.lock",
		HTTPClient: http.DefaultClient,
	})
	mgr.SetOAuth(&OAuthClient{TokenURL: oauth.URL})

	token, err := mgr.EnsureFresh(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, false)
	if err != nil {
		t.Fatal(err)
	}
	if token != jwt {
		t.Fatalf("token=%q", token)
	}
	if oauthCalls != 0 {
		t.Fatalf("oauth calls=%d", oauthCalls)
	}
}

func TestManagerEnsureFreshRefreshesStaleJWT(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case CredentialExchangePath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"credential": makeManagerTestJWT(time.Now().Add(30 * 24 * time.Hour)),
				"target": map[string]any{
					"type":            "org",
					"org":             "my-org",
					"runner_group_id": float64(1),
				},
			})
		default:
			t.Fatalf("path %s", r.URL.Path)
		}
	}))
	defer broker.Close()

	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access",
			"refresh_token": "refresh-new",
		})
	}))
	defer oauth.Close()

	jwt := makeManagerTestJWT(time.Now().Add(48 * time.Hour))
	bundle, err := NewBundle(jwt, "refresh-old", "octocat")
	if err != nil {
		t.Fatal(err)
	}

	store := keychain.NewMemoryStore()
	_ = store.Set("svc", "acct", bundle)
	mgr := NewManager(ManagerOptions{
		Store:      store,
		Service:    "svc",
		Account:    "acct",
		BrokerURL:  broker.URL,
		LockPath:   t.TempDir() + "/credential.refresh.lock",
		HTTPClient: broker.Client(),
	})
	mgr.SetOAuth(&OAuthClient{TokenURL: oauth.URL})

	_, err = mgr.EnsureFresh(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, false)
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.Get("svc", "acct")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(updated)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RefreshToken != "refresh-new" {
		t.Fatalf("refresh=%q", loaded.RefreshToken)
	}
}

func makeManagerTestJWT(exp time.Time) string {
	payload, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	return "eyJhbGciOiJFUzI1NiJ9." + payloadB64 + ".sig"
}
