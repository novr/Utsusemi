package runnercache

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	MountName     = "utsusemi-runner-cache"
	GuestCacheDir = "/Volumes/My Shared Files/" + MountName
	subdir        = "runner-cache"
)

const (
	defaultBaseURL   = "https://github.com/actions/runner/releases/download"
	downloadTimeout  = 5 * time.Minute
	maxDownloadTries = 3
	retryDelay       = 2 * time.Second
)

// Dir returns the host directory that holds cached runner tarballs.
func Dir(stateDir string) string {
	return filepath.Join(stateDir, subdir)
}

// TarballName is the on-disk filename for a runner release asset.
func TarballName(arch, version string) string {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	arch = strings.TrimSpace(arch)
	return fmt.Sprintf("actions-runner-%s-%s.tar.gz", arch, version)
}

// TarballPath is the absolute path of the cached tarball for version/arch.
func TarballPath(stateDir, arch, version string) string {
	return filepath.Join(Dir(stateDir), TarballName(arch, version))
}

// MountEntry returns a Tart --dir value that exposes the cache read-only.
// Empty stateDir yields "" (caller should skip injection).
func MountEntry(stateDir string) string {
	stateDir = strings.TrimSpace(stateDir)
	if stateDir == "" {
		return ""
	}
	dir, err := filepath.Abs(Dir(stateDir))
	if err != nil {
		dir = Dir(stateDir)
	}
	return fmt.Sprintf("%s:%s:ro", MountName, dir)
}

// WithRunnerCacheMount prepends the automatic runner-cache mount when stateDir is set.
// It creates the cache directory so Tart --dir always points at an existing path.
func WithRunnerCacheMount(stateDir string, mounts []string) []string {
	entry := MountEntry(stateDir)
	if entry == "" {
		return append([]string(nil), mounts...)
	}
	_ = PrepareDir(stateDir)
	out := make([]string, 0, len(mounts)+1)
	out = append(out, entry)
	out = append(out, mounts...)
	return out
}

// PrepareDir creates the cache directory so Tart can mount it even before Ensure succeeds.
func PrepareDir(stateDir string) error {
	if strings.TrimSpace(stateDir) == "" {
		return fmt.Errorf("state dir is empty")
	}
	return os.MkdirAll(Dir(stateDir), 0o755)
}

// Options configures Ensure downloads (tests inject BaseURL / HTTPClient).
type Options struct {
	HTTPClient *http.Client
	BaseURL    string
	Logger     *slog.Logger
}

// Ensure downloads the runner tarball into StateDir when missing.
func Ensure(ctx context.Context, stateDir, version, arch string, opts Options) error {
	version = strings.TrimPrefix(strings.TrimSpace(version), "v")
	arch = strings.TrimSpace(arch)
	if stateDir == "" || version == "" || arch == "" {
		return fmt.Errorf("stateDir, version, and arch are required")
	}
	if err := PrepareDir(stateDir); err != nil {
		return err
	}

	dest := TarballPath(stateDir, arch, version)
	if st, err := os.Stat(dest); err == nil && st.Size() > 0 {
		return nil
	}

	log := opts.Logger
	if log == nil {
		log = slog.Default()
	}
	baseURL := strings.TrimRight(opts.BaseURL, "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	url := fmt.Sprintf("%s/v%s/%s", baseURL, version, TarballName(arch, version))

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: downloadTimeout}
	}

	log.Info("caching runner tarball", "version", version, "arch", arch, "path", dest)
	var lastErr error
	for attempt := 1; attempt <= maxDownloadTries; attempt++ {
		lastErr = downloadOnce(ctx, client, url, dest)
		if lastErr == nil {
			if err := pruneOther(Dir(stateDir), filepath.Base(dest)); err != nil {
				log.Warn("prune old runner cache failed", "error", err)
			}
			log.Info("runner tarball cached", "version", version, "arch", arch)
			return nil
		}
		log.Warn("runner cache download failed", "attempt", attempt, "error", lastErr)
		if !retryable(lastErr) || attempt == maxDownloadTries {
			break
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(retryDelay):
		}
	}
	return lastErr
}

type httpStatusError struct {
	code int
	body string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.code, e.body)
}

func retryable(err error) bool {
	if err == nil {
		return false
	}
	var he *httpStatusError
	if errors.As(err, &he) {
		return he.code >= 500
	}
	return true // transport / I/O
}

func downloadOnce(ctx context.Context, client *http.Client, url, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "utsusemi")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return &httpStatusError{code: resp.StatusCode, body: strings.TrimSpace(string(body))}
	}

	tmp, err := os.CreateTemp(filepath.Dir(dest), "actions-runner-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		_ = os.Remove(tmpName)
	}()

	if _, err := io.Copy(tmp, resp.Body); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return err
	}
	return nil
}

func pruneOther(dir, keep string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if name == keep {
			continue
		}
		if !strings.HasPrefix(name, "actions-runner-") || !strings.HasSuffix(name, ".tar.gz") {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}
