package pool

import (
	"context"
	"fmt"
	"time"

	"github.com/novr/utsusemi/internal/provider"
)

// Bound each tart stop/delete so a hung CLI cannot block reclaim/purge forever.
const managedVMOpTimeout = 30 * time.Second

func (p *Pool) stopAndDeleteManagedVM(ctx context.Context, vm provider.VM) error {
	if vm.Running {
		if err := p.runVMOp(ctx, func(opCtx context.Context) error {
			return p.provider.Stop(opCtx, vm.Name)
		}); err != nil {
			if !provider.IsBenignMissing(err) {
				p.logger.Warn("stop managed vm failed", "vm", vm.Name, "error", err)
				return fmt.Errorf("stop %s: %w", vm.Name, err)
			}
		}
	}
	if err := p.runVMOp(ctx, func(opCtx context.Context) error {
		return p.provider.Delete(opCtx, vm.Name)
	}); err != nil {
		if provider.IsBenignMissing(err) {
			return nil
		}
		p.logger.Warn("delete managed vm failed", "vm", vm.Name, "error", err)
		return fmt.Errorf("delete %s: %w", vm.Name, err)
	}
	return nil
}

func (p *Pool) runVMOp(parent context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(parent, managedVMOpTimeout)
	defer cancel()
	return fn(ctx)
}
