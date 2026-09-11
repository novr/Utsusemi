package brokerhttp

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/novr/utsusemi/internal/target"
)

type jwtClaims struct {
	Iss            string         `json:"iss"`
	Sub            string         `json:"sub"`
	InstallationID int64          `json:"installation_id"`
	Target         map[string]any `json:"target"`
	Iat            int64          `json:"iat"`
	Nbf            int64          `json:"nbf"`
	Exp            int64          `json:"exp"`
	Jti            string         `json:"jti"`
	Ver            string         `json:"ver"`
}

func GenerateSigningKeyPEM() (string, error) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", err
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), nil
}

func parseEd25519PrivateKey(pemData string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(pemData))
	if block == nil {
		return nil, fmt.Errorf("credential signing key is invalid")
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("credential signing key is invalid")
	}
	priv, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("credential signing key is invalid")
	}
	return priv, nil
}

func issueHostJWT(env Env, installationID int64, tgt orgTarget) (string, error) {
	if strings.TrimSpace(env.SigningKeyPEM) == "" {
		return "", &httpError{status: http.StatusInternalServerError, message: "credential signing key is not configured"}
	}
	priv, err := parseEd25519PrivateKey(env.SigningKeyPEM)
	if err != nil {
		return "", &httpError{status: http.StatusInternalServerError, message: "credential signing key is invalid"}
	}
	now := time.Now().Unix()
	claims := jwtClaims{
		Iss:            env.JWTIssuer,
		Sub:            randomID(),
		InstallationID: installationID,
		Target: map[string]any{
			"type":            "org",
			"org":             tgt.Org,
			"runner_group_id": tgt.RunnerGroupID,
		},
		Iat: now,
		Nbf: now,
		Exp: now + 30*24*60*60,
		Jti: randomID(),
		Ver: env.JWTVersion,
	}
	return signEdDSA(priv, claims)
}

func verifyHostJWT(env Env, token string) (jwtClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return jwtClaims{}, unauthorized()
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return jwtClaims{}, unauthorized()
	}
	var header struct {
		Alg string `json:"alg"`
	}
	if err := json.Unmarshal(headerJSON, &header); err != nil || header.Alg != "EdDSA" {
		return jwtClaims{}, unauthorized()
	}
	priv, err := parseEd25519PrivateKey(env.SigningKeyPEM)
	if err != nil {
		return jwtClaims{}, unauthorized()
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return jwtClaims{}, unauthorized()
	}
	if !ed25519.Verify(priv.Public().(ed25519.PublicKey), []byte(parts[0]+"."+parts[1]), sig) {
		return jwtClaims{}, unauthorized()
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return jwtClaims{}, unauthorized()
	}
	var claims jwtClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return jwtClaims{}, unauthorized()
	}
	if claims.Iss != env.JWTIssuer || claims.Ver != env.JWTVersion {
		return jwtClaims{}, unauthorized()
	}
	now := time.Now().Unix()
	if claims.Nbf > now || claims.Exp <= now {
		return jwtClaims{}, unauthorized()
	}
	return claims, nil
}

func signEdDSA(priv ed25519.PrivateKey, claims jwtClaims) (string, error) {
	header, err := json.Marshal(map[string]string{"alg": "EdDSA", "typ": "JWT"})
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	data := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(payload)
	sig := ed25519.Sign(priv, []byte(data))
	return data + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func claimsTarget(claims jwtClaims) (orgTarget, error) {
	return parseOrgTarget(claims.Target)
}

func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return fmt.Sprintf("%x", b[:])
}

func bearerToken(authorization string) (string, bool) {
	const prefix = "bearer "
	if len(authorization) < len(prefix) || !strings.EqualFold(authorization[:len(prefix)], prefix) {
		return "", false
	}
	token := strings.TrimSpace(authorization[len(prefix):])
	return token, token != ""
}

func hostCredentialFromAuth(env Env, authorization string) (int64, orgTarget, error) {
	token, ok := bearerToken(authorization)
	if !ok {
		return 0, orgTarget{}, unauthorized()
	}
	claims, err := verifyHostJWT(env, token)
	if err != nil {
		return 0, orgTarget{}, err
	}
	if claims.InstallationID <= 0 {
		return 0, orgTarget{}, unauthorized()
	}
	tgt, err := claimsTarget(claims)
	if err != nil {
		return 0, orgTarget{}, unauthorized()
	}
	return claims.InstallationID, tgt, nil
}

func targetKey(t orgTarget) string {
	return fmt.Sprintf("org:%s:%d", t.Org, t.RunnerGroupID)
}

func parseOrgTarget(raw any) (orgTarget, error) {
	m, ok := raw.(map[string]any)
	if !ok {
		return orgTarget{}, badRequest("invalid target")
	}
	typ, _ := m["type"].(string)
	if typ != string(target.TypeOrg) {
		return orgTarget{}, badRequest("invalid target")
	}
	org, _ := m["org"].(string)
	org = strings.ToLower(strings.TrimSpace(org))
	var group int64
	switch v := m["runner_group_id"].(type) {
	case float64:
		group = int64(v)
	case int64:
		group = v
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return orgTarget{}, badRequest("invalid target")
		}
		group = n
	default:
		return orgTarget{}, badRequest("invalid target")
	}
	if org == "" || group <= 0 {
		return orgTarget{}, badRequest("invalid target")
	}
	return orgTarget{Org: org, RunnerGroupID: group}, nil
}
