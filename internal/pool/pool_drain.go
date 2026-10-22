package pool

import (
	"errors"
	"time"

	"github.com/novr/utsusemi/internal/drainctl"
	"github.com/novr/utsusemi/internal/spawn"
)

// Overridable for tests.
var (
	poolDrainControlInterval = time.Second
	poolDrainForceGrace      = drainctl.ForceGrace
)

type poolDrainState struct {
	requestID string
	agentID   string
	deadline  time.Time
	force     bool
	forced    bool
	forcedAt  time.Time
	errMsg    string
}

func (p *Pool) scrubDrainControl() {
	if p.session == nil {
		return
	}
	req, err := drainctl.LoadRequest(p.cfg.StateDir)
	if err != nil {
		if !errors.Is(err, drainctl.ErrNoRequest) {
			p.logger.Warn("load drain request failed", "error", err)
		}
	} else if drainctl.ShouldDiscardRequest(req, p.session.ID, p.session.StartedAt) {
		p.logger.Info("discarding stale drain request",
			"request_id", req.RequestID,
			"request_agent", req.AgentID,
		)
		_ = drainctl.RemoveRequest(p.cfg.StateDir)
	}

	st, ok, err := drainctl.LoadStatus(p.cfg.StateDir)
	if err != nil || !ok {
		return
	}
	// Drop status from a previous agent, or a running status left by a crash.
	if st.AgentID != p.session.ID || st.State == drainctl.StateRunning {
		p.logger.Info("clearing stale drain status",
			"request_id", st.RequestID,
			"state", st.State,
			"status_agent", st.AgentID,
		)
		_ = drainctl.RemoveStatus(p.cfg.StateDir)
	}
}

func (p *Pool) controlTick() {
	p.mu.Lock()
	draining := p.poolDrain
	p.mu.Unlock()

	if !draining {
		p.acceptDrainRequest()
		return
	}

	p.rejectBusyDrainRequest()

	p.mu.Lock()
	st := p.poolDrainState
	forceNow := false
	forceTimedOut := false
	if st != nil {
		if st.force && !st.forced {
			forceNow = true
		}
		if !st.force && !st.forced && !st.deadline.IsZero() && !time.Now().Before(st.deadline) {
			forceNow = true
		}
		if st.forced && !st.forcedAt.IsZero() && time.Since(st.forcedAt) >= poolDrainForceGrace {
			forceTimedOut = true
		}
	}
	p.mu.Unlock()

	// Re-Force every tick while forced so slots that registered after the first
	// Force (race with accept) still get cancelled.
	if forceNow || (st != nil && st.forced) {
		p.forcePoolDrainSlots()
	}
	if forceTimedOut {
		p.finishPoolDrain("force reclaim timed out with runners still active")
		return
	}
	p.writePoolDrainStatus()
}

// rejectBusyDrainRequest fails any new request that is not the in-flight drain.
func (p *Pool) rejectBusyDrainRequest() {
	req, err := drainctl.LoadRequest(p.cfg.StateDir)
	if err != nil {
		return
	}
	p.mu.Lock()
	st := p.poolDrainState
	active := p.active
	p.mu.Unlock()
	if st == nil || req.RequestID == st.requestID {
		return
	}
	// Keep the active drain's status visible; attach rejection for the other CLI.
	_ = drainctl.WriteStatus(p.cfg.StateDir, drainctl.Status{
		RequestID:         st.requestID,
		AgentID:           st.agentID,
		State:             drainctl.StateRunning,
		Active:            active,
		Forced:            st.forced,
		Deadline:          st.deadline,
		RejectedRequestID: req.RequestID,
		RejectedError:     drainctl.ErrBusy.Error(),
	})
	_ = drainctl.RemoveRequest(p.cfg.StateDir)
}

