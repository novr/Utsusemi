package hostcredential

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDeviceFlowTokenRequiresRefreshToken(t *testing.T) {
	if err := validateDeviceFlowTokens("access-only", ""); err == nil || !strings.Contains(err.Error(), "refresh token") {
		t.Fatalf("error=%v", err)
	}
}

func TestDeviceFlowTokenAcceptsRefreshToken(t *testing.T) {
	if err := validateDeviceFlowTokens("access", "refresh"); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceFlowTokenParsesJSON(t *testing.T) {
	body := []byte(`{"access_token":"access-only"}`)
	var tokenResult struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &tokenResult); err != nil {
		t.Fatal(err)
	}
	if err := validateDeviceFlowTokens(tokenResult.AccessToken, tokenResult.RefreshToken); err == nil {
		t.Fatal("expected error")
	}
}

func TestDeviceFlowClientAuthorize(t *testing.T) {
	var tokenPolls atomic.Int32

	deviceCode := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"device_code":               "device-code",
			"user_code":                 "ABCD-1234",
			"verification_uri":          "https://github.com/login/device",
			"verification_uri_complete": "https://github.com/login/device?user_code=ABCD-1234",
			"expires_in":                60,
			"interval":                  0,
		})
	}))
	defer deviceCode.Close()

	token := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		switch tokenPolls.Add(1) {
		case 1:
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "authorization_pending"})
		default:
			_ = json.NewEncoder(w).Encode(map[string]string{
				"access_token":  "access-token",
				"refresh_token": "refresh-token",
			})
		}
	}))
	defer token.Close()

	client := &DeviceFlowClient{
		HTTPClient:    deviceCode.Client(),
		DeviceCodeURL: deviceCode.URL,
		TokenURL:      token.URL,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := client.Authorize(ctx, "client-id", DeviceFlowPrompt{})
	if err != nil {
		t.Fatal(err)
	}
	if result.AccessToken != "access-token" || result.RefreshToken != "refresh-token" {
		t.Fatalf("result=%+v", result)
	}
	if tokenPolls.Load() < 2 {
		t.Fatalf("token polls=%d", tokenPolls.Load())
	}
}
