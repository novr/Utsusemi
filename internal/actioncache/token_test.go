package actioncache

import (
	"encoding/base64"
	"encoding/json"
	"testing"
)

func TestRepoFromBearer(t *testing.T) {
	mk := func(claims map[string]any) string {
		h := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"none"}`))
		p, _ := json.Marshal(claims)
		return "Bearer " + h + "." + base64.RawURLEncoding.EncodeToString(p) + ".x"
	}
	if got := RepoFromBearer(mk(map[string]any{"repository": "acme/app"})); got != "acme/app" {
		t.Fatalf("repository = %q", got)
	}
	if got := RepoFromBearer(mk(map[string]any{"orchid": "acme.app.__default"})); got != "acme/app" {
		t.Fatalf("orchid = %q", got)
	}
	if got := RepoFromBearer(""); got != "default" {
		t.Fatalf("empty = %q", got)
	}
}
