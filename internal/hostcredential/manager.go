package hostcredential

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/target"
)

type CredentialStore interface {
	Get(service, account string) (string, error)
	Set(service, account, secret string) error
}

type OnRefreshFailure func(stage, githubUser string, err error)

type Manager struct {
	store      CredentialStore
	service    string
	account    string
	brokerURL  string
	lockPath   string
	oauth      *OAuthClient
	httpClient *http.Client
	onFailure  OnRefreshFailure
	refreshMu  sync.Mutex
}

type ManagerOptions struct {
	Store      CredentialStore
	Service    string
	Account    string
	BrokerURL  string
	LockPath   string
	OAuth      *OAuthClient
	HTTPClient *http.Client
	OnFailure  OnRefreshFailure
}

func NewManager(opts ManagerOptions) *Manager {
	return &Manager{
		store:      opts.Store,
		service:    opts.Service,
		account:    opts.Account,
		brokerURL:  strings.TrimRight(opts.BrokerURL, "/"),
		lockPath:   opts.LockPath,
		oauth:      opts.OAuth,
		httpClient: opts.HTTPClient,
		onFailure:  opts.OnFailure,
	}
}

func (m *Manager) SetOAuth(oauth *OAuthClient) {
	m.oauth = oauth
}

func (m *Manager) Load() (Loaded, error) {
	raw, err := m.store.Get(m.service, m.account)
	if err != nil {
		return Loaded{}, fmt.Errorf("credential missing from keychain: %w", err)
	}
	if strings.TrimSpace(raw) == "" || raw == "-" {
		return Loaded{}, fmt.Errorf("invalid credential in keychain; %s", ReconfigureAppHint)
	}
	return Load(raw)
}

func (m *Manager) GitHubUser() string {
	loaded, err := m.Load()
	if err != nil {
		return ""
	}
	return loaded.GitHubUser
}

func (m *Manager) EnsureFresh(ctx context.Context, tgt target.Target, force bool) (string, error) {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()

	lock, err := instancelock.AcquireBlocking(m.lockPath)
	if err != nil {
		return "", err
	}
	defer lock.Release()

	loaded, err := m.Load()
	if err != nil {
		return "", err
	}

	needs, err := NeedsRefresh(loaded.HostJWT, force)
	if err != nil {
		return "", err
	}
	if !needs {
		return loaded.HostJWT, nil
	}

	loaded, err = m.Load()
	if err != nil {
		return "", err
	}
	if !force {
		stillNeeds, err := NeedsRefresh(loaded.HostJWT, false)
		if err != nil {
			return "", err
		}
		if !stillNeeds {
			return loaded.HostJWT, nil
		}
	}

	refreshed, err := m.oauthClient().RefreshGitHubToken(ctx, PublicAppClientID, loaded.RefreshToken)
	if err != nil {
		m.fail("refresh", loaded.GitHubUser, err)
		return "", UserRefreshError(loaded.GitHubUser, err)
	}

	partial, err := NewBundle(loaded.HostJWT, refreshed.RefreshToken, loaded.GitHubUser)
	if err != nil {
		return "", err
	}
	if err := m.store.Set(m.service, m.account, partial); err != nil {
		return "", err
	}

	hostJWT, _, err := ExchangeHostJWT(ctx, m.httpClient, m.brokerURL, refreshed.AccessToken, tgt)
	if err != nil {
		m.fail("exchange", loaded.GitHubUser, err)
		return "", UserRefreshError(loaded.GitHubUser, err)
	}

	final, err := NewBundle(hostJWT, refreshed.RefreshToken, loaded.GitHubUser)
	if err != nil {
		return "", err
	}
	if err := m.store.Set(m.service, m.account, final); err != nil {
		return "", err
	}
	return hostJWT, nil
}

func (m *Manager) oauthClient() *OAuthClient {
	if m.oauth != nil {
		return m.oauth
	}
	return &OAuthClient{HTTPClient: m.httpClient}
}

func (m *Manager) fail(stage, githubUser string, err error) {
	if m.onFailure != nil {
		m.onFailure(stage, githubUser, err)
	}
}

func UserRefreshError(githubUser string, err error) error {
	if githubUser == "" {
		return err
	}
	return fmt.Errorf("credential update failed for GitHub user %q: %w", githubUser, err)
}
