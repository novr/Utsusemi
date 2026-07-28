package hostcredential

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/novr/utsusemi/internal/target"
)

const (
	PublicAppClientID = "Iv23ctWrJ3Yq0JDLEa85"
	BundleVersion     = 1
	RefreshThreshold  = 7 * 24 * time.Hour
)

type Bundle struct {
	V            int    `json:"v"`
	HostJWT      string `json:"host_jwt"`
	RefreshToken string `json:"refresh_token"`
	GitHubUser   string `json:"github_user"`
}

type Loaded struct {
	HostJWT      string
	RefreshToken string
	GitHubUser   string
}

func Load(raw string) (Loaded, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Loaded{}, fmt.Errorf("empty credential")
	}
	if strings.HasPrefix(raw, "{") {
		var b Bundle
		if err := json.Unmarshal([]byte(raw), &b); err != nil {
			return Loaded{}, fmt.Errorf("parse credential bundle: %w", err)
		}
		if b.V != BundleVersion {
			return Loaded{}, fmt.Errorf("unsupported credential bundle version %d", b.V)
		}
		if b.HostJWT == "" || b.RefreshToken == "" || b.GitHubUser == "" {
			return Loaded{}, fmt.Errorf("invalid credential bundle")
		}
		return Loaded{
			HostJWT:      b.HostJWT,
			RefreshToken: b.RefreshToken,
			GitHubUser:   b.GitHubUser,
		}, nil
	}
	return Loaded{}, fmt.Errorf("unrecognized credential format; run `utsusemi configure app`")
}

func MarshalBundle(b Bundle) (string, error) {
	b.V = BundleVersion
	data, err := json.Marshal(b)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func NewBundle(hostJWT, refreshToken, githubUser string) (string, error) {
	return MarshalBundle(Bundle{
		HostJWT:      hostJWT,
		RefreshToken: refreshToken,
		GitHubUser:   githubUser,
	})
}

type Status struct {
	GitHubUser       string
	HostJWTExpiresIn time.Duration
}

func Describe(raw string) (Status, error) {
	loaded, err := Load(raw)
	if err != nil {
		return Status{}, err
	}
	remaining, err := HostJWTExpiresIn(loaded.HostJWT)
	if err != nil {
		return Status{}, err
	}
	return Status{
		GitHubUser:       loaded.GitHubUser,
		HostJWTExpiresIn: remaining,
	}, nil
}

func HostJWTExpiresIn(jwt string) (time.Duration, error) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return 0, fmt.Errorf("invalid jwt")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return 0, fmt.Errorf("decode jwt payload: %w", err)
	}
	var claims struct {
		Exp int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return 0, fmt.Errorf("parse jwt payload: %w", err)
	}
	if claims.Exp == 0 {
		return 0, fmt.Errorf("jwt missing exp")
	}
	return time.Until(time.Unix(claims.Exp, 0)), nil
}

func NeedsRefresh(hostJWT string, force bool) (bool, error) {
	if force {
		return true, nil
	}
	remaining, err := HostJWTExpiresIn(hostJWT)
	if err != nil {
		return true, nil
	}
	return remaining <= RefreshThreshold, nil
}

func TargetPayload(tgt target.Target) map[string]any {
	if tgt.Type != target.TypeOrg {
		return map[string]any{}
	}
	return map[string]any{
		"type":            "org",
		"org":             tgt.Org,
		"runner_group_id": tgt.RunnerGroupID,
	}
}

func ParseTargetMap(raw map[string]any) (target.Target, error) {
	typ, _ := raw["type"].(string)
	if typ != "org" {
		return target.Target{}, fmt.Errorf("invalid target in response")
	}
	org, _ := raw["org"].(string)
	group, _ := raw["runner_group_id"].(float64)
	return target.Target{Type: target.TypeOrg, Org: org, RunnerGroupID: int64(group)}, nil
}
