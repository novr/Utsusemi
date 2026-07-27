package config

import (
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

func TestDurationYAMLRoundTrip(t *testing.T) {
	type sample struct {
		Interval Duration `yaml:"interval"`
	}
	in := sample{Interval: Duration(30 * time.Second)}
	data, err := yaml.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	if got := string(data); got != "interval: 30s\n" {
		t.Fatalf("marshal = %q, want interval: 30s\\n", got)
	}

	var out sample
	if err := yaml.Unmarshal(data, &out); err != nil {
		t.Fatal(err)
	}
	if out.Interval != in.Interval {
		t.Fatalf("round-trip = %v, want %v", out.Interval, in.Interval)
	}
}

func TestDurationUnmarshalNanoseconds(t *testing.T) {
	var out struct {
		Interval Duration `yaml:"interval"`
	}
	if err := yaml.Unmarshal([]byte("interval: 30000000000\n"), &out); err != nil {
		t.Fatal(err)
	}
	if out.Interval != Duration(30*time.Second) {
		t.Fatalf("got %v, want 30s", time.Duration(out.Interval))
	}
}
