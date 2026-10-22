package drainctl

import (
	"errors"
	"testing"
	"time"
)

func TestWriteLoadRemoveRequest(t *testing.T) {
	dir := t.TempDir()
	req := Request{
		RequestID:   "abc",
		AgentID:     "agent-1",
		RequestedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		Deadline:    time.Date(2026, 1, 2, 3, 34, 5, 0, time.UTC),
		Force:       true,
	}
	if err := WriteRequest(dir, req); err != nil {
		t.Fatal(err)
	}
	got, err := LoadRequest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestID != req.RequestID || got.AgentID != req.AgentID || !got.Force {
		t.Fatalf("got %+v", got)
	}
	if err := RemoveRequest(dir); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadRequest(dir); !errors.Is(err, ErrNoRequest) {
		t.Fatalf("want ErrNoRequest, got %v", err)
	}
}

func TestWriteLoadStatus(t *testing.T) {
	dir := t.TempDir()
	st := Status{
		RequestID: "abc",
		AgentID:   "agent-1",
		State:     StateRunning,
		Active:    2,
		Deadline:  time.Date(2026, 1, 2, 3, 34, 5, 0, time.UTC),
	}
	if err := WriteStatus(dir, st); err != nil {
		t.Fatal(err)
	}
	got, ok, err := LoadStatus(dir)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.RequestID != "abc" || got.Active != 2 || got.State != StateRunning {
		t.Fatalf("got %+v", got)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatal("UpdatedAt should be set")
	}
}

func TestShouldDiscardRequest(t *testing.T) {
	started := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		req  Request
		want bool
	}{
		{
			name: "match",
			req:  Request{AgentID: "a1", RequestedAt: started.Add(time.Minute)},
			want: false,
		},
		{
			name: "wrong agent",
			req:  Request{AgentID: "other", RequestedAt: started.Add(time.Minute)},
			want: true,
		},
		{
			name: "before session",
			req:  Request{AgentID: "a1", RequestedAt: started.Add(-time.Minute)},
			want: true,
		},
		{
			name: "empty agent",
			req:  Request{RequestedAt: started.Add(time.Minute)},
			want: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ShouldDiscardRequest(tc.req, "a1", started); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestMatchesAndTerminal(t *testing.T) {
	st := Status{RequestID: "x", State: StateComplete}
	if !MatchesRequest(st, "x") || MatchesRequest(st, "y") {
		t.Fatal("MatchesRequest")
	}
	if !IsTerminal(st) {
		t.Fatal("complete should be terminal")
	}
	if IsTerminal(Status{State: StateRunning}) {
		t.Fatal("running should not be terminal")
	}
}

func TestWriteRequestExclusive(t *testing.T) {
	dir := t.TempDir()
	if err := WriteRequest(dir, Request{RequestID: "1", AgentID: "a"}); err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(dir, Request{RequestID: "2", AgentID: "a"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
	got, err := LoadRequest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.RequestID != "1" {
		t.Fatalf("first request should remain, got %q", got.RequestID)
	}
	if err := RemoveRequest(dir); err != nil {
		t.Fatal(err)
	}
	if err := WriteRequest(dir, Request{RequestID: "2", AgentID: "a"}); err != nil {
		t.Fatal(err)
	}
}

func TestRemoveStatus(t *testing.T) {
	dir := t.TempDir()
	if err := WriteStatus(dir, Status{RequestID: "x", AgentID: "a", State: StateRunning}); err != nil {
		t.Fatal(err)
	}
	if err := RemoveStatus(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := LoadStatus(dir); err != nil || ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
}
