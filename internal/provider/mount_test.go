package provider

import "testing"

func TestExpandMountDir(t *testing.T) {
	home := "/Users/test"
	tests := []struct {
		in, want string
	}{
		{"~/cache", "/Users/test/cache"},
		{"~/toolchains:ro", "/Users/test/toolchains:ro"},
		{"share:~/cache:ro", "share:/Users/test/cache:ro"},
		{"/abs/path", "/abs/path"},
		{"/abs/path:ro", "/abs/path:ro"},
	}
	for _, tc := range tests {
		if got := expandMountDir(tc.in, home); got != tc.want {
			t.Errorf("expandMountDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestMountHostPath(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/Users/test/cache", "/Users/test/cache"},
		{"/Users/test/cache:ro", "/Users/test/cache"},
		{"share:/Users/test/cache:ro", "/Users/test/cache"},
		{"/Users/test/cache:tag=foo", "/Users/test/cache"},
	}
	for _, tc := range tests {
		if got := MountHostPath(tc.in); got != tc.want {
			t.Errorf("MountHostPath(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveMountDirsSkipsEmpty(t *testing.T) {
	dirs, err := ResolveMountDirs([]string{"", "  ", "~/cache"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) != 1 {
		t.Fatalf("dirs = %v, want one entry", dirs)
	}
	if !mountNeedsHome("~/cache") {
		t.Fatal("expected ~/cache to need home")
	}
}
