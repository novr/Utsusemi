package provider

import (
	"context"
)

type Capabilities struct {
	MaxConcurrent int
}

type VM struct {
	Name    string
	Running bool
}

type VMProvider interface {
	Available() error
	SyncImage(ctx context.Context, ref string) error
	Clone(ctx context.Context, ref, name string) error
	Start(ctx context.Context, name string) error
	ExecStdin(ctx context.Context, name, cmd string, args []string, stdin []byte, env map[string]string) error
	Stop(ctx context.Context, name string) error
	Delete(ctx context.Context, name string) error
	ListManaged(ctx context.Context, prefix string) ([]VM, error)
	HealthCheck(ctx context.Context, name string) error
	IsRunning(ctx context.Context, name string) (bool, error)
	Capabilities() Capabilities
	FreeDiskGB(ctx context.Context) (float64, error)
}

type CommandExecutor interface {
	Run(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error
	RunStreaming(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error
	StartDetached(ctx context.Context, name string, args []string, env map[string]string) error
	Output(ctx context.Context, name string, args []string) ([]byte, error)
}

type RealExecutor struct{}

func (RealExecutor) Run(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error {
	return runCommand(ctx, name, args, stdin, env)
}

func (RealExecutor) RunStreaming(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error {
	return runCommandStreaming(ctx, name, args, stdin, env)
}

func (RealExecutor) StartDetached(ctx context.Context, name string, args []string, env map[string]string) error {
	return startDetached(ctx, name, args, env)
}

func (RealExecutor) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	return outputCommand(ctx, name, args)
}
