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
	stop      bool
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
	// Held complete (Stop) for this session is kept so status can show pool_stop.
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
	draining := p.phase == phasePoolDrain
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
		Stop:              st.stop,
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
	ph := p.phase
	active := p.active
	p.mu.Unlock()

	if ph == phaseHeld {
		if req.Stop {
			_ = drainctl.WriteStatus(p.cfg.StateDir, drainctl.Status{
				RequestID: req.RequestID,
				AgentID:   req.AgentID,
				State:     drainctl.StateComplete,
				Active:    active,
				Stop:      true,
				Deadline:  req.Deadline,
			})
			_ = drainctl.RemoveRequest(p.cfg.StateDir)
			p.logger.Info("pool already held; acknowledging stop",
				"request_id", req.RequestID,
			)
			return
		}
		p.rejectHeldDrainRequest(req)
		return
	}

	if ph != phaseRun {
		p.rejectNonRunDrainRequest(req)
		return
	}

	p.mu.Lock()
	if p.phase != phaseRun || !p.setPhase(phasePoolDrain) {
		p.mu.Unlock()
		p.rejectNonRunDrainRequest(req)
		return
	}
	p.poolDrainState = &poolDrainState{
		requestID: req.RequestID,
		agentID:   req.AgentID,
		deadline:  req.Deadline,
		force:     req.Force,
		stop:      req.Stop,
	}
	ctrls := make([]*spawn.SlotControl, 0, len(p.slotControls))
	for _, c := range p.slotControls {
		ctrls = append(ctrls, c)
	}
	active = p.active
	p.mu.Unlock()

	// Drop the request so a concurrent CLI write is visible to rejectBusy.
	_ = drainctl.RemoveRequest(p.cfg.StateDir)

	deadlineLog := "none"
	if !req.Deadline.IsZero() {
		deadlineLog = req.Deadline.UTC().Format(time.RFC3339)
	}
	p.logger.Info("pool drain started",
		"request_id", req.RequestID,
		"force", req.Force,
		"stop", req.Stop,
		"deadline", deadlineLog,
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
	done := p.phase == phasePoolDrain && p.active == 0
	p.mu.Unlock()
	if done {
		p.finishPoolDrain("")
	}
}

func (p *Pool) rejectHeldDrainRequest(req drainctl.Request) {
	existing, ok, err := drainctl.LoadStatus(p.cfg.StateDir)
	if err != nil || !ok || !existing.Stop || existing.State != drainctl.StateComplete {
		p.mu.Lock()
		active := p.active
		p.mu.Unlock()
		existing = drainctl.Status{
			RequestID: req.RequestID,
			AgentID:   req.AgentID,
			State:     drainctl.StateComplete,
			Active:    active,
			Stop:      true,
		}
	}
	existing.RejectedRequestID = req.RequestID
	existing.RejectedError = drainctl.ErrHeld.Error()
	_ = drainctl.WriteStatus(p.cfg.StateDir, existing)
	_ = drainctl.RemoveRequest(p.cfg.StateDir)
}

func (p *Pool) rejectNonRunDrainRequest(req drainctl.Request) {
	p.mu.Lock()
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
		status.Stop = st.stop
		status.Deadline = st.deadline
	} else {
		status.RequestID = req.RequestID
		status.AgentID = req.AgentID
		status.Error = drainctl.ErrBusy.Error()
	}
	_ = drainctl.WriteStatus(p.cfg.StateDir, status)
	_ = drainctl.RemoveRequest(p.cfg.StateDir)
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
		Stop:      st.stop,
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

// maybeFinishPoolDrain clears phasePoolDrain when the last in-flight spawn exits.
func (p *Pool) maybeFinishPoolDrain() {
	p.mu.Lock()
	if p.poolDrainState == nil || p.active > 0 {
		p.mu.Unlock()
		return
	}
	if p.phase != phasePoolDrain && p.phase != phaseShutdown {
		p.mu.Unlock()
		return
	}
	p.mu.Unlock()
	p.finishPoolDrain("")
}

func (p *Pool) finishPoolDrain(errMsg string) {
	p.mu.Lock()
	if p.poolDrainState == nil {
		p.mu.Unlock()
		return
	}
	// Allow finishing after shutdown raced past phasePoolDrain so status is still written.
	if p.phase != phasePoolDrain && p.phase != phaseShutdown {
		p.mu.Unlock()
		return
	}
	st := *p.poolDrainState
	if errMsg != "" {
		st.errMsg = errMsg
	}
	active := p.active
	forced := st.forced
	refill := false
	switch p.phase {
	case phasePoolDrain:
		if st.stop {
			_ = p.setPhase(phaseHeld)
		} else {
			refill = p.setPhase(phaseRun)
		}
	case phaseShutdown:
		p.poolDrainState = nil
	}
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
		Stop:      st.stop,
		Deadline:  st.deadline,
		Error:     st.errMsg,
	})
	_ = drainctl.RemoveRequest(p.cfg.StateDir)
	p.logger.Info("pool drain finished",
		"request_id", st.requestID,
		"state", state,
		"forced", forced,
		"stop", st.stop,
		"error", st.errMsg,
	)
	if !refill {
		return
	}
	// Force-timeout finish does not go through the spawn defer; refill here.
	if ctx := p.runCtx; ctx != nil {
		go p.tick(ctx)
	}
}

// syncHeldStatusActive rewrites a finished stop status after later spawn exits.
// Force-grace can latch phaseHeld while runners are still winding down; the
// status Active captured at that moment would otherwise stay stale.
func (p *Pool) syncHeldStatusActive() {
	p.mu.Lock()
	if p.phase != phaseHeld {
		p.mu.Unlock()
		return
	}
	active := p.active
	p.mu.Unlock()

	st, ok, err := drainctl.LoadStatus(p.cfg.StateDir)
	if err != nil || !ok || !st.Stop || st.State == drainctl.StateRunning || st.Active == active {
		return
	}
	// A newer stop/drain status may have replaced this one.
	cur, ok, err := drainctl.LoadStatus(p.cfg.StateDir)
	if err != nil || !ok || cur.RequestID != st.RequestID || !cur.Stop || cur.State == drainctl.StateRunning {
		return
	}
	cur.Active = active
	cur.UpdatedAt = time.Time{}
	_ = drainctl.WriteStatus(p.cfg.StateDir, cur)
}
