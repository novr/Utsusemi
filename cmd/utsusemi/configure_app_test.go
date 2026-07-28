package main

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDeviceFlowTokenRequiresRefreshToken(t *testing.T) {
	body := []byte(`{"access_token":"access-only"}`)
	var tokenResult struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(body, &tokenResult); err != nil {
		t.Fatal(err)
	}
	err := validateDeviceFlowTokens(tokenResult.AccessToken, tokenResult.RefreshToken)
	if err == nil || !strings.Contains(err.Error(), "refresh token") {
		t.Fatalf("error=%v", err)
	}
}

func TestDeviceFlowTokenAcceptsRefreshToken(t *testing.T) {
	if err := validateDeviceFlowTokens("access", "refresh"); err != nil {
		t.Fatal(err)
	}
}
