package spawn

import (
	"bytes"
	"strings"
	"sync"
	"time"
)

// Markers emitted by github/actions/runner when a job is assigned or finishes.
// Pool short-exit detection treats ClaimedJob as proof the runner is healthy.
const (
	claimMarkerRunning   = "Running job:"
	claimMarkerCompleted = "completed with result:"
	idleMarkerListening  = "Listening for Jobs"
	connectErrorMarker   = "Runner connect error"
	reconnectedMarker    = "Runner reconnected."
)

// claimScanner is a line-buffered io.Writer that records whether runner output
// shows a claimed job or idle Listening. Safe for concurrent Write from
// stdout/stderr tees.
type claimScanner struct {
	mu             sync.Mutex
	buf            []byte
	claimed        bool
	listening      bool
	connectErr     bool
	connectSince   time.Time
	onIdleReady    func()
	now            func() time.Time
}

func (c *claimScanner) clock() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *claimScanner) Write(p []byte) (int, error) {
	var notify []func()
	c.mu.Lock()
	c.buf = append(c.buf, p...)
	for {
		i := bytes.IndexByte(c.buf, '\n')
		if i < 0 {
			break
		}
		line := string(bytes.TrimSpace(c.buf[:i]))
		c.buf = c.buf[i+1:]
		if fn := c.observe(line); fn != nil {
			notify = append(notify, fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range notify {
		fn()
	}
	return len(p), nil
}

func (c *claimScanner) Flush() error {
	var notify []func()
	c.mu.Lock()
	line := string(bytes.TrimSpace(c.buf))
	c.buf = nil
	if line != "" {
		if fn := c.observe(line); fn != nil {
			notify = append(notify, fn)
		}
	}
	c.mu.Unlock()
	for _, fn := range notify {
		fn()
	}
	return nil
}

// observe updates state under c.mu. On first Listening or claim marker it
// returns onIdleReady to invoke after unlocking (cold start is done either way).
func (c *claimScanner) observe(line string) func() {
	var fn func()
	take := func() {
		if c.onIdleReady != nil {
			fn = c.onIdleReady
			c.onIdleReady = nil
		}
	}
	if strings.Contains(line, connectErrorMarker) {
		if !c.connectErr {
			c.connectErr = true
			c.connectSince = c.clock()
		}
	}
	if strings.Contains(line, reconnectedMarker) {
		c.connectErr = false
		c.connectSince = time.Time{}
	}
	if !c.listening && strings.Contains(line, idleMarkerListening) {
		c.listening = true
		c.connectErr = false
		c.connectSince = time.Time{}
		take()
	}
	if !c.claimed {
		if strings.Contains(line, claimMarkerRunning) || strings.Contains(line, claimMarkerCompleted) {
			c.claimed = true
			c.connectErr = false
			c.connectSince = time.Time{}
			take()
		}
	}
	return fn
}

func (c *claimScanner) Claimed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.claimed
}

func (c *claimScanner) Listening() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.listening
}

// ConnectErrorSince returns when the first unresolved connect error was seen.
// Zero if Listening/claim/reconnected cleared the error, or none was seen.
func (c *claimScanner) ConnectErrorSince() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.connectErr || c.listening || c.claimed {
		return time.Time{}
	}
	return c.connectSince
}
