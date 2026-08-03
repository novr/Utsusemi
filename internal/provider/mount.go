package provider

import (
	"fmt"
	"os"
	"strings"
)

func hasTildeHome(m string) bool {
	return strings.HasPrefix(m, "~/") || strings.Contains(m, ":~/")
}

func expandTilde(mount, home string) string {
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
	var (
		home    string
		homeSet bool
	)
	getHome := func() (string, error) {
		if homeSet {
			return home, nil
		}
		var err error
		home, err = os.UserHomeDir()
		homeSet = true
		if err != nil {
			return "", fmt.Errorf("resolve home directory for mounts: %w", err)
		}
		return home, nil
	}

	out := make([]string, 0, len(mounts))
	for _, m := range mounts {
		m = strings.TrimSpace(m)
		if m == "" {
			continue
		}
		if hasTildeHome(m) {
			h, err := getHome()
			if err != nil {
				return nil, err
			}
			m = expandTilde(m, h)
		}
		out = append(out, m)
	}
	return out, nil
}

// HostPathFromDir extracts the host filesystem path from a resolved tart --dir value.
func HostPathFromDir(dir string) string {
	path := dir
	if i := strings.Index(path, ":"); i >= 0 {
		if candidate := path[i+1:]; strings.HasPrefix(candidate, "/") {
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
