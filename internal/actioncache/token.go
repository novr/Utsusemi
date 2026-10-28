package actioncache

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

// RepoFromBearer extracts a stable repo namespace from an Actions runtime
// token without verifying the signature. Collision avoidance only — jobs on
// the same host share one store and can forge claims; see README trust boundary.
func RepoFromBearer(authHeader string) string {
	raw := strings.TrimSpace(authHeader)
	if len(raw) >= 7 && strings.EqualFold(raw[:7], "Bearer ") {
		raw = strings.TrimSpace(raw[7:])
	}
	if raw == "" {
		return "default"
	}
	parts := strings.Split(raw, ".")
	if len(parts) < 2 {
		return "default"
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		payload, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return "default"
		}
	}
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "default"
	}
	for _, key := range []string{"repository", "repository_name", "repo"} {
		if v, ok := claims[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	if v, ok := claims["repository_id"]; ok {
		switch t := v.(type) {
		case string:
			if strings.TrimSpace(t) != "" {
				return "id:" + strings.TrimSpace(t)
			}
		case float64:
			return fmt.Sprintf("id:%d", int64(t))
		}
	}
	if orchid, ok := claims["orchid"].(string); ok && strings.TrimSpace(orchid) != "" {
		// orchid looks like "org.repo.__default" — take the first two segments when present.
		segs := strings.Split(orchid, ".")
		if len(segs) >= 2 && segs[0] != "" && segs[1] != "" {
			return segs[0] + "/" + segs[1]
		}
		return strings.TrimSpace(orchid)
	}
	if nameid, ok := claims["nameid"].(string); ok && strings.TrimSpace(nameid) != "" {
		return "nameid:" + strings.TrimSpace(nameid)
	}
	return "default"
}
