package pool

import (
	"context"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/lease"
)

func (p *Pool) startupReclaim(ctx context.Context) error {
	switch p.cfg.ReclaimPolicy {
	case config.ReclaimHard:
		return p.reclaim(ctx, true)
	case config.ReclaimGrace:
		return p.reclaim(ctx, false)
	default:
		return p.reclaim(ctx, false)
	}
}

func (p *Pool) reclaim(ctx context.Context, startupHard bool) error {
	freeGB, err := p.provider.FreeDiskGB(ctx)
	if err != nil {
		return err
	}
	p.mu.Lock()
	if freeGB < float64(p.cfg.MinFreeDiskGB) {
		p.lowDisk = true
		p.logger.Warn("low disk space, pausing spawn", "free_gb", freeGB)
	} else {
		p.lowDisk = false
	}
	shutdown := p.shutdown || p.drain
	inFlight := make(map[string]struct{}, len(p.inFlightVMs))
	for name := range p.inFlightVMs {
		inFlight[name] = struct{}{}
	}
	p.mu.Unlock()

	if shutdown {
		return nil
	}

	runners, err := p.registrar.ListRunners(ctx, p.tgt, p.effectivePrefix)
	if err != nil {
		p.logger.Warn("list runners failed", "error", err)
		return nil
	}

	vms, err := p.provider.ListManaged(ctx, p.effectivePrefix)
	if err != nil {
		return err
	}

	leaseMap, err := p.leases.LeaseMap()
	if err != nil {
		p.logger.Warn("list leases failed", "error", err)
		leaseMap = map[string]lease.Lease{}
	}

	runningVMs := make(map[string]struct{})
	for _, vm := range vms {
		if vm.Running {
			runningVMs[vm.Name] = struct{}{}
		}
	}

	now := time.Now()
	policy := p.cfg.ReclaimPolicy
	grace := p.cfg.ReclaimGrace.Duration()

	for _, vm := range vms {
		if _, ok := inFlight[vm.Name]; ok {
			continue
		}

		l, hasLease := leaseMap[vm.Name]
		var leasePtr *lease.Lease
		if hasLease {
			leasePtr = &l
		}

		if vm.Running {
			aggressive := startupHard && policy == config.ReclaimHard
			if !aggressive && !lease.ShouldReclaimRunning(leasePtr, p.session, policy, grace, now) {
				continue
			}
		}

		if err := p.stopAndDeleteManagedVM(ctx, vm); err != nil {
			continue
		}
		_ = p.leases.RemoveLease(vm.Name)
		delete(runningVMs, vm.Name)
	}

	for _, runner := range runners {
		if _, active := inFlight[runner.Name]; active {
			continue
		}
		if _, running := runningVMs[runner.Name]; running {
			continue
		}

		l, hasLease := leaseMap[runner.Name]
		if hasLease && !lease.IsStale(&l, p.session) && p.cfg.ReclaimPolicy == config.ReclaimSoft {
			continue
		}

		if err := p.registrar.DeleteRunner(ctx, p.tgt, runner.ID); err != nil {
			p.logger.Warn("delete orphan runner failed", "runner", runner.Name, "error", err)
		}
	}
	return nil
}
