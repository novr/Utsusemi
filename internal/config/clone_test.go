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
		Env:       map[string]string{"HTTP_PROXY": "http://proxy:8080"},
		SystemProxy: &SystemProxy{
			HTTP:   "192.168.64.1:8888",
			Bypass: []string{"localhost"},
		},
	}
	got := Clone(cfg)
	if got == cfg {
		t.Fatal("Clone returned same pointer")
	}
	cfg.BaseImage = "img:2"
	cfg.PoolSize = 9
	cfg.Labels[0] = "mutated"
	cfg.Mounts[0] = "/tmp/y"
	cfg.Env["HTTP_PROXY"] = "mutated"
	cfg.SystemProxy.HTTP = "10.0.0.1:9"
	cfg.SystemProxy.Bypass[0] = "mutated"
	if got.BaseImage != "img:1" || got.PoolSize != 2 {
		t.Fatalf("value fields leaked: %+v", got)
	}
	if got.Labels[0] != "a" || got.Mounts[0] != "/tmp/x" {
		t.Fatalf("slice fields leaked: labels=%v mounts=%v", got.Labels, got.Mounts)
	}
	if got.Env["HTTP_PROXY"] != "http://proxy:8080" {
		t.Fatalf("env map leaked: %v", got.Env)
	}
	if got.SystemProxy == cfg.SystemProxy || got.SystemProxy.HTTP != "192.168.64.1:8888" || got.SystemProxy.Bypass[0] != "localhost" {
		t.Fatalf("system proxy leaked: %+v", got.SystemProxy)
	}
}

func TestCloneNilSlicesStayNil(t *testing.T) {
	got := Clone(&Config{BaseImage: "img:1"})
	if got.Labels != nil || got.Mounts != nil || got.Env != nil || got.SystemProxy != nil {
		t.Fatalf("nil slices/maps: labels=%v mounts=%v env=%v proxy=%v", got.Labels, got.Mounts, got.Env, got.SystemProxy)
	}
}
