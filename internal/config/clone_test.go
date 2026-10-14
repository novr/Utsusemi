package config

import "testing"

func TestCloneNil(t *testing.T) {
	if Clone(nil) != nil {
		t.Fatal("Clone(nil) want nil")
	}
}

func TestCloneIndependentSlices(t *testing.T) {
	cfg := &Config{
		BaseImage: "img:1",
		PoolSize:  2,
		Labels:    []string{"a", "b"},
		Mounts:    []string{"/tmp/x"},
	}
	got := Clone(cfg)
	if got == cfg {
		t.Fatal("Clone returned same pointer")
	}
	cfg.BaseImage = "img:2"
	cfg.PoolSize = 9
	cfg.Labels[0] = "mutated"
	cfg.Mounts[0] = "/tmp/y"
	if got.BaseImage != "img:1" || got.PoolSize != 2 {
		t.Fatalf("value fields leaked: %+v", got)
	}
	if got.Labels[0] != "a" || got.Mounts[0] != "/tmp/x" {
		t.Fatalf("slice fields leaked: labels=%v mounts=%v", got.Labels, got.Mounts)
	}
}

func TestCloneNilSlicesStayNil(t *testing.T) {
	got := Clone(&Config{BaseImage: "img:1"})
	if got.Labels != nil || got.Mounts != nil {
		t.Fatalf("nil slices: labels=%v mounts=%v", got.Labels, got.Mounts)
	}
}
