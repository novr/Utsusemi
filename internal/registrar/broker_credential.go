package registrar

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"

	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/instancelock"
	"github.com/novr/utsusemi/internal/target"
)

func (r *BrokerRegistrar) loadCredential() (hostcredential.Loaded, error) {
	raw, err := r.store.Get(r.cfg.CredentialService(), r.cfg.CredentialAccount())
	if err != nil {
		return hostcredential.Loaded{}, fmt.Errorf("credential missing from keychain: %w", err)
	}
	if strings.TrimSpace(raw) == "" || raw == "-" {
		return hostcredential.Loaded{}, fmt.Errorf("invalid credential in keychain; run `utsusemi configure app` again")
	}
	return hostcredential.Load(raw)
}

func (r *BrokerRegistrar) ensureFresh(ctx context.Context, tgt target.Target, force bool) (string, error) {
	r.refreshMu.Lock()
	defer r.refreshMu.Unlock()

	lock, err := instancelock.AcquireBlocking(filepath.Join(r.cfg.StateDir, "credential.refresh.lock"))
	if err != nil {
		return "", err
	}
	defer lock.Release()

	loaded, err := r.loadCredential()
	if err != nil {
		return "", err
	}

	needs, err := hostcredential.NeedsRefresh(loaded.HostJWT, force)
	if err != nil {
		return "", err
	}
	if !needs {
		return loaded.HostJWT, nil
	}

	loaded, err = r.loadCredential()
	if err != nil {
		return "", err
	}
	if !force {
		stillNeeds, err := hostcredential.NeedsRefresh(loaded.HostJWT, false)
		if err != nil {
			return "", err
		}
		if !stillNeeds {
			return loaded.HostJWT, nil
		}
	}

	oauth := r.oauth
	if oauth == nil {
		oauth = &hostcredential.OAuthClient{HTTPClient: r.client}
	}

	refreshed, err := oauth.RefreshGitHubToken(ctx, hostcredential.PublicAppClientID, loaded.RefreshToken)
	if err != nil {
		r.logCredentialFailure("refresh", loaded.GitHubUser, err)
		return "", r.refreshError(loaded.GitHubUser, err)
	}

	partial, err := hostcredential.NewBundle(loaded.HostJWT, refreshed.RefreshToken, loaded.GitHubUser)
	if err != nil {
		return "", err
	}
	if err := r.store.Set(r.cfg.CredentialService(), r.cfg.CredentialAccount(), partial); err != nil {
		return "", err
	}

	hostJWT, _, err := hostcredential.ExchangeHostJWT(ctx, r.client, r.baseURL, refreshed.AccessToken, tgt)
	if err != nil {
		r.logCredentialFailure("exchange", loaded.GitHubUser, err)
		return "", r.refreshError(loaded.GitHubUser, err)
	}

	final, err := hostcredential.NewBundle(hostJWT, refreshed.RefreshToken, loaded.GitHubUser)
	if err != nil {
		return "", err
	}
	if err := r.store.Set(r.cfg.CredentialService(), r.cfg.CredentialAccount(), final); err != nil {
		return "", err
	}
	return hostJWT, nil
}

func (r *BrokerRegistrar) requestWithCredential(
	ctx context.Context,
	tgt target.Target,
	fn func(token string) error,
) error {
	token, err := r.ensureFresh(ctx, tgt, false)
	if err != nil {
		return err
	}
	if err := fn(token); err == nil {
		return nil
	} else if !isUnauthorized(err) {
		return err
	}

	token, err = r.ensureFresh(ctx, tgt, true)
	if err != nil {
		return err
	}
	if err := fn(token); err != nil {
		if isUnauthorized(err) {
			user := r.authorizedGitHubUser()
			r.logCredentialFailure("broker_unauthorized", user, err)
			if user != "" {
				return fmt.Errorf("credential rejected for GitHub user %q: %w; run `utsusemi configure app` again", user, err)
			}
			return fmt.Errorf("%w; run `utsusemi configure app` again", err)
		}
		return err
	}
	return nil
}

func (r *BrokerRegistrar) authorizedGitHubUser() string {
	loaded, err := r.loadCredential()
	if err != nil {
		return ""
	}
	return loaded.GitHubUser
}

func (r *BrokerRegistrar) SetLogger(logger *slog.Logger) {
	r.logger = logger
}

func (r *BrokerRegistrar) log() *slog.Logger {
	if r.logger != nil {
		return r.logger
	}
	return slog.Default()
}

func (r *BrokerRegistrar) logCredentialFailure(stage, githubUser string, err error) {
	r.log().Error(
		"hosted app credential update failed",
		"stage", stage,
		"github_user", githubUser,
		"error", err,
		"action", "run `utsusemi configure app` again or restore GitHub App authorization for this user",
	)
}

func (r *BrokerRegistrar) refreshError(githubUser string, err error) error {
	if githubUser == "" {
		return err
	}
	return fmt.Errorf("credential update failed for GitHub user %q: %w", githubUser, err)
}
