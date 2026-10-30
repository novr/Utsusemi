package registrar

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/keychain"
	"github.com/novr/utsusemi/internal/target"
)

const freshHostJWTExp = 9999999999 // far future; payload includes org target

func testFreshBundle(t *testing.T) string {
	t.Helper()
	bundle, err := hostcredential.NewBundle(makeExpiringJWT(time.Unix(freshHostJWTExp, 0)), "refresh-test", "octocat")
	if err != nil {
		t.Fatal(err)
	}
	return bundle
}

func testBrokerConfig(t *testing.T, serverURL string) *config.Config {
	t.Helper()
	cfg := &config.Config{
		Target: config.TargetYAML("my-org", "", 1),
		Registration: config.Registration{
			Mode:      config.ModeHostedApp,
			BrokerURL: serverURL,
		},
		StateDir: t.TempDir(),
	}
	config.ApplyDefaults(cfg)
	return cfg
}

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
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, testFreshBundle(t))
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, server.URL), nil)
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
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, testFreshBundle(t))
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, server.URL), nil)
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
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, testFreshBundle(t))
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, server.URL), nil)
	_, err := reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if attempts < 2 {
		t.Fatalf("expected retry, attempts=%d", attempts)
	}
}

func TestBrokerRegistrarUnauthorizedSuggestsReregister(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case hostcredential.CredentialExchangePath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"credential": makeExpiringJWT(time.Now().Add(30 * 24 * time.Hour)),
				"target": map[string]any{
					"type":            "org",
					"org":             "my-org",
					"runner_group_id": float64(1),
				},
			})
		case "/v1/jitconfig":
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte("unauthorized"))
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

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, testFreshBundle(t))
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, broker.URL), nil)
	reg.credentials.SetOAuth(&hostcredential.OAuthClient{TokenURL: oauth.URL})

	_, err := reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "utsusemi configure app") {
		t.Fatalf("error=%v", err)
	}
	if !strings.Contains(err.Error(), "octocat") {
		t.Fatalf("error=%v", err)
	}
}

func TestBrokerRegistrarRefreshBeforeExpiry(t *testing.T) {
	brokerCalls := 0
	oauthCalls := 0

	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		brokerCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"encoded_jit_config": "jit",
			"runner":             map[string]any{"id": 1, "name": "n"},
		})
	}))
	defer broker.Close()

	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oauthCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access",
			"refresh_token": "refresh-new",
		})
	}))
	defer oauth.Close()

	jwt := makeExpiringJWT(time.Now().Add(30 * 24 * time.Hour))
	bundle, err := hostcredential.NewBundle(jwt, "refresh-old", "octocat")
	if err != nil {
		t.Fatal(err)
	}

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, bundle)
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, broker.URL), nil)
	reg.credentials.SetOAuth(&hostcredential.OAuthClient{TokenURL: oauth.URL})

	_, err = reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if oauthCalls != 0 {
		t.Fatalf("oauth calls=%d", oauthCalls)
	}
	if brokerCalls != 1 {
		t.Fatalf("broker calls=%d", brokerCalls)
	}
}

func TestBrokerRegistrarRefreshWhenStale(t *testing.T) {
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case hostcredential.CredentialExchangePath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"credential": makeExpiringJWT(time.Now().Add(30 * 24 * time.Hour)),
				"target": map[string]any{
					"type":            "org",
					"org":             "my-org",
					"runner_group_id": float64(1),
				},
			})
		case "/v1/jitconfig":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"encoded_jit_config": "jit",
				"runner":             map[string]any{"id": 1, "name": "n"},
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

	jwt := makeExpiringJWT(time.Now().Add(48 * time.Hour))
	bundle, err := hostcredential.NewBundle(jwt, "refresh-old", "octocat")
	if err != nil {
		t.Fatal(err)
	}

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, bundle)
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, broker.URL), nil)
	reg.credentials.SetOAuth(&hostcredential.OAuthClient{TokenURL: oauth.URL})

	_, err = reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}

	updated, err := store.Get(config.DefaultCredentialService, config.DefaultCredentialAccount)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := hostcredential.Load(updated)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RefreshToken != "refresh-new" {
		t.Fatalf("refresh=%q", loaded.RefreshToken)
	}
}

func TestBrokerRegistrarUnauthorizedRefreshesOnce(t *testing.T) {
	attempts := 0
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case hostcredential.CredentialExchangePath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"credential": makeExpiringJWT(time.Now().Add(30 * 24 * time.Hour)),
				"target": map[string]any{
					"type":            "org",
					"org":             "my-org",
					"runner_group_id": float64(1),
				},
			})
		case "/v1/jitconfig":
			attempts++
			if attempts == 1 {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte("unauthorized"))
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"encoded_jit_config": "jit",
				"runner":             map[string]any{"id": 1, "name": "n"},
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

	jwt := makeExpiringJWT(time.Now().Add(30 * 24 * time.Hour))
	bundle, err := hostcredential.NewBundle(jwt, "refresh-old", "octocat")
	if err != nil {
		t.Fatal(err)
	}

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, bundle)
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, broker.URL), nil)
	reg.credentials.SetOAuth(&hostcredential.OAuthClient{TokenURL: oauth.URL})

	_, err = reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 {
		t.Fatalf("attempts=%d", attempts)
	}
}

