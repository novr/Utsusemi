package runnerrelease

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLatest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v2.340.0"}`))
	}))
	defer srv.Close()

	got, err := (&Client{HTTPClient: srv.Client(), URL: srv.URL}).Latest(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got != "2.340.0" {
		t.Fatalf("got %q, want 2.340.0", got)
	}
}

func TestOlder(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"2.335.0", "2.336.0", true},
		{"2.336.0", "2.336.0", false},
		{"2.337.0", "2.336.0", false},
		{"v2.340.0", "2.341.0", true},
	}
	for _, tc := range cases {
		if Older(tc.a, tc.b) != tc.want {
			t.Fatalf("Older(%q, %q) = %v, want %v", tc.a, tc.b, !tc.want, tc.want)
		}
	}
}
