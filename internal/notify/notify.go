package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/novr/utsusemi/internal/keychain"
)

const (
	CodeAgentFatal   = "agent_fatal"
	CodeDiskBlocked  = "disk_blocked"
	CodePoolStuck    = "pool_stuck"
	CodeReadyFailing = "ready_failing"

	EnvWebhookURL = "UTSUSEMI_ALERT_WEBHOOK"

	AlertService = "utsusemi-alerts"
	AlertAccount = "webhook"

	DefaultStuckAfter        = 15 * time.Minute
	DefaultReadyFailingAfter = 3
	PostTimeout              = 2 * time.Second
)

type Event struct {
	Code    string
	Message string
	HostID  string
	Target  string
	Detail  string
	At      time.Time
}

type Notifier interface {
	Alert(ctx context.Context, e Event) error
}

type Nop struct{}

func (Nop) Alert(context.Context, Event) error { return nil }

type Webhook struct {
	URL    string
	Client *http.Client
}

func NewWebhook(url string) *Webhook {
	return &Webhook{
		URL: url,
		Client: &http.Client{
			Timeout: PostTimeout,
		},
	}
}

type webhookPayload struct {
	Text   string `json:"text"`
	Code   string `json:"code"`
	HostID string `json:"host_id,omitempty"`
	Target string `json:"target,omitempty"`
	At     string `json:"at"`
	Detail string `json:"detail,omitempty"`
}

func (w *Webhook) Alert(ctx context.Context, e Event) error {
	if w == nil || w.URL == "" {
		return nil
	}
	at := e.At
	if at.IsZero() {
		at = time.Now().UTC()
	}
	msg := e.Message
	if msg == "" {
		msg = e.Code
	}
	// Slack Incoming Webhooks display `text` only; include code/host/target there.
	text := fmt.Sprintf("[%s]", e.Code)
	if e.HostID != "" {
		text += " host=" + e.HostID
	}
	if e.Target != "" {
		text += " target=" + e.Target
	}
	text += " " + msg
	if e.Detail != "" {
		text += ": " + e.Detail
	}
	body, err := json.Marshal(webhookPayload{
		Text:   text,
		Code:   e.Code,
		HostID: e.HostID,
		Target: e.Target,
		At:     at.UTC().Format(time.RFC3339),
		Detail: e.Detail,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, w.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook status %d", resp.StatusCode)
	}
	return nil
}

func ValidateWebhookURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("invalid webhook URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("webhook URL must use http or https")
	}
	if u.Host == "" {
		return fmt.Errorf("webhook URL must include a host")
	}
	return nil
}

func ResolveWebhookURL(store keychain.Store) (string, error) {
	if v := strings.TrimSpace(os.Getenv(EnvWebhookURL)); v != "" {
		return v, nil
	}
	if store == nil {
		return "", nil
	}
	v, err := store.Get(AlertService, AlertAccount)
	if errors.Is(err, keychain.ErrNotFound) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(v), nil
}

func WebhookConfigured(store keychain.Store) bool {
	u, err := ResolveWebhookURL(store)
	if err != nil || u == "" {
		return false
	}
	return ValidateWebhookURL(u) == nil
}

func Resolve(store keychain.Store, logger *slog.Logger) Notifier {
	u, err := ResolveWebhookURL(store)
	if err != nil {
		if logger != nil {
			logger.Warn("resolve alert webhook", "error", err)
		}
		return Nop{}
	}
	if u == "" {
		return Nop{}
	}
	if err := ValidateWebhookURL(u); err != nil {
		if logger != nil {
			logger.Warn("invalid alert webhook URL", "error", err)
		}
		return Nop{}
	}
	return NewWebhook(u)
}

// Snapshot is the pool view passed to Monitor.Evaluate each tick.
type Snapshot struct {
	Active                int
	PoolSize              int
	LowDisk               bool
	LastSpawnErr          string
	ConsecutiveReadyFails int
}

// Clock selects how a Rule measures time-to-fire.
type Clock int

const (
	// ClockSustain advances only while Active; resets when inactive.
	ClockSustain Clock = iota
	// ClockWarm uses lastGoodAt; in-flight must not reset the clock.
	ClockWarm
	// ClockExternal fires as soon as Active (counter thresholds, etc.).
	ClockExternal
)

// Rule describes one alert condition evaluated by Monitor.
type Rule struct {
	Code                   string
	Clock                  Clock
	After                  time.Duration // Sustain/Warm; 0 means stuckAfter. Ignored for External.
	ClearFiredWhenInactive bool          // false: keep fired until Clear (pool_stuck)
	Active                 func(Snapshot) bool
	Message                func(Snapshot) string
	Detail                 func(Snapshot) string
}

type MonitorOptions struct {
	Notifier   Notifier
	StuckAfter time.Duration
	HostID     string
	Target     string
	Logger     *slog.Logger
	Now        func() time.Time
	Rules      []Rule // nil → defaultRules()
}

