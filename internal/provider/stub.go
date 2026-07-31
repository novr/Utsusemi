package provider

import "context"

// Stub is a no-op VMProvider for tests. Only Capabilities and FreeDiskGB are meaningful.
type Stub struct {
	Caps Capabilities
}

func NewStub(maxConcurrent int) *Stub {
	return &Stub{Caps: Capabilities{MaxConcurrent: maxConcurrent}}
}

func (s *Stub) Available() error { return nil }

func (s *Stub) SyncImage(context.Context, string) error { return nil }

func (s *Stub) Clone(context.Context, string, string) error { return nil }

func (s *Stub) Start(context.Context, string) error { return nil }

func (s *Stub) ExecStdin(context.Context, string, string, []string, []byte, map[string]string) error {
	return nil
}

func (s *Stub) Stop(context.Context, string) error { return nil }

func (s *Stub) Delete(context.Context, string) error { return nil }

func (s *Stub) ListManaged(context.Context, string) ([]VM, error) { return nil, nil }

func (s *Stub) HealthCheck(context.Context, string) error { return nil }

func (s *Stub) IsRunning(context.Context, string) (bool, error) { return false, nil }

func (s *Stub) Capabilities() Capabilities { return s.Caps }

func (s *Stub) FreeDiskGB(context.Context) (float64, error) { return 100, nil }
