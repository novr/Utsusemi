package hostcredential

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/target"
)

func testJWT(exp time.Time) string {
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"EdDSA"}`))
	payload, _ := json.Marshal(map[string]int64{"exp": exp.Unix()})
	payloadB64 := base64.RawURLEncoding.EncodeToString(payload)
	return header + "." + payloadB64 + ".sig"
}

func TestLoadBundle(t *testing.T) {
	raw, err := NewBundle("eyJ.a.b", "refresh-1", "octocat")
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(raw)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RefreshToken != "refresh-1" || loaded.HostJWT != "eyJ.a.b" || loaded.GitHubUser != "octocat" {
		t.Fatalf("loaded=%+v", loaded)
	}
}

func TestLoadRejectsLegacyJWT(t *testing.T) {
	jwt := testJWT(time.Now().Add(30 * 24 * time.Hour))
	if _, err := Load(jwt); err == nil {
		t.Fatal("expected error for legacy jwt")
	}
}

func TestLoadRejectsUnknownFormat(t *testing.T) {
	if _, err := Load("host-jwt"); err == nil {
		t.Fatal("expected error")
	}
}

func TestNeedsRefreshThreshold(t *testing.T) {
	fresh := testJWT(time.Now().Add(30 * 24 * time.Hour))
	needs, err := NeedsRefresh(fresh, false)
	if err != nil || needs {
		t.Fatalf("needs=%v err=%v", needs, err)
	}

	stale := testJWT(time.Now().Add(48 * time.Hour))
	needs, err = NeedsRefresh(stale, false)
	if err != nil || !needs {
		t.Fatalf("needs=%v err=%v", needs, err)
	}

	expired := testJWT(time.Now().Add(-time.Hour))
	needs, err = NeedsRefresh(expired, false)
	if err != nil || !needs {
		t.Fatalf("needs=%v err=%v", needs, err)
	}
}

func TestRefreshGitHubToken(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Fatalf("grant_type=%q", r.Form.Get("grant_type"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    28800,
		})
	}))
	defer server.Close()

	client := &OAuthClient{TokenURL: server.URL}
	result, err := client.RefreshGitHubToken(t.Context(), PublicAppClientID, "old-refresh")
	if err != nil {
		t.Fatal(err)
	}
	if result.AccessToken != "new-access" || result.RefreshToken != "new-refresh" {
		t.Fatalf("result=%+v", result)
	}
}

func TestRefreshGitHubTokenInvalidGrant(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error":             "invalid_grant",
			"error_description": "bad refresh token",
		})
	}))
	defer server.Close()

	client := &OAuthClient{TokenURL: server.URL}
	_, err := client.RefreshGitHubToken(t.Context(), PublicAppClientID, "bad")
	if err == nil || !strings.Contains(err.Error(), "configure app") {
		t.Fatalf("error=%v", err)
	}
}

func TestExchangeHostJWT(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/register/exchange" {
			t.Fatalf("path=%s", r.URL.Path)
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer user-token") {
			t.Fatalf("auth=%q", r.Header.Get("Authorization"))
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"credential": "host-jwt",
			"target": map[string]any{
				"type":            "org",
				"org":             "my-org",
				"runner_group_id": float64(1),
			},
		})
	}))
	defer server.Close()

	credential, confirmed, err := ExchangeHostJWT(
		t.Context(),
		server.Client(),
		server.URL,
		"user-token",
		target.Target{Type: target.TypeOrg, Org: "my-org", RunnerGroupID: 1},
	)
	if err != nil {
		t.Fatal(err)
	}
	if credential != "host-jwt" || confirmed.Org != "my-org" {
		t.Fatalf("credential=%q target=%+v", credential, confirmed)
	}
}

func TestFetchGitHubUserLogin(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"login": "octocat"})
	}))
	defer server.Close()

	login, err := fetchGitHubUserLogin(t.Context(), server.Client(), "token", server.URL+"/user")
	if err != nil {
		t.Fatal(err)
	}
	if login != "octocat" {
		t.Fatalf("login=%q", login)
	}
}

func TestDescribeBundle(t *testing.T) {
	jwt := testJWT(time.Now().Add(30 * 24 * time.Hour))
	raw, err := NewBundle(jwt, "refresh", "octocat")
	if err != nil {
		t.Fatal(err)
	}
	status, err := Describe(raw)
	if err != nil {
		t.Fatal(err)
	}
	if status.GitHubUser != "octocat" {
		t.Fatalf("status=%+v", status)
	}
	if status.HostJWTExpiresIn <= 0 {
		t.Fatalf("remaining=%s", status.HostJWTExpiresIn)
	}
}

func TestRefreshGitHubTokenForm(t *testing.T) {
	var form url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		form = r.Form
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "a",
			"refresh_token": "b",
		})
	}))
	defer server.Close()

	client := &OAuthClient{TokenURL: server.URL}
	if _, err := client.RefreshGitHubToken(t.Context(), PublicAppClientID, "rt"); err != nil {
		t.Fatal(err)
	}
	if form.Get("client_secret") != "" {
		t.Fatalf("client_secret should be empty, got %q", form.Get("client_secret"))
	}
}
