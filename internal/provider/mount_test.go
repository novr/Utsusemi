package provider

import (
	"path/filepath"
	"testing"
)

func TestResolveMountDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "expands tilde and tagged form",
			in:   []string{"~/cache", "~/toolchains:ro", "share:~/named:ro"},
			want: []string{
				filepath.Join(home, "cache"),
				filepath.Join(home, "toolchains:ro"),
				"share:" + filepath.Join(home, "named:ro"),
			},
		},
		{
			name: "skips empty",
			in:   []string{"", "  ", "~/cache"},
			want: []string{filepath.Join(home, "cache")},
		},
		{
			name: "passes absolute paths",
			in:   []string{"/abs/path", "/abs/path:ro"},
			want: []string{"/abs/path", "/abs/path:ro"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ResolveMountDirs(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Fatalf("got[%d] = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestHostPathFromDir(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/Users/test/cache", "/Users/test/cache"},
		{"/Users/test/cache:ro", "/Users/test/cache"},
		{"share:/Users/test/cache:ro", "/Users/test/cache"},
		{"/Users/test/cache:tag=foo", "/Users/test/cache"},
	}
	for _, tc := range tests {
		if got := HostPathFromDir(tc.in); got != tc.want {
			t.Errorf("HostPathFromDir(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
