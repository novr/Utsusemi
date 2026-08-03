package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type FakeExecutor struct {
	mu        sync.Mutex
	Calls     []Call
	Outputs   map[string][]byte
	VMs       map[string]bool
	FailNext  map[string]error
	FailClone error
}

type Call struct {
	Name  string
	Args  []string
	Stdin []byte
	Env   map[string]string
}

func NewFakeExecutor() *FakeExecutor {
	return &FakeExecutor{
		Outputs:  make(map[string][]byte),
		VMs:      make(map[string]bool),
		FailNext: make(map[string]error),
	}
}

func (f *FakeExecutor) key(name string, args []string) string {
	return name + " " + strings.Join(args, " ")
}

func (f *FakeExecutor) Run(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error {
	return f.runUnlocked(ctx, name, args, stdin, env)
}

func (f *FakeExecutor) RunStreaming(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error {
	return f.runUnlocked(ctx, name, args, stdin, env)
}

func (f *FakeExecutor) runUnlocked(ctx context.Context, name string, args []string, stdin []byte, env map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Name: name, Args: append([]string{}, args...), Stdin: append([]byte{}, stdin...), Env: env})
	key := f.key(name, args)
	if err, ok := f.FailNext[key]; ok {
		delete(f.FailNext, key)
		return err
	}
	if len(args) >= 2 && args[0] == "clone" {
		if f.FailClone != nil {
			err := f.FailClone
			f.FailClone = nil
			return err
		}
		f.VMs[args[2]] = false
	}
	if len(args) >= 2 && args[0] == "stop" {
		f.VMs[args[1]] = false
	}
	if len(args) >= 2 && args[0] == "delete" {
		delete(f.VMs, args[1])
	}
	if len(args) >= 2 && args[0] == "exec" {
		if err, ok := f.FailNext["exec"]; ok {
			delete(f.FailNext, "exec")
			return err
		}
	}
	return nil
}

func (f *FakeExecutor) StartDetached(ctx context.Context, name string, args []string, env map[string]string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Name: name, Args: append([]string{}, args...), Env: env})
	if len(args) >= 2 && args[0] == "run" {
		f.VMs[args[1]] = true
	}
	return nil
}

func (f *FakeExecutor) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Calls = append(f.Calls, Call{Name: name, Args: append([]string{}, args...)})
	if out, ok := f.Outputs[f.key(name, args)]; ok {
		return out, nil
	}
	if len(args) > 0 && args[0] == "list" {
		return f.listJSONLocked(), nil
	}
	return nil, fmt.Errorf("unexpected output call: %s %v", name, args)
}

func (f *FakeExecutor) listJSONLocked() []byte {
	records := make([]tartVMRecord, 0, len(f.VMs))
	for vm, running := range f.VMs {
		state := "stopped"
		if running {
			state = "running"
		}
		records = append(records, tartVMRecord{Name: vm, State: state})
	}
	out, _ := json.Marshal(records)
	return out
}

func (f *FakeExecutor) SetListOutput(output string) {
	f.Outputs[f.key("tart", []string{"list", "--source", "local", "--format", "json"})] = []byte(output)
}

func (f *FakeExecutor) SetVersionOutput(version string) {
	f.Outputs[f.key("tart", []string{"--version"})] = []byte(version)
}
