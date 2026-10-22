package spawn

import "sync"

// SlotControl lets the pool ask a running spawn to stop without cancelling the
// agent context. Nil receivers are no-ops; nil channels never fire in select.
type SlotControl struct {
	abandonOnce sync.Once
	forceOnce   sync.Once
	abandonCh   chan struct{}
	forceCh     chan struct{}
}

func NewSlotControl() *SlotControl {
	return &SlotControl{
		abandonCh: make(chan struct{}),
		forceCh:   make(chan struct{}),
	}
}

// AbandonIdle requests teardown when the runner has not claimed a job (and
// during pre-job phases). After Claimed, the spawn ignores this signal.
func (c *SlotControl) AbandonIdle() {
	if c == nil {
		return
	}
	c.abandonOnce.Do(func() { close(c.abandonCh) })
}

// Force stops the spawn at any phase, including after a job claim.
func (c *SlotControl) Force() {
	if c == nil {
		return
	}
	c.forceOnce.Do(func() { close(c.forceCh) })
}

func (c *SlotControl) abandon() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.abandonCh
}

func (c *SlotControl) force() <-chan struct{} {
	if c == nil {
		return nil
	}
	return c.forceCh
}