func TestBrokerRegistrarExchangeFailurePreservesRefreshToken(t *testing.T) {
	oauthCalls := 0
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case hostcredential.CredentialExchangePath:
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte("broker down"))
		case "/v1/jitconfig":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"encoded_jit_config": "jit",
				"runner":             map[string]any{"id": 1, "name": "n"},
			})
		default:
			t.Fatalf("path %s", r.URL.Path)
		}
	}))
	defer broker.Close()

	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oauthCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access",
			"refresh_token": "refresh-new",
		})
	}))
	defer oauth.Close()

	jwt := makeExpiringJWT(time.Now().Add(48 * time.Hour))
	bundle, err := hostcredential.NewBundle(jwt, "refresh-old", "octocat")
	if err != nil {
		t.Fatal(err)
	}

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, bundle)
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, broker.URL), nil)
	reg.credentials.SetOAuth(&hostcredential.OAuthClient{TokenURL: oauth.URL})

	_, err = reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err == nil {
		t.Fatal("expected exchange failure")
	}
	if oauthCalls != 1 {
		t.Fatalf("oauth calls=%d", oauthCalls)
	}

	updated, err := store.Get(config.DefaultCredentialService, config.DefaultCredentialAccount)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := hostcredential.Load(updated)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RefreshToken != "refresh-new" {
		t.Fatalf("refresh=%q", loaded.RefreshToken)
	}

	broker.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case hostcredential.CredentialExchangePath:
			_ = json.NewEncoder(w).Encode(map[string]any{
				"credential": makeExpiringJWT(time.Now().Add(30 * 24 * time.Hour)),
				"target": map[string]any{
					"type":            "org",
					"org":             "my-org",
					"runner_group_id": float64(1),
				},
			})
		case "/v1/jitconfig":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"encoded_jit_config": "jit",
				"runner":             map[string]any{"id": 1, "name": "n"},
			})
		default:
			t.Fatalf("path %s", r.URL.Path)
		}
	})

	_, err = reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err != nil {
		t.Fatal(err)
	}
	if oauthCalls != 2 {
		t.Fatalf("oauth calls=%d", oauthCalls)
	}
}

func TestBrokerRegistrarExchangeNotFoundNoRetryLoop(t *testing.T) {
	oauthCalls := 0
	broker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case hostcredential.CredentialExchangePath:
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("installation not found"))
		case "/v1/jitconfig":
			t.Fatal("jitconfig should not be called")
		default:
			t.Fatalf("path %s", r.URL.Path)
		}
	}))
	defer broker.Close()

	oauth := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oauthCalls++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "access",
			"refresh_token": "refresh-new",
		})
	}))
	defer oauth.Close()

	jwt := makeExpiringJWT(time.Now().Add(48 * time.Hour))
	bundle, err := hostcredential.NewBundle(jwt, "refresh-old", "octocat")
	if err != nil {
		t.Fatal(err)
	}

	store := keychain.NewMemoryStore()
	_ = store.Set(config.DefaultCredentialService, config.DefaultCredentialAccount, bundle)
	reg := NewBrokerRegistrar(store, testBrokerConfig(t, broker.URL), nil)
	reg.credentials.SetOAuth(&hostcredential.OAuthClient{TokenURL: oauth.URL})

	_, err = reg.CreateJIT(context.Background(), target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1}, []string{"self-hosted"}, "n")
	if err == nil {
		t.Fatal("expected error")
	}
	if oauthCalls != 1 {
		t.Fatalf("oauth calls=%d", oauthCalls)
	}
}

func TestBrokerValidateCredentialMissing(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	cfg := testBrokerConfig(t, server.URL)
	cfg.Registration.CredentialKeychainService = "repo-a"
	reg := NewBrokerRegistrar(keychain.NewMemoryStore(), cfg, nil)
	cfg.Registration.CredentialKeychainService = "later"
	err := reg.ValidateCredential(context.Background(), "", "")
	if !errors.Is(err, keychain.ErrNotFound) {
		t.Fatalf("err=%v", err)
	}
	msg := err.Error()
	if strings.Contains(msg, "credential missing from keychain:") || strings.Contains(msg, "credential not found") {
		t.Fatalf("msg=%q", msg)
	}
	if !strings.Contains(msg, `"repo-a"`) || strings.Contains(msg, `"later"`) || !strings.Contains(msg, "utsusemi configure app") {
		t.Fatalf("msg=%q", msg)
	}
}

func TestBrokerValidateCredentialKeepsOtherKeychainErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected HTTP %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	locked := errors.New("keychain get: locked")
	cfg := testBrokerConfig(t, server.URL)
	reg := NewBrokerRegistrar(keychainErrStore{err: locked}, cfg, nil)
	err := reg.ValidateCredential(context.Background(), "", "")
	if err == nil || errors.Is(err, keychain.ErrNotFound) || strings.Contains(err.Error(), "configure app") {
		t.Fatalf("err=%v", err)
	}
	if !errors.Is(err, locked) {
		t.Fatalf("err=%v", err)
	}
}

func makeExpiringJWT(exp time.Time) string {
	payload, _ := json.Marshal(map[string]any{
		"exp": exp.Unix(),
		"target": map[string]any{
			"type":            "org",
			"org":             "my-org",
			"runner_group_id": int64(1),
		},
	})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	return "eyJhbGciOiJFUzI1NiJ9." + payloadB64 + ".sig"
}
