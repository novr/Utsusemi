package drainctl

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const (
	RequestFile = "drain.request"
	StatusFile  = "drain.status"

	StateRunning  = "running"
	StateComplete = "complete"
	StateError    = "error"

	// ForceGrace is how long the agent waits after Force before finishing
	// poolDrain even if spawns have not exited. CLI wait budgets should
	// cover deadline + ForceGrace.
	ForceGrace = 2 * time.Minute
)

var (
	ErrNoRequest    = errors.New("drain: no request")
	ErrBusy         = errors.New("drain: already in progress")
	ErrAgentStopped = errors.New("drain: agent is not running")
)

type Request struct {
	RequestID   string    `json:"request_id"`
	AgentID     string    `json:"agent_id"`
	RequestedAt time.Time `json:"requested_at"`
	Deadline    time.Time `json:"deadline"`
	Force       bool      `json:"force"`
}

type Status struct {
	RequestID string    `json:"request_id"`
	AgentID   string    `json:"agent_id"`
	State     string    `json:"state"`
	Active    int       `json:"active"`
	Forced    bool      `json:"forced,omitempty"`
	Error     string    `json:"error,omitempty"`
	Deadline  time.Time `json:"deadline,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
	// RejectedRequestID is set when a concurrent drain request was refused while
	// this status still describes the in-flight drain (State=running).
	RejectedRequestID string `json:"rejected_request_id,omitempty"`
	RejectedError     string `json:"rejected_error,omitempty"`
}

func NewRequestID() (string, error) {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func requestPath(stateDir string) string {
	return filepath.Join(stateDir, RequestFile)
}

func statusPath(stateDir string) string {
	return filepath.Join(stateDir, StatusFile)
}

func WriteRequest(stateDir string, req Request) error {
	if stateDir == "" {
		return fmt.Errorf("drain: empty state dir")
	}
	if req.RequestID == "" || req.AgentID == "" {
		return fmt.Errorf("drain: request_id and agent_id required")
	}
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now().UTC()
	}
	if err := os.MkdirAll(stateDir, 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	// Exclusive create so two CLIs cannot silently overwrite each other.
	f, err := os.OpenFile(requestPath(stateDir), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			return ErrBusy
		}
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(requestPath(stateDir))
		return werr
	}
	if cerr != nil {
		_ = os.Remove(requestPath(stateDir))
		return cerr
	}
	return nil
}

func LoadRequest(stateDir string) (Request, error) {
	var req Request
	ok, err := readJSON(requestPath(stateDir), &req)
	if err != nil {
		return Request{}, err
	}
	if !ok {
		return Request{}, ErrNoRequest
	}
	return req, nil
}

func RemoveRequest(stateDir string) error {
	err := os.Remove(requestPath(stateDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func WriteStatus(stateDir string, st Status) error {
	if stateDir == "" {
		return fmt.Errorf("drain: empty state dir")
	}
	if st.UpdatedAt.IsZero() {
		st.UpdatedAt = time.Now().UTC()
	}
	return writeJSON(statusPath(stateDir), st)
}

func LoadStatus(stateDir string) (Status, bool, error) {
	var st Status
	ok, err := readJSON(statusPath(stateDir), &st)
	if err != nil {
		return Status{}, false, err
	}
	return st, ok, nil
}

func RemoveStatus(stateDir string) error {
	err := os.Remove(statusPath(stateDir))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// MatchesRequest reports whether status belongs to the given request id.
func MatchesRequest(st Status, requestID string) bool {
	return st.RequestID != "" && st.RequestID == requestID
}

// IsTerminal is true for complete or error.
func IsTerminal(st Status) bool {
	return st.State == StateComplete || st.State == StateError
}

// ShouldDiscardRequest is true when the request is not for this agent session.
func ShouldDiscardRequest(req Request, agentID string, sessionStartedAt time.Time) bool {
	if req.AgentID == "" || req.AgentID != agentID {
		return true
	}
	if !sessionStartedAt.IsZero() && req.RequestedAt.Before(sessionStartedAt) {
		return true
	}
	return false
}

func writeJSON(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func readJSON(path string, v any) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if err := json.Unmarshal(data, v); err != nil {
		return false, err
	}
	return true, nil
}
