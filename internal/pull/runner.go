package pull

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/novr/utsusemi/internal/runnercache"
	"github.com/novr/utsusemi/internal/runnerrelease"
)

// RunnerOptions caches one Actions runner tarball.
// Config is not modified. Latest resolves the "latest" argument.
type RunnerOptions struct {
	VersionArg          string
	StateDir            string
	Arch                string
	ConfigRunnerVersion string
	Latest              func(context.Context) (string, error)
	Logger              *slog.Logger
	Stderr              io.Writer
	Cache               runnercache.Options
}

// Runner downloads the tarball when it is missing.
// An existing file is left unchanged, and other versions are not removed.
func Runner(ctx context.Context, opts RunnerOptions) error {
	version, resolvedLatest, err := resolveRunnerVersion(ctx, opts.VersionArg, opts.Latest)
	if err != nil {
		return err
	}
	if strings.TrimSpace(opts.StateDir) == "" {
		return fmt.Errorf("state dir is required")
	}
	arch := strings.TrimSpace(opts.Arch)
	if arch == "" {
		arch = "osx-arm64"
	}
	out := stderrOf(opts.Stderr)
	if resolvedLatest {
		fmt.Fprintf(out, "runner version %s\n", version)
	}
	configVersion := stripRunnerVersionPrefix(opts.ConfigRunnerVersion)
	if configVersion != "" && configVersion != version {
		fmt.Fprintf(out, "next start still uses runner_version %s\n", strings.TrimSpace(opts.ConfigRunnerVersion))
	}

	cache := opts.Cache
	cache.SkipPrune = true
	if cache.Logger == nil {
		cache.Logger = opts.Logger
	}
	if err := runnercache.Ensure(ctx, opts.StateDir, version, arch, cache); err != nil {
		return fmt.Errorf("pull runner: %w", err)
	}
	return nil
}

func resolveRunnerVersion(ctx context.Context, arg string, latest func(context.Context) (string, error)) (version string, resolvedLatest bool, err error) {
	arg = strings.TrimSpace(arg)
	if strings.EqualFold(arg, "latest") {
		if latest == nil {
			latest = func(ctx context.Context) (string, error) {
				return runnerrelease.Latest(ctx, nil)
			}
		}
		resolved, err := latest(ctx)
		if err != nil {
			return "", false, fmt.Errorf("resolve runner version latest: %w", err)
		}
		version, err = concreteRunnerVersion(resolved)
		if err != nil {
			return "", false, fmt.Errorf("resolve runner version latest: %w", err)
		}
		return version, true, nil
	}
	version, err = concreteRunnerVersion(arg)
	if err != nil {
		return "", false, err
	}
	return version, false, nil
}

func concreteRunnerVersion(version string) (string, error) {
	version = strings.TrimSpace(version)
	if strings.EqualFold(version, "latest") {
		return "", fmt.Errorf("runner version %q is invalid", version)
	}
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		version = version[1:]
	}
	if version == "" || strings.EqualFold(version, "latest") || strings.Contains(version, "/") || strings.Contains(version, "..") {
		return "", fmt.Errorf("runner version %q is invalid", version)
	}
	return version, nil
}

func stripRunnerVersionPrefix(version string) string {
	version = strings.TrimSpace(version)
	if len(version) > 0 && (version[0] == 'v' || version[0] == 'V') {
		return version[1:]
	}
	return version
}