func (p *Pool) acceptDrainRequest() {
	if p.session == nil {
		return
	}
	req, err := drainctl.LoadRequest(p.cfg.StateDir)
	if err != nil {
		if !errors.Is(err, drainctl.ErrNoRequest) {
			p.logger.Warn("load drain request failed", "error", err)
		}
		return
	}
	if drainctl.ShouldDiscardRequest(req, p.session.ID, p.session.StartedAt) {
		_ = drainctl.RemoveRequest(p.cfg.StateDir)
		return
	}

	p.mu.Lock()
	if p.poolDrain || p.shutdown || p.drain {
		st := p.poolDrainState
		active := p.active
		p.mu.Unlock()
		status := drainctl.Status{
			State:             drainctl.StateError,
			RejectedRequestID: req.RequestID,
			RejectedError:     drainctl.ErrBusy.Error(),
			Deadline:          req.Deadline,
		}
		if st != nil {
			status.RequestID = st.requestID
			status.AgentID = st.agentID
			status.State = drainctl.StateRunning
			status.Active = active
			status.Forced = st.forced
			status.Deadline = st.deadline
		} else {
			status.RequestID = req.RequestID
			status.AgentID = req.AgentID
			status.Error = drainctl.ErrBusy.Error()
		}
		_ = drainctl.WriteStatus(p.cfg.StateDir, status)
		_ = drainctl.RemoveRequest(p.cfg.StateDir)
		return
	}
	p.poolDrain = true
	p.poolDrainState = &poolDrainState{
		requestID: req.RequestID,
		agentID:   req.AgentID,
		deadline:  req.Deadline,
		force:     req.Force,
	}
	ctrls := make([]*spawn.SlotControl, 0, len(p.slotControls))
	for _, c := range p.slotControls {
		ctrls = append(ctrls, c)
	}
	active := p.active
	p.mu.Unlock()

	// Drop the request so a concurrent CLI write is visible to rejectBusy.
	_ = drainctl.RemoveRequest(p.cfg.StateDir)

	p.logger.Info("pool drain started",
		"request_id", req.RequestID,
		"force", req.Force,
		"deadline", req.Deadline.UTC().Format(time.RFC3339),
		"active", active,
	)
	for _, c := range ctrls {
		c.AbandonIdle()
	}
	if req.Force {
		p.forcePoolDrainSlots()
	}
	p.writePoolDrainStatus()

	p.mu.Lock()
	done := p.poolDrain && p.active == 0
	p.mu.Unlock()
	if done {
		p.finishPoolDrain("")
	}
}

func (p *Pool) forcePoolDrainSlots() {
	p.mu.Lock()
	first := false
	if p.poolDrainState != nil {
		if !p.poolDrainState.forced {
			first = true
		}
		p.poolDrainState.forced = true
		if p.poolDrainState.forcedAt.IsZero() {
			p.poolDrainState.forcedAt = time.Now()
		}
	}
	ctrls := make([]*spawn.SlotControl, 0, len(p.slotControls))
	for _, c := range p.slotControls {
		ctrls = append(ctrls, c)
	}
	p.mu.Unlock()
	if first {
		p.logger.Info("pool drain forcing remaining runners", "count", len(ctrls))
	}
	for _, c := range ctrls {
		c.Force()
	}
}

func (p *Pool) writePoolDrainStatus() {
	p.mu.Lock()
	st := p.poolDrainState
	active := p.active
	p.mu.Unlock()
	if st == nil {
		return
	}
	status := drainctl.Status{
		RequestID: st.requestID,
		AgentID:   st.agentID,
		State:     drainctl.StateRunning,
		Active:    active,
		Forced:    st.forced,
		Deadline:  st.deadline,
		Error:     st.errMsg,
	}
	// Preserve a concurrent busy rejection until the rejected CLI observes it.
	if existing, ok, err := drainctl.LoadStatus(p.cfg.StateDir); err == nil && ok &&
		existing.RequestID == st.requestID && existing.RejectedRequestID != "" {
		status.RejectedRequestID = existing.RejectedRequestID
		status.RejectedError = existing.RejectedError
	}
	_ = drainctl.WriteStatus(p.cfg.StateDir, status)
}

// maybeFinishPoolDrain clears poolDrain when the last in-flight spawn exits.
func (p *Pool) maybeFinishPoolDrain() {
	p.mu.Lock()
	if !p.poolDrain || p.active > 0 {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.finishPoolDrain("")
}

func (p *Pool) finishPoolDrain(errMsg string) {
	p.mu.Lock()
	if !p.poolDrain || p.poolDrainState == nil {
		p.mu.Unlock()
		return
	}
	st := *p.poolDrainState
	if errMsg != "" {
		st.errMsg = errMsg
	}
	active := p.active
	forced := st.forced
	p.poolDrain = false
	p.poolDrainState = nil
	p.mu.Unlock()

	state := drainctl.StateComplete
	if st.errMsg != "" {
		state = drainctl.StateError
	}
	_ = drainctl.WriteStatus(p.cfg.StateDir, drainctl.Status{
		RequestID: st.requestID,
		AgentID:   st.agentID,
		State:     state,
		Active:    active,
		Forced:    forced,
		Deadline:  st.deadline,
		Error:     st.errMsg,
	})
	_ = drainctl.RemoveRequest(p.cfg.StateDir)
	p.logger.Info("pool drain finished",
		"request_id", st.requestID,
		"state", state,
		"forced", forced,
		"error", st.errMsg,
	)
}
