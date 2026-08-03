package provider

import (
	"fmt"
	"os"
	"strings"
)

func mountNeedsHome(m string) bool {
	return strings.HasPrefix(m, "~/") || strings.Contains(m, ":~/")
}

func expandMountDir(mount, home string) string {
	if home == "" {
		return mount
	}
	if strings.HasPrefix(mount, "~/") {
		return home + mount[1:]
	}
	if i := strings.Index(mount, ":~/"); i >= 0 {
		return mount[:i+1] + home + mount[i+2:]
	}
	return mount
}

// ResolveMountDirs expands ~/ in mount entries for tart --dir flags.
func ResolveMountDirs(mounts []string) ([]string, error) {
	needsHome := false
	cleaned := make([]string, 0, len(mounts))
	for _, m := range mounts {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		cleaned = append(cleaned, m)
		if mountNeedsHome(m) {
			needsHome = true
		}
	}
	home := ""
	if needsHome {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory for mounts: %w", err)
		}
	}
	out := make([]string, 0, len(cleaned))
	for _, m := range cleaned {
		out = append(out, expandMountDir(m, home))
	}
	return out, nil
}

// MountHostPath returns the host filesystem path from a resolved tart --dir value.
func MountHostPath(dir string) string {
	path := dir
	if i := strings.Index(path, ":"); i >= 0 {
		candidate := path[i+1:]
		if strings.HasPrefix(candidate, "/") {
			path = candidate
		}
	}
	if i := strings.LastIndex(path, ":"); i > 0 {
		opt := path[i+1:]
		if opt == "ro" || strings.HasPrefix(opt, "tag=") {
			path = path[:i]
		}
	}
	return path
}
