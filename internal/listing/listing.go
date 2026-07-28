package listing

import (
	"context"
	"fmt"
	"strings"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

const (
	ScopeAll     = "all"
	ScopeVMs     = "vms"
	ScopeRunners = "runners"
)

type VM struct {
	Name    string `json:"name"`
	Running bool   `json:"running"`
}

type Runner struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Report struct {
	VMs     []VM     `json:"vms,omitempty"`
	Runners []Runner `json:"runners,omitempty"`
}

type Input struct {
	Cfg       *config.Config
	Target    target.Target
	Provider  provider.VMProvider
	Registrar registrar.RunnerRegistrar
	Scope     string
}

func Collect(ctx context.Context, in Input) (Report, error) {
	switch in.Scope {
	case ScopeAll, ScopeVMs, ScopeRunners:
	default:
		return Report{}, fmt.Errorf("unknown list scope %q", in.Scope)
	}

	report := Report{}
	if in.Scope == ScopeAll || in.Scope == ScopeVMs {
		vms, err := in.Provider.ListManaged(ctx, in.Cfg.VMNamePrefix)
		if err != nil {
			return Report{}, err
		}
		report.VMs = make([]VM, len(vms))
		for i, vm := range vms {
			report.VMs[i] = VM{Name: vm.Name, Running: vm.Running}
		}
	}
	if in.Scope == ScopeAll || in.Scope == ScopeRunners {
		runners, err := in.Registrar.ListRunners(ctx, in.Target, in.Cfg.VMNamePrefix)
		if err != nil {
			return Report{}, err
		}
		report.Runners = make([]Runner, len(runners))
		for i, runner := range runners {
			report.Runners[i] = Runner{ID: runner.ID, Name: runner.Name}
		}
	}
	if in.Scope == ScopeAll {
		if report.VMs == nil {
			report.VMs = []VM{}
		}
		if report.Runners == nil {
			report.Runners = []Runner{}
		}
	}
	return report, nil
}

func FormatText(r Report) string {
	var b strings.Builder
	if r.VMs != nil {
		b.WriteString("vms:\n")
		if len(r.VMs) == 0 {
			b.WriteString("  (none)\n")
		}
		for _, vm := range r.VMs {
			state := "stopped"
			if vm.Running {
				state = "running"
			}
			fmt.Fprintf(&b, "  %s (%s)\n", vm.Name, state)
		}
	}
	if r.Runners != nil {
		if r.VMs != nil {
			b.WriteByte('\n')
		}
		b.WriteString("runners:\n")
		if len(r.Runners) == 0 {
			b.WriteString("  (none)\n")
		}
		for _, runner := range r.Runners {
			fmt.Fprintf(&b, "  %s (id %d)\n", runner.Name, runner.ID)
		}
	}
	return b.String()
}
