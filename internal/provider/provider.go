package provider

import (
	"context"
	"io"
)

// ExecPipe is a bidirectional binary stdio stream to a guest command.
type ExecPipe interface {
	io.ReadWriteCloser
	Stderr() string
}

// ExecPipeProvider is implemented by providers that can open a binary stdio
// stream to a guest VM (e.g. `tart exec -i`).
type ExecPipeProvider interface {
	OpenExecPipe(ctx context.Context, name, cmd string, args []string) (ExecPipe, error)
}

type execPipeExecutor interface {
	OpenPipe(ctx context.Context, name string, args []string) (ExecPipe, error)
}

type Capabilities struct {
	MaxConcurrent int
	RunnerArch    string
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

func (RealExecutor) OpenPipe(ctx context.Context, name string, args []string) (ExecPipe, error) {
	return openExecPipe(ctx, name, args)
}
