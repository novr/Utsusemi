package spawn

import (
	"bytes"
	"strings"
	"sync"
)

// Markers emitted by github/actions/runner when a job is assigned or finishes.
// Pool short-exit detection treats ClaimedJob as proof the runner is healthy.
const (
	claimMarkerRunning   = "Running job:"
	claimMarkerCompleted = "completed with result:"
)

// claimScanner is a line-buffered io.Writer that records whether runner output
// shows a claimed job. Safe for concurrent Write from stdout/stderr tees.
type claimScanner struct {
	mu      sync.Mutex
	buf     []byte
	claimed bool
}

func (c *claimScanner) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.buf = append(c.buf, p...)
	for {
		i := bytes.IndexByte(c.buf, '\n')
		if i < 0 {
			break
		}
		line := string(bytes.TrimSpace(c.buf[:i]))
		c.buf = c.buf[i+1:]
		c.observe(line)
	}
	return len(p), nil
}

func (c *claimScanner) Flush() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	line := string(bytes.TrimSpace(c.buf))
	c.buf = nil
	if line != "" {
		c.observe(line)
	}
	return nil
}

func (c *claimScanner) observe(line string) {
	if c.claimed {
		return
	}
	if strings.Contains(line, claimMarkerRunning) || strings.Contains(line, claimMarkerCompleted) {
		c.claimed = true
	}
}

func (c *claimScanner) Claimed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.claimed
}
