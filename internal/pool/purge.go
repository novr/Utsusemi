package pool

import (
	"context"

	"github.com/novr/utsusemi/internal/provider"
)

func (p *Pool) purgeAllManaged(ctx context.Context, dryRun bool) ([]provider.VM, []int64, error) {
	vms, err := p.provider.ListManaged(ctx, p.effectivePrefix)
	if err != nil {
		return nil, nil, err
	}
	runners, err := p.registrar.ListRunners(ctx, p.tgt, p.effectivePrefix)
	if err != nil {
		return nil, nil, err
	}

	if dryRun {
		deleted := make([]int64, 0, len(runners))
		for _, runner := range runners {
			deleted = append(deleted, runner.ID)
		}
		return vms, deleted, nil
	}

	for _, vm := range vms {
		_ = p.stopAndDeleteManagedVM(ctx, vm)
	}
	deleted := make([]int64, 0, len(runners))
	for _, runner := range runners {
		if err := p.registrar.DeleteRunner(ctx, p.tgt, runner.ID); err != nil {
			p.logger.Warn("delete managed runner failed", "runner", runner.Name, "error", err)
			continue
		}
		deleted = append(deleted, runner.ID)
	}
	if err := p.leases.ClearLeases(); err != nil {
		p.logger.Warn("clear leases failed", "error", err)
	}
	return vms, deleted, nil
}
