package pool

import (
	"context"

	"github.com/novr/utsusemi/internal/provider"
)

func (p *Pool) stopAndDeleteManagedVM(ctx context.Context, vm provider.VM) error {
	if vm.Running {
		if err := p.provider.Stop(ctx, vm.Name); err != nil {
			p.logger.Warn("stop managed vm failed", "vm", vm.Name, "error", err)
		}
	}
	if err := p.provider.Delete(ctx, vm.Name); err != nil {
		p.logger.Warn("delete managed vm failed", "vm", vm.Name, "error", err)
		return err
	}
	return nil
}
