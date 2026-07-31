package pool

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/lease"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/spawn"
	"github.com/novr/utsusemi/internal/target"
)

type Pool struct {
	cfg             *config.Config
	tgt             target.Target
	provider        provider.VMProvider
	registrar       registrar.RunnerRegistrar
	spawner         *spawn.Spawner
	leases          *lease.Registry
	session         *lease.AgentSession
	logger          *slog.Logger
	effectivePrefix string // VMNamePrefix + hostID + "-"; scopes reclaim to this host

	mu           sync.Mutex
	active       int
	failures     int
	backoffUntil time.Time
	shutdown     bool
	drain        bool
	lowDisk      bool
	fatalErr     error
	fatalCh      chan error
	inFlight     sync.WaitGroup
	inFlightVMs  map[string]struct{}
}

func New(cfg *config.Config, tgt target.Target, vmProvider provider.VMProvider, reg registrar.RunnerRegistrar, logger *slog.Logger) *Pool {
	if logger == nil {
		logger = slog.Default()
	}
	leases := lease.NewRegistry(cfg.StateDir)
	hostID := loadOrInitHostID(cfg.StateDir)
	effectivePrefix := cfg.VMNamePrefix + hostID + "-"
	return &Pool{
		cfg:             cfg,
		tgt:             tgt,
		provider:        vmProvider,
		registrar:       reg,
		leases:          leases,
		spawner:         spawn.New(spawn.Options{Config: cfg, Target: tgt, Provider: vmProvider, Registrar: reg, Leases: leases, Logger: logger}),
		logger:          logger,
		effectivePrefix: effectivePrefix,
		fatalCh:         make(chan error, 1),
		inFlightVMs:     make(map[string]struct{}),
	}
}

func (p *Pool) Run(ctx context.Context) error {
	session, err := p.leases.BeginAgentSession()
	if err != nil {
		return fmt.Errorf("begin agent session: %w", err)
	}
	p.session = session
	p.spawner.SetSession(session)

	if err := p.startupReclaim(ctx); err != nil {
		if registrar.IsUnauthorized(err) {
			return err
		}
		p.logger.Warn("startup reclaim failed", "error", err)
	}

	p.logger.Info("pool loop started",
		"pool_size", p.cfg.PoolSize,
		"check_interval", p.cfg.PoolCheckInterval.Duration().String(),
		"reconciliation_interval", p.cfg.ReconciliationInterval.Duration().String(),
		"effective_prefix", p.effectivePrefix,
	)
	p.tick(ctx)

	ticker := time.NewTicker(p.cfg.PoolCheckInterval.Duration())
	defer ticker.Stop()

	reconcileEvery := p.cfg.ReconciliationInterval.Duration()
	if reconcileEvery <= 0 {
		reconcileEvery = config.DefaultReconciliationInterval
	}
	reconcileTicker := time.NewTicker(reconcileEvery)
	defer reconcileTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			return p.drainAndWait()
		case err := <-p.fatalCh:
			_ = p.drainAndWait()
			return err
		case <-ticker.C:
			p.tick(ctx)
		case <-reconcileTicker.C:
			if err := p.reclaim(ctx, false); err != nil {
				if registrar.IsUnauthorized(err) {
					_ = p.drainAndWait()
					return err
				}
				p.logger.Warn("reconciliation failed", "error", err)
			}
		}
	}
}

func (p *Pool) BeginShutdown() {
	p.mu.Lock()
	p.shutdown = true
	p.mu.Unlock()
}

func (p *Pool) drainAndWait() error {
	p.mu.Lock()
	p.drain = true
	p.mu.Unlock()
	p.inFlight.Wait()
	return nil
}

func (p *Pool) tick(ctx context.Context) {
	p.mu.Lock()
	if p.shutdown || p.drain || p.lowDisk {
		p.mu.Unlock()
		return
	}
	if time.Now().Before(p.backoffUntil) {
		p.mu.Unlock()
		return
	}
	if p.active >= p.cfg.PoolSize {
		p.mu.Unlock()
		return
	}
	p.active++
	p.mu.Unlock()

	p.inFlight.Add(1)
	go func() {
		defer p.inFlight.Done()
		defer func() {
			p.mu.Lock()
			p.active--
			p.mu.Unlock()
		}()

		vmName, err := p.newVMName()
		if err != nil {
			p.recordFailure(err)
			return
		}

		p.logger.Info("spawning runner", "vm", vmName)

		p.mu.Lock()
		p.inFlightVMs[vmName] = struct{}{}
		p.mu.Unlock()
		defer func() {
			p.mu.Lock()
			delete(p.inFlightVMs, vmName)
			p.mu.Unlock()
		}()

		if err := p.spawner.Run(ctx, vmName); err != nil {
			if registrar.IsUnauthorized(err) {
				p.reportFatal(err)
				return
			}
			p.recordFailure(err)
			p.logger.Warn("spawn failed", "vm", vmName, "error", err)
			return
		}
		p.logger.Info("runner finished", "vm", vmName)
		p.mu.Lock()
		p.failures = 0
		p.backoffUntil = time.Time{}
		p.mu.Unlock()
	}()
}

func (p *Pool) reportFatal(err error) {
	p.mu.Lock()
	already := p.fatalErr != nil
	if !already {
		p.fatalErr = err
		p.shutdown = true
	}
	p.mu.Unlock()
	if already {
		return
	}
	p.logger.Error("fatal authorization error; stopping", "error", err)
	select {
	case p.fatalCh <- err:
	default:
	}
}

func (p *Pool) recordFailure(err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failures++
	backoff := time.Duration(p.failures) * 10 * time.Second
	if backoff > 5*time.Minute {
		backoff = 5 * time.Minute
	}
	p.backoffUntil = time.Now().Add(backoff)
	p.logger.Warn("spawn backoff", "failures", p.failures, "backoff", backoff, "error", err)
}

func (p *Pool) newVMName() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s%s", p.effectivePrefix, hex.EncodeToString(buf)), nil
}

// loadOrInitHostID returns a stable, sanitized identifier for this host.
// It reads from {stateDir}/host_id when present, otherwise derives one from
// os.Hostname() and writes it back so the value survives hostname changes.
func loadOrInitHostID(stateDir string) string {
	path := filepath.Join(stateDir, "host_id")
	if data, err := os.ReadFile(path); err == nil {
		if id := strings.TrimSpace(string(data)); id != "" {
			return id
		}
	}
	hostname, _ := os.Hostname()
	id := sanitizeHostname(hostname)
	if id == "" {
		buf := make([]byte, 4)
		_, _ = rand.Read(buf)
		id = hex.EncodeToString(buf)
	}
	// Best-effort persist; failure is non-fatal.
	_ = os.MkdirAll(stateDir, 0o755)
	_ = os.WriteFile(path, []byte(id), 0o644)
	return id
}

// sanitizeHostname converts a hostname to a safe VM-name component:
// lowercase, non-alphanumeric characters replaced with dashes, consecutive
// dashes collapsed, leading/trailing dashes stripped, capped at 24 characters.
func sanitizeHostname(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		} else {
			b.WriteRune('-')
		}
	}
	result := strings.Trim(b.String(), "-")
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	if len(result) > 24 {
		result = result[:24]
		result = strings.TrimRight(result, "-")
	}
	return result
}

func (p *Pool) PurgeAll(ctx context.Context, dryRun bool) ([]provider.VM, []int64, error) {
	return p.purgeAllManaged(ctx, dryRun)
}
