package listing

import (
	"context"
	"fmt"
	"strings"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

const (
	ScopeAll     = "all"
	ScopeVMs     = "vms"
	ScopeRunners = "runners"
)

// TargetChoices are valid non-empty arguments for `utsusemi list`.
func TargetChoices() []string {
	return []string{ScopeVMs, ScopeRunners}
}

type VM struct {
	Name          string `json:"name"`
	Running       bool   `json:"running"`
	BaseImage     string `json:"base_image,omitempty"`
	RunnerVersion string `json:"runner_version,omitempty"`
}

type Runner struct {
	ID            int64  `json:"id"`
	Name          string `json:"name"`
	BaseImage     string `json:"base_image,omitempty"`
	RunnerVersion string `json:"runner_version,omitempty"`
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
	Leases    *lease.Registry
	Scope     string
}

func Collect(ctx context.Context, in Input) (Report, error) {
	switch in.Scope {
	case ScopeAll, ScopeVMs, ScopeRunners:
	default:
		return Report{}, fmt.Errorf("unknown list scope %q", in.Scope)
	}

	leaseByVM, leaseByRunner := loadLeaseIndexes(in.Leases)

	report := Report{}
	if in.Scope == ScopeAll || in.Scope == ScopeVMs {
		vms, err := in.Provider.ListManaged(ctx, in.Cfg.VMNamePrefix)
		if err != nil {
			return Report{}, err
		}
		report.VMs = make([]VM, len(vms))
		for i, vm := range vms {
			row := VM{Name: vm.Name, Running: vm.Running}
			if l, ok := leaseByVM[vm.Name]; ok {
				row.BaseImage = l.BaseImage
				row.RunnerVersion = l.RunnerVersion
			}
			report.VMs[i] = row
		}
	}
	if in.Scope == ScopeAll || in.Scope == ScopeRunners {
		runners, err := in.Registrar.ListRunners(ctx, in.Target, in.Cfg.VMNamePrefix)
		if err != nil {
			return Report{}, err
		}
		report.Runners = make([]Runner, len(runners))
		for i, runner := range runners {
			row := Runner{ID: runner.ID, Name: runner.Name}
			if l, ok := leaseByRunner[runner.ID]; ok {
				row.BaseImage = l.BaseImage
				row.RunnerVersion = l.RunnerVersion
			} else if l, ok := leaseByVM[runner.Name]; ok {
				row.BaseImage = l.BaseImage
				row.RunnerVersion = l.RunnerVersion
			}
			report.Runners[i] = row
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

func loadLeaseIndexes(reg *lease.Registry) (map[string]lease.Lease, map[int64]lease.Lease) {
	byVM := map[string]lease.Lease{}
	byRunner := map[int64]lease.Lease{}
	if reg == nil {
		return byVM, byRunner
	}
	leases, err := reg.ListLeases()
	if err != nil {
		return byVM, byRunner
	}
	for _, l := range leases {
		if l.VMName != "" {
			byVM[l.VMName] = l
		}
		if l.RunnerID != 0 {
			byRunner[l.RunnerID] = l
		}
	}
	return byVM, byRunner
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
			fmt.Fprintf(&b, "  %s (%s)", vm.Name, state)
			writeMeta(&b, vm.BaseImage, vm.RunnerVersion)
			b.WriteByte('\n')
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
			fmt.Fprintf(&b, "  %s (id %d)", runner.Name, runner.ID)
			writeMeta(&b, runner.BaseImage, runner.RunnerVersion)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func writeMeta(b *strings.Builder, baseImage, runnerVersion string) {
	if baseImage != "" {
		fmt.Fprintf(b, " base_image=%s", baseImage)
	}
	if runnerVersion != "" {
		fmt.Fprintf(b, " runner_version=%s", runnerVersion)
	}
}
