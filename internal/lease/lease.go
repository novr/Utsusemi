package lease

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"github.com/novr/utsusemi/internal/config"
)

type Lease struct {
	VMName    string    `json:"vm_name"`
	RunnerID  int64     `json:"runner_id"`
	AgentID   string    `json:"agent_id"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

type AgentSession struct {
	ID        string    `json:"id"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
}

type Registry struct {
	stateDir string
}

func NewRegistry(stateDir string) *Registry {
	return &Registry{stateDir: stateDir}
}

func (r *Registry) StateDir() string {
	return r.stateDir
}

func (r *Registry) leaseDir() string {
	return filepath.Join(r.stateDir, "leases")
}

func (r *Registry) agentPath() string {
	return filepath.Join(r.stateDir, "agent.json")
}

func (r *Registry) EnsureDirs() error {
	return os.MkdirAll(r.leaseDir(), 0o755)
}

func (r *Registry) BeginAgentSession() (*AgentSession, error) {
	if err := r.EnsureDirs(); err != nil {
		return nil, err
	}
	session := &AgentSession{
		ID:        newAgentID(),
		PID:       os.Getpid(),
		StartedAt: time.Now().UTC(),
	}
	data, err := json.Marshal(session)
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(r.agentPath(), data, 0o644); err != nil {
		return nil, err
	}
	return session, nil
}

func (r *Registry) WriteLease(session *AgentSession, lease Lease) error {
	if session == nil {
		return errors.New("session is required")
	}
	if err := r.EnsureDirs(); err != nil {
		return err
	}
	lease.AgentID = session.ID
	lease.PID = session.PID
	if lease.StartedAt.IsZero() {
		lease.StartedAt = time.Now().UTC()
	}
	data, err := json.Marshal(lease)
	if err != nil {
		return err
	}
	path := filepath.Join(r.leaseDir(), lease.VMName+".json")
	return os.WriteFile(path, data, 0o644)
}

func (r *Registry) RemoveLease(vmName string) error {
	path := filepath.Join(r.leaseDir(), vmName+".json")
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (r *Registry) ListLeases() ([]Lease, error) {
	entries, err := os.ReadDir(r.leaseDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	leases := make([]Lease, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(r.leaseDir(), entry.Name()))
		if err != nil {
			continue
		}
		var lease Lease
		if err := json.Unmarshal(data, &lease); err != nil {
			continue
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

func (r *Registry) ClearLeases() error {
	entries, err := os.ReadDir(r.leaseDir())
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(r.leaseDir(), entry.Name())); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func (r *Registry) LeaseMap() (map[string]Lease, error) {
	leases, err := r.ListLeases()
	if err != nil {
		return nil, err
	}
	out := make(map[string]Lease, len(leases))
	for _, lease := range leases {
		out[lease.VMName] = lease
	}
	return out, nil
}

func IsStale(lease *Lease, session *AgentSession) bool {
	if lease == nil || session == nil {
		return true
	}
	if lease.AgentID != session.ID {
		return true
	}
	return !pidAlive(lease.PID)
}

func ShouldReclaimRunning(lease *Lease, session *AgentSession, policy string, grace time.Duration, now time.Time) bool {
	switch policy {
	case config.ReclaimHard:
		return IsStale(lease, session)
	case config.ReclaimGrace:
		if lease == nil {
			return true
		}
		if IsStale(lease, session) && now.Sub(lease.StartedAt) >= grace {
			return true
		}
		return false
	default:
		return false
	}
}

func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func newAgentID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}
