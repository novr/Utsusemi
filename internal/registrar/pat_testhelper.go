package registrar

import (
	"context"

	"github.com/novr/utsusemi/internal/target"
)

func (r *GitHubPATRegistrar) createJITWithBase(ctx context.Context, base string, tgt target.Target, labels []string, name string) (JITConfig, error) {
	orig := r.api.baseURL
	r.api.baseURL = base
	defer func() { r.api.baseURL = orig }()
	return r.CreateJIT(ctx, tgt, labels, name)
}
