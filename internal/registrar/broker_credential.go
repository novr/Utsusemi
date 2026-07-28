package registrar

import (
	"context"
	"fmt"
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
	if loaded.Legacy {
		if force {
			return "", fmt.Errorf("credential cannot be refreshed; run `utsusemi configure app` again")
		}
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
		return "", err
	}

	partial, err := hostcredential.NewBundle(loaded.HostJWT, refreshed.RefreshToken)
	if err != nil {
		return "", err
	}
	if err := r.store.Set(r.cfg.CredentialService(), r.cfg.CredentialAccount(), partial); err != nil {
		return "", err
	}

	hostJWT, _, err := hostcredential.ExchangeHostJWT(ctx, r.client, r.baseURL, refreshed.AccessToken, tgt)
	if err != nil {
		return "", err
	}

	final, err := hostcredential.NewBundle(hostJWT, refreshed.RefreshToken)
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
			return fmt.Errorf("%w; run `utsusemi configure app` again", err)
		}
		return err
	}
	return nil
}
