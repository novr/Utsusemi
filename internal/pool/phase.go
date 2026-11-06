package pool

// phase is the pool lifecycle. lowDisk stays a separate bool: it pauses spawn
// during phaseRun without stopping reclaim or disk_blocked alerts.
type phase uint8

const (
	phaseRun phase = iota
	phasePoolDrain
	phaseHeld
	phaseShutdown
)

// setPhase updates lifecycle. phaseShutdown is sticky: no transition leaves it.
// Caller must hold p.mu. poolDrainState is cleared when leaving phasePoolDrain
// except to phaseShutdown (finishPoolDrain still needs the metadata).
func (p *Pool) setPhase(next phase) bool {
	if p.phase == phaseShutdown && next != phaseShutdown {
		return false
	}
	if p.phase == next {
		return true
	}
	prev := p.phase
	p.phase = next
	if prev == phasePoolDrain && next != phasePoolDrain && next != phaseShutdown {
		p.poolDrainState = nil
	}
	return true
}