// Monitor tracks sustained pool conditions and dedupes webhook delivery.
type Monitor struct {
	notifier   Notifier
	stuckAfter time.Duration
	hostID     string
	target     string
	logger     *slog.Logger
	now        func() time.Time
	rules      []Rule

	mu         sync.Mutex
	fired      map[string]bool
	since      map[string]time.Time // ClockSustain only
	lastGoodAt time.Time            // ClockWarm; Clear(pool_stuck) / NewMonitor
}

func defaultRules() []Rule {
	return []Rule{
		{
			Code:                   CodeDiskBlocked,
			Clock:                  ClockSustain,
			ClearFiredWhenInactive: true,
			Active:                 func(s Snapshot) bool { return s.LowDisk },
			Message:                func(Snapshot) string { return "utsusemi spawn paused: low disk" },
		},
		{
			Code:                   CodePoolStuck,
			Clock:                  ClockWarm,
			ClearFiredWhenInactive: false,
			Active: func(s Snapshot) bool {
				if s.LowDisk || s.PoolSize <= 0 {
					return false
				}
				if s.Active == 0 {
					return true
				}
				// Full pool running healthy jobs must not alert; saturated + ready
				// failures indicates slots are occupied but not becoming warm.
				if s.Active >= s.PoolSize && s.ConsecutiveReadyFails > 0 {
					return true
				}
				return false
			},
			Message: func(Snapshot) string {
				return "utsusemi pool: no warm capacity (empty or saturated)"
			},
			Detail: func(s Snapshot) string { return s.LastSpawnErr },
		},
		{
			Code:                   CodeReadyFailing,
			Clock:                  ClockExternal,
			ClearFiredWhenInactive: true,
			Active: func(s Snapshot) bool {
				return s.ConsecutiveReadyFails >= DefaultReadyFailingAfter
			},
			Message: func(Snapshot) string {
				return "utsusemi spawn: repeated wait-for-ready failures"
			},
			Detail: func(s Snapshot) string { return s.LastSpawnErr },
		},
	}
}

func NewMonitor(opts MonitorOptions) *Monitor {
	n := opts.Notifier
	if n == nil {
		n = Nop{}
	}
	stuckAfter := opts.StuckAfter
	if stuckAfter <= 0 {
		stuckAfter = DefaultStuckAfter
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	rules := opts.Rules
	if rules == nil {
		rules = defaultRules()
	}
	start := now()
	return &Monitor{
		notifier:   n,
		stuckAfter: stuckAfter,
		hostID:     opts.HostID,
		target:     opts.Target,
		logger:     logger,
		now:        now,
		rules:      rules,
		fired:      make(map[string]bool),
		since:      make(map[string]time.Time),
		lastGoodAt: start,
	}
}

func (m *Monitor) Clear(code string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.fired, code)
	delete(m.since, code)
	if code == CodePoolStuck {
		m.lastGoodAt = m.now()
	}
}

func (m *Monitor) AlertFatal(ctx context.Context, err error) {
	if m == nil || err == nil {
		return
	}
	m.fire(ctx, Event{
		Code:    CodeAgentFatal,
		Message: "utsusemi agent stopped",
		Detail:  err.Error(),
	})
}

func (m *Monitor) Evaluate(ctx context.Context, s Snapshot) {
	if m == nil {
		return
	}
	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()

	for _, r := range m.rules {
		active := false
		if r.Active != nil {
			active = r.Active(s)
		}
		if !active {
			if r.ClearFiredWhenInactive {
				delete(m.fired, r.Code)
			}
			if r.Clock == ClockSustain {
				delete(m.since, r.Code)
			}
			continue
		}
		switch r.Clock {
		case ClockExternal:
			m.fireLocked(ctx, m.eventFrom(r, s))
		case ClockSustain:
			after := r.After
			if after <= 0 {
				after = m.stuckAfter
			}
			if m.since[r.Code].IsZero() {
				m.since[r.Code] = now
			}
			if now.Sub(m.since[r.Code]) >= after {
				m.fireLocked(ctx, m.eventFrom(r, s))
			}
		case ClockWarm:
			after := r.After
			if after <= 0 {
				after = m.stuckAfter
			}
			if now.Sub(m.lastGoodAt) >= after {
				m.fireLocked(ctx, m.eventFrom(r, s))
			}
		}
	}
}

func (m *Monitor) eventFrom(r Rule, s Snapshot) Event {
	e := Event{Code: r.Code}
	if r.Message != nil {
		e.Message = r.Message(s)
	}
	if r.Detail != nil {
		e.Detail = r.Detail(s)
	}
	return e
}

func (m *Monitor) fire(ctx context.Context, e Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fireLocked(ctx, e)
}

func (m *Monitor) fireLocked(ctx context.Context, e Event) {
	if m.fired[e.Code] {
		return
	}
	m.fired[e.Code] = true
	e.HostID = m.hostID
	e.Target = m.target
	if e.At.IsZero() {
		e.At = m.now().UTC()
	}
	notifier := m.notifier
	logger := m.logger
	// Release before HTTP so Evaluate/Clear are not blocked for up to PostTimeout.
	m.mu.Unlock()
	defer m.mu.Lock()

	alertCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), PostTimeout)
	defer cancel()
	if err := notifier.Alert(alertCtx, e); err != nil {
		logger.Error("alert webhook failed", "code", e.Code, "error", err)
	}
}
