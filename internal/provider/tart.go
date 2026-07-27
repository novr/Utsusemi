package provider

import (
	"context"
	"fmt"
)

type TartProvider struct {
	exec CommandExecutor
}

func NewTartProvider(exec CommandExecutor) *TartProvider {
	return &TartProvider{exec: exec}
}

func (p *TartProvider) Capabilities() Capabilities {
	return Capabilities{MaxConcurrent: 2}
}

func (p *TartProvider) SyncImage(ctx context.Context, ref string) error {
	return p.exec.Run(ctx, "tart", []string{"pull", ref}, nil, nil)
}

func (p *TartProvider) Clone(ctx context.Context, ref, name string) error {
	return p.exec.Run(ctx, "tart", []string{"clone", ref, name}, nil, nil)
}

func (p *TartProvider) Start(ctx context.Context, name string) error {
	return p.exec.StartDetached(ctx, "tart", []string{"run", name, "--no-graphics"}, nil)
}

func (p *TartProvider) ExecStdin(ctx context.Context, name, cmd string, args []string, stdin []byte, env map[string]string) error {
	execArgs := append([]string{"exec", name, "--", cmd}, args...)
	return p.exec.Run(ctx, "tart", execArgs, stdin, env)
}

func (p *TartProvider) Stop(ctx context.Context, name string) error {
	return p.exec.Run(ctx, "tart", []string{"stop", name}, nil, nil)
}

func (p *TartProvider) Delete(ctx context.Context, name string) error {
	return p.exec.Run(ctx, "tart", []string{"delete", name, "--force"}, nil, nil)
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
	return freeDiskGB("/")
}
