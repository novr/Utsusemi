package provider

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
)

type TartProvider struct {
	exec    CommandExecutor
	softnet bool
}

func NewTartProvider(exec CommandExecutor, softnet bool) *TartProvider {
	return &TartProvider{exec: exec, softnet: softnet}
}

func (p *TartProvider) Capabilities() Capabilities {
	// pool_size must not exceed 2 while using Tart on macOS (see https://tart.run/faq/).
	// Guest networking mode (built-in NAT vs softnet) does not raise this cap.
	return Capabilities{MaxConcurrent: 2}
}

func (p *TartProvider) Available() error {
	if _, err := exec.LookPath("tart"); err != nil {
		return fmt.Errorf("tart not found in PATH; install with `brew install tart`")
	}
	if p.softnet {
		if _, err := exec.LookPath("softnet"); err != nil {
			return fmt.Errorf("softnet not found in PATH; install with `brew install cirruslabs/cli/softnet` and grant it root (SUID or passwordless sudo), or remove softnet from the config")
		}
	}
	return nil
}

func (p *TartProvider) SyncImage(ctx context.Context, ref string) error {
	return p.exec.RunStreaming(ctx, "tart", []string{"pull", ref}, nil, nil)
}

func (p *TartProvider) Clone(ctx context.Context, ref, name string) error {
	return p.exec.Run(ctx, "tart", []string{"clone", ref, name}, nil, nil)
}

func (p *TartProvider) Start(ctx context.Context, name string) error {
	args := []string{"run", name, "--no-graphics"}
	if p.softnet {
		args = append(args, "--net-softnet")
	}
	return p.exec.StartDetached(ctx, "tart", args, nil)
}

// tart exec neither attaches stdin by default nor forwards the host
// environment, so -i is added on demand and env is applied by the guest's env(1).
func (p *TartProvider) ExecStdin(ctx context.Context, name, cmd string, args []string, stdin []byte, env map[string]string) error {
	execArgs := []string{"exec"}
	if len(stdin) > 0 {
		execArgs = append(execArgs, "-i")
	}
	execArgs = append(execArgs, name)
	if len(env) > 0 {
		execArgs = append(execArgs, "env")
		for _, key := range sortedKeys(env) {
			execArgs = append(execArgs, fmt.Sprintf("%s=%s", key, env[key]))
		}
	}
	execArgs = append(execArgs, cmd)
	execArgs = append(execArgs, args...)
	return p.exec.Run(ctx, "tart", execArgs, stdin, nil)
}

func sortedKeys(env map[string]string) []string {
	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func (p *TartProvider) Stop(ctx context.Context, name string) error {
	return p.exec.Run(ctx, "tart", []string{"stop", name}, nil, nil)
}

func (p *TartProvider) Delete(ctx context.Context, name string) error {
	return p.exec.Run(ctx, "tart", []string{"delete", name}, nil, nil)
}

func (p *TartProvider) ListManaged(ctx context.Context, prefix string) ([]VM, error) {
	out, err := p.exec.Output(ctx, "tart", []string{"list", "--source", "local", "--format", "json"})
	if err != nil {
		return nil, err
	}
	return parseTartLocalList(out, prefix)
}

func (p *TartProvider) HealthCheck(ctx context.Context, name string) error {
	running, err := p.IsRunning(ctx, name)
	if err != nil {
		return err
	}
	if !running {
		return fmt.Errorf("vm %s is not running", name)
	}
	return p.ExecStdin(ctx, name, "bash", []string{"-c", "true"}, nil, nil)
}

func (p *TartProvider) IsRunning(ctx context.Context, name string) (bool, error) {
	vms, err := p.ListManaged(ctx, "")
	if err != nil {
		return false, err
	}
	for _, vm := range vms {
		if vm.Name == name {
			return vm.Running, nil
		}
	}
	return false, nil
}

func (p *TartProvider) FreeDiskGB(ctx context.Context) (float64, error) {
	return freeDiskGB(tartHome())
}

func tartHome() string {
	if dir := os.Getenv("TART_HOME"); dir != "" {
		return dir
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".tart"
	}
	return filepath.Join(home, ".tart")
}
