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
	CodeAgentFatal  = "agent_fatal"
	CodeDiskBlocked = "disk_blocked"
	CodePoolStuck   = "pool_stuck"

	EnvWebhookURL = "UTSUSEMI_ALERT_WEBHOOK"

	AlertService = "utsusemi-alerts"
	AlertAccount = "webhook"

	DefaultStuckAfter = 15 * time.Minute
	PostTimeout       = 2 * time.Second
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

type PoolState struct {
	Active       int
	PoolSize     int
	LowDisk      bool
	LastSpawnErr string
}

type MonitorOptions struct {
	Notifier   Notifier
	StuckAfter time.Duration
	HostID     string
	Target     string
	Logger     *slog.Logger
	Now        func() time.Time
}

// Monitor tracks sustained pool conditions and dedupes webhook delivery.
type Monitor struct {
	notifier   Notifier
	stuckAfter time.Duration
	hostID     string
	target     string
	logger     *slog.Logger
	now        func() time.Time

	mu           sync.Mutex
	fired        map[string]bool
	lastGoodAt   time.Time // last successful capacity (or monitor start); drives pool_stuck
	lowDiskSince time.Time
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
	start := now()
	return &Monitor{
		notifier:   n,
		stuckAfter: stuckAfter,
		hostID:     opts.HostID,
		target:     opts.Target,
		logger:     logger,
		now:        now,
		fired:      make(map[string]bool),
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
	if code == CodePoolStuck {
		m.lastGoodAt = m.now()
	}
	if code == CodeDiskBlocked {
		m.lowDiskSince = time.Time{}
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

func (m *Monitor) Evaluate(ctx context.Context, state PoolState) {
	if m == nil {
		return
	}
	now := m.now()

	m.mu.Lock()
	defer m.mu.Unlock()

	if state.LowDisk {
		if m.lowDiskSince.IsZero() {
			m.lowDiskSince = now
		}
		if now.Sub(m.lowDiskSince) >= m.stuckAfter {
			m.fireLocked(ctx, Event{
				Code:    CodeDiskBlocked,
				Message: "utsusemi spawn paused: low disk",
			})
		}
		return
	}

	if !m.lowDiskSince.IsZero() {
		delete(m.fired, CodeDiskBlocked)
		m.lowDiskSince = time.Time{}
	}

	if state.PoolSize <= 0 {
		return
	}
	// In-flight spawn/job is not warm capacity yet, but must not refresh lastGoodAt:
	// otherwise failure loops (active flickers 0→1→0) never reach pool_stuck.
	if state.Active > 0 {
		return
	}
	if now.Sub(m.lastGoodAt) >= m.stuckAfter {
		m.fireLocked(ctx, Event{
			Code:    CodePoolStuck,
			Message: "utsusemi pool empty: no warm capacity",
			Detail:  state.LastSpawnErr,
		})
	}
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
