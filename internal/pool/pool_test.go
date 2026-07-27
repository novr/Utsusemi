package pool

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/novr/utsusemi/internal/config"
	"github.com/novr/utsusemi/internal/provider"
	"github.com/novr/utsusemi/internal/registrar"
	"github.com/novr/utsusemi/internal/target"
)

type noopRegistrar struct{}

func (noopRegistrar) CreateJIT(ctx context.Context, tgt target.Target, labels []string, name string) (registrar.JITConfig, error) {
	return registrar.JITConfig{Encoded: "jit", Runner: registrar.Runner{ID: 1, Name: name}}, nil
}
func (noopRegistrar) DeleteRunner(ctx context.Context, tgt target.Target, runnerID int64) error {
	return nil
}
func (noopRegistrar) ListRunners(ctx context.Context, tgt target.Target, prefix string) ([]registrar.Runner, error) {
	return nil, nil
}
func (noopRegistrar) ValidateCredential(ctx context.Context, service, account string) error { return nil }

func TestPoolBackoffOnFailure(t *testing.T) {
	exec := provider.NewFakeExecutor()
	exec.FailClone = context.Canceled

	cfg := &config.Config{
		BaseImage:              "image",
		RunnerVersion:          "2.336.0",
		Labels:                 []string{"self-hosted"},
		PoolSize:               1,
		PoolCheckInterval:      config.Duration(20 * time.Millisecond),
		ReconciliationInterval: config.Duration(time.Hour),
		SpawnTimeout:           config.Duration(time.Second),
		JobTimeout:             config.Duration(time.Second),
		MinFreeDiskGB:          1,
		VMNamePrefix:           "utsusemi-",
	}
	p := New(cfg, target.Target{Type: target.TypeRepo, Owner: "a", Repo: "b"}, provider.NewTartProvider(exec), noopRegistrar{}, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_ = p.Run(ctx)

	p.mu.Lock()
	defer p.mu.Unlock()
	if p.failures == 0 {
		t.Fatal("expected failures")
	}
}
