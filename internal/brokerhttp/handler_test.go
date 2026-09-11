package brokerhttp

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/registrar"
)

func TestParseListen(t *testing.T) {
	if _, err := ParseListen("0.0.0.0:8787"); err == nil {
		t.Fatal("expected error")
	}
	got, err := ParseListen("127.0.0.1:8787")
	if err != nil || got != "127.0.0.1:8787" {
		t.Fatalf("got %q %v", got, err)
	}
	if _, err := ParseListen("localhost:8787"); err == nil {
		t.Fatal("localhost listen must be rejected")
	}
}

func TestIsLoopbackBrokerURL(t *testing.T) {
	if !IsLoopbackBrokerURL("http://127.0.0.1:8787") {
		t.Fatal("127.0.0.1")
	}
	if IsLoopbackBrokerURL("https://utsusemi-broker.novrd.workers.dev") {
		t.Fatal("hosted")
	}
}

func TestCheckReachableFails(t *testing.T) {
	err := CheckReachable(context.Background(), "http://127.0.0.1:1")
	if err == nil {
		t.Fatal("expected unreachable")
	}
}

func TestCheckReachableSkipsHosted(t *testing.T) {
	if err := CheckReachable(context.Background(), "https://example.example"); err != nil {
		t.Fatal(err)
	}
}

func TestHandlerRoutes(t *testing.T) {
	gh := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/user/installations":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"installations": []map[string]any{{
					"id":      9,
					"app_id":  42,
					"account": map[string]any{"login": "my-org"},
				}},
			})
		case strings.HasPrefix(r.URL.Path, "/app/installations/") && strings.HasSuffix(r.URL.Path, "/access_tokens"):
			_ = json.NewEncoder(w).Encode(map[string]any{"token": "inst"})
		case r.URL.Path == "/orgs/my-org/actions/runners/generate-jitconfig":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"encoded_jit_config": "jit",
				"runner":             map[string]any{"id": 1, "name": "n"},
			})
		case r.URL.Path == "/orgs/my-org/actions/runners":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"runners": []map[string]any{{"id": 1, "name": "utsusemi-a"}},
			})
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/orgs/my-org/actions/runners/"):
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}))
	t.Cleanup(gh.Close)

	rsaPEM := mustRSA(t)
	signPEM, err := GenerateSigningKeyPEM()
	if err != nil {
		t.Fatal(err)
	}
	env := Env{
		GitHubAppID:         "42",
		GitHubAppPrivateKey: rsaPEM,
		SigningKeyPEM:       signPEM,
		JWTIssuer:           DefaultJWTIssuer,
		JWTVersion:          DefaultJWTVersion,
		GitHubAPI:           gh.URL,
		HTTPClient:          gh.Client(),
	}
	srv := httptest.NewServer(Handler(env))
	t.Cleanup(srv.Close)

	if err := CheckReachable(context.Background(), srv.URL); err != nil {
		t.Fatal(err)
	}

	t.Run("exchange unauthorized", func(t *testing.T) {
		resp := post(t, srv.URL+hostcredential.CredentialExchangePath, "", `{"target":{"type":"org","org":"my-org","runner_group_id":1}}`)
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("status %d", resp.StatusCode)
		}
	})

	t.Run("exchange repo rejected", func(t *testing.T) {
		resp := post(t, srv.URL+hostcredential.CredentialExchangePath, "user", `{"target":{"type":"repo","org":"my-org","runner_group_id":1}}`)
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status %d body %s", resp.StatusCode, resp.body)
		}
	})

	t.Run("exchange ok", func(t *testing.T) {
		resp := post(t, srv.URL+hostcredential.CredentialExchangePath, "user", `{"target":{"type":"org","org":"my-org","runner_group_id":1}}`)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status %d body %s", resp.StatusCode, resp.body)
		}
		var out struct {
			Credential string `json:"credential"`
		}
		if err := json.Unmarshal([]byte(resp.body), &out); err != nil || out.Credential == "" {
			t.Fatalf("body %s", resp.body)
		}

		t.Run("jit unauthorized", func(t *testing.T) {
			r := post(t, srv.URL+registrar.BrokerJITConfigPath, "", `{"target":{"type":"org","org":"my-org","runner_group_id":1},"name":"n","labels":["self-hosted"]}`)
			if r.StatusCode != http.StatusUnauthorized {
				t.Fatalf("status %d", r.StatusCode)
			}
		})

		t.Run("jit forbidden", func(t *testing.T) {
			r := post(t, srv.URL+registrar.BrokerJITConfigPath, out.Credential, `{"target":{"type":"org","org":"other","runner_group_id":1},"name":"n","labels":["self-hosted"]}`)
			if r.StatusCode != http.StatusForbidden {
				t.Fatalf("status %d", r.StatusCode)
			}
		})

		t.Run("jit ok", func(t *testing.T) {
			r := post(t, srv.URL+registrar.BrokerJITConfigPath, out.Credential, `{"target":{"type":"org","org":"my-org","runner_group_id":1},"name":"n","labels":["self-hosted"]}`)
			if r.StatusCode != http.StatusOK {
				t.Fatalf("status %d body %s", r.StatusCode, r.body)
			}
		})

		t.Run("list ok", func(t *testing.T) {
			r := post(t, srv.URL+registrar.BrokerRunnersListPath, out.Credential, `{"target":{"type":"org","org":"my-org","runner_group_id":1},"prefix":"utsusemi-"}`)
			if r.StatusCode != http.StatusOK {
				t.Fatalf("status %d body %s", r.StatusCode, r.body)
			}
		})

		t.Run("delete ok", func(t *testing.T) {
			req, err := http.NewRequest(http.MethodDelete, srv.URL+"/v1/runners/1", strings.NewReader(`{"target":{"type":"org","org":"my-org","runner_group_id":1}}`))
			if err != nil {
				t.Fatal(err)
			}
			req.Header.Set("Authorization", "Bearer "+out.Credential)
			req.Header.Set("Content-Type", "application/json")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("status %d", resp.StatusCode)
			}
		})
	})
}

type httpResp struct {
	StatusCode int
	body       string
}

func post(t *testing.T, url, token, body string) httpResp {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return httpResp{StatusCode: resp.StatusCode, body: string(b)}
}

func mustRSA(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der := x509.MarshalPKCS1PrivateKey(key)
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: der}))
}
