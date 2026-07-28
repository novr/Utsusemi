package registrar

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/novr/utsusemi/internal/hostcredential"
	"github.com/novr/utsusemi/internal/target"
)

func (r *BrokerRegistrar) requestWithCredential(
	ctx context.Context,
	tgt target.Target,
	fn func(token string) error,
) error {
	token, err := r.credentials.EnsureFresh(ctx, tgt, false)
	if err != nil {
		return err
	}
	if err := fn(token); err == nil {
		return nil
	} else if !IsUnauthorized(err) {
		return err
	}

	token, err = r.credentials.EnsureFresh(ctx, tgt, true)
	if err != nil {
		return err
	}
	if err := fn(token); err != nil {
		if IsUnauthorized(err) {
			user := r.credentials.GitHubUser()
			r.logCredentialFailure("broker_unauthorized", user, err)
			if user != "" {
				return fmt.Errorf("credential rejected for GitHub user %q: %w; %s", user, err, hostcredential.ReconfigureAppHint)
			}
			return fmt.Errorf("%w; %s", err, hostcredential.ReconfigureAppHint)
		}
		return err
	}
	return nil
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
		"action", hostcredential.ReconfigureAppAuthAction,
	)
}
