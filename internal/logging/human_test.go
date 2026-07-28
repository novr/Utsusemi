package logging

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestHumanHandlerFormat(t *testing.T) {
	var buf bytes.Buffer
	handler := redactingHandler{inner: newHumanHandler(&buf, slog.LevelInfo)}
	logger := slog.New(handler)

	logger.Info("syncing base image", "image", "ghcr.io/example:latest", "note", "first download can take several minutes")

	out := buf.String()
	for _, want := range []string{
		"INFO ",
		"syncing base image",
		"image=ghcr.io/example:latest",
		`note="first download can take several minutes"`,
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
	if strings.Contains(out, "time=") || strings.Contains(out, "msg=") {
		t.Fatalf("unexpected slog text format: %q", out)
	}
}

func TestLinePrefixWriter(t *testing.T) {
	var buf bytes.Buffer
	w := &linePrefixWriter{dst: &buf, prefix: []byte("  | ")}
	if _, err := w.Write([]byte("pulling manifest...\n\nimage ready\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	want := "  | pulling manifest...\n  | image ready\n"
	if buf.String() != want {
		t.Fatalf("got %q want %q", buf.String(), want)
	}
}

func TestSlogLineWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(redactingHandler{inner: newHumanHandler(&buf, slog.LevelInfo)}).With("component", "subprocess")
	w := &slogLineWriter{logger: logger}
	if _, err := w.Write([]byte("pulling manifest...\n")); err != nil {
		t.Fatal(err)
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"pulling manifest...", "component=subprocess"} {
		if !strings.Contains(out, want) {
			t.Fatalf("output %q missing %q", out, want)
		}
	}
}

func TestFormatAttrSlice(t *testing.T) {
	got := formatAttr(slog.Any("labels", []string{"self-hosted", "macOS"}))
	if got != `labels="[self-hosted macOS]"` {
		t.Fatalf("got %q", got)
	}
}

func TestHumanHandlerRedactsAttrs(t *testing.T) {
	var buf bytes.Buffer
	handler := redactingHandler{inner: newHumanHandler(&buf, slog.LevelInfo)}
	record := slog.NewRecord(time.Now(), slog.LevelInfo, "authorized", 0)
	record.AddAttrs(slog.String("token", "ghp_abcdefghijklmnopqrstuvwxyz1234567890"))
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "ghp_") {
		t.Fatalf("expected redaction, got %q", buf.String())
	}
}
