# AGENTS.md

Maintainer and agent reference. User-facing docs live in [README.md](README.md) only.

## Concerns

- **Ephemeral job VMs** — each `spawn.Run` clones a VM, runs one Actions job, then deletes the VM and GitHub runner. The pool only keeps up to `pool_size` such cycles in flight; it does not retain long-lived runner VMs.
- **Pool availability** — maintain warm capacity without leaking VMs, runners, disk, or leases.
- **Single agent process** — at most one `utsusemi run` / `brew services` instance per config (`StateDir/utsusemi.lock`).
- **Credential safety** — secrets in Keychain only; `config.yaml` is non-secret.
- **Hosted app renewability** — device flow once; OAuth refresh + broker exchange thereafter while the host runs.
- **Registration modes** — `hosted_app` (organization + broker) and `github_pat` (organization or repository, direct GitHub API). Keep code paths separate.
- **macOS / Tart** — Keychain, NAT lease limits, optional Softnet; Tart CLI stays behind `internal/provider`.

## Responsibilities

| Area | Package / path | Owns |
|------|----------------|------|
| CLI | `cmd/utsusemi` | Cobra commands, prompts, `loadConfigRuntime` / `loadValidatedRuntime` / `buildAgentFromRuntime`, configure flows |
| Agent | `internal/agent` | `utsusemi.lock` for `run`, signal drain, delegates to pool |
| Pool | `internal/pool` | Spawn loop, backoff, in-flight tracking, **reclaim**, **purge** (`clean`) |
| Spawn | `internal/spawn` | One job lifecycle: clone → register → bootstrap → wait → teardown |
| VMs | `internal/provider` | Tart / executor abstraction |
| Runners | `internal/registrar` | `RunnerRegistrar` interface |
| PAT mode | `internal/registrar/pat.go` | GitHub REST with PAT from Keychain |
| Broker mode | `internal/registrar/broker.go` | Broker HTTP; `requestWithCredential` + `hostcredential.Manager` |
| Host credentials | `internal/hostcredential` | Bundle (hosted app), device flow, OAuth refresh, exchange, `EnsureFresh` |
| Targets | `internal/target` | Org/repo target types, parsing, `RequireOrg`, lowercase org |
| Config | `internal/config` | YAML model, defaults, `Validate`, `ValidateBrokerURL` |
| Leases | `internal/lease` | On-disk VM ↔ runner ↔ agent session |
| Status | `internal/status` | Read-only aggregation + text format for `utsusemi status` |
| Listing | `internal/listing` | VM/runner rows for `utsusemi list` |
| Credential view | `internal/credentialview` | Keychain credential summary (no refresh) |
| Keychain | `internal/keychain` | Platform secret store |
| Locks | `internal/instancelock` | `utsusemi.lock` (agent/clean), used with blocking flock for credential refresh |
| Broker (cloud) | `worker/` | Host JWT issue/verify, GitHub App calls, JIT/list/delete proxy |
| Release | `.github/workflows` | macOS binary, GitHub release, Homebrew formula dispatch |
| Homebrew formula | `novr/homebrew-taps` | `Formula/utsusemi.rb` (separate repo) |

## Boundaries

### README vs this file

| README | AGENTS.md |
|--------|-----------|
| Install, configure, run, clean | Architecture, invariants, contributor workflow |
| No broker API or build internals | Broker routes, deploy, test constraints |

### Process and locks

| Lock | Path | Purpose |
|------|------|---------|
| Agent | `{StateDir}/utsusemi.lock` | Non-blocking exclusive flock; held for entire `agent.Run` |
| Credential refresh | `{StateDir}/credential.refresh.lock` | Blocking flock inside `Manager.EnsureFresh` |

- `clean` must acquire `utsusemi.lock` (agent stopped) before `PurgeAll`.
- `validate` and `run` can call `EnsureFresh` concurrently; credential refresh serializes on `credential.refresh.lock`, not `utsusemi.lock`.

### CLI (`cmd/utsusemi`)

- Wire dependencies only; no pool logic, Tart commands, or OAuth/device-flow implementation.
- `configure app` → `hostcredential.DeviceFlowClient`.
- `configure token` → raw PAT in Keychain via `saveCredential`.
- **`validate`** — config + credential API check (`loadValidatedRuntime`).
- **`status`** — local ops summary via `internal/status.Collect` (`loadConfigRuntime`, no network).
- **`list [vms|runners]`** — VM and/or runner rows via `internal/listing.Collect` (`loadValidatedRuntime`, network).

### Shell completion

- Cobra `completion` generates zsh/bash/fish scripts. The Homebrew formula installs them via `generate_completions_from_executable(..., shell_parameter_format: :cobra)`.
- When adding, removing, or renaming commands, subcommands, or positional args, update `cmd/utsusemi/completion.go` (`registerListCompletions`, etc.) and `completion_test.go`. Do not document end-user setup in README; leave that to the formula.

### Credentials

| Mode | Keychain contents |
|------|-------------------|
| `hosted_app` | JSON bundle (`hostcredential.Bundle` v1): host JWT, refresh token, GitHub user |
| `github_pat` | Raw PAT string |

Hosted app rules:

- `Load` accepts bundle JSON only; legacy bare JWT is rejected.
- **`hostcredential.Manager`** owns refresh → exchange → Keychain update.
- After OAuth refresh, persist bundle with the **new refresh token** before exchange (refresh tokens are single-use). The partial write may still carry the previous host JWT until exchange succeeds.
- **`BrokerRegistrar`** uses `Manager.EnsureFresh` via `requestWithCredential`; do not duplicate store/oauth/lock logic there.
- Broker **401** → `EnsureFresh(..., force=true)` and one retry; other HTTP errors do not force refresh.

### Registration modes

- `hosted_app` requires an **organization** target (`config.Validate`).
- `github_pat` supports organization or repository targets.
- New backends: implement `RunnerRegistrar`; do not branch on `registration.mode` in `pool` or `spawn`.

### Pool: reclaim vs purge

| | reclaim | purge (`utsusemi clean`) |
|--|---------|--------------------------|
| When | Startup + periodic during `run` | Manual; agent must be stopped |
| Scope | Stale/orphan per `reclaim_policy` | All prefix-matched VMs and runners |
| In-flight VMs | Skipped | Not skipped |
| Leases | `RemoveLease` per VM | `ClearLeases` |

Shared teardown: `stopAndDeleteManagedVM` (`managed_vm.go`).

### Multi-host safety

Each `Pool` carries an `effectivePrefix = cfg.VMNamePrefix + hostID + "-"`.

- **hostID** is loaded from `{StateDir}/host_id`. On first run, it is derived
  from `os.Hostname()` (sanitized: lowercase, non-alphanumeric → dash, max 24
  chars) and written to that file for stability across hostname changes.
- `reclaim` and `purgeAllManaged` both use `effectivePrefix` instead of
  `cfg.VMNamePrefix`, so they only see and delete runners created by this host.
- `listing` (`utsusemi list`) still uses `cfg.VMNamePrefix` for full
  visibility across all hosts sharing that prefix.
- Two hosts with identical hostnames will still conflict; each must have a
  unique `LocalHostName` or a unique `vm_name_prefix` in config.
- Runners created by older versions (named `{VMNamePrefix}{hex}` without a
  host segment) are invisible to reclaim after upgrade and must be removed
  manually.

### Broker HTTP paths

Keep aligned across:

- `hostcredential.CredentialExchangePath`
- `internal/registrar/paths.go`
- `worker/src/routes.ts`

Worker:

- Stateless; no host Keychain or pool state.
- `withBrokerAuth` for JIT, list, delete.
- `POST /v1/credentials/exchange` uses the GitHub **user** access token, not the host JWT.

Deploy broker separately from CLI. After JWT signing or route changes, operators may need `utsusemi configure app`.

### Config

- Never put tokens, PATs, or bundles in `config.yaml`.
- Default broker: `config.DefaultHostedAppBrokerURL`; validate with `config.ValidateBrokerURL` before device flow.
- Default `reclaim_policy`: `grace` (`config.DefaultReclaimPolicy`).
- **`pool_size` upper bound is provider-specific**: `config.Validate(cfg, VMProvider)` reads `VMProvider.Capabilities().MaxConcurrent`. Configure uses `app.ValidateConfig` (capabilities only; does not require `tart` in PATH). Run/status use `app.Load`, which also checks provider availability.
- **Runtime assembly**: `internal/app` owns provider construction (`buildProvider`), config validation, registrar setup, and `Runtime` helpers used by CLI commands.
- **Bootstrap env**: `spawn.BootstrapEnv` sets `RUNNER_VERSION`, `RUNNER_ARCH`, and `RUNNER_HOME` for `bootstrap.sh`. `RUNNER_ARCH` comes from `VMProvider.Capabilities().RunnerArch` (Tart: `osx-arm64`).
- Operator docs in [README.md](README.md) Operations and Provider. Alerts/notifications are out of scope.

### Tests and toolchain

- **Go 1.23** (`go.mod`). Do not use `testing.T.Context()` (Go 1.24+).
- Tests: `provider.FakeExecutor`, `trackingRegistrar`, `keychain.MemoryStore`.

### Release and Homebrew

```bash
make test && make build && make worker-test
cd worker && npm install && npm run deploy
```

- Tag `v*` → `.github/workflows/release.yml`.
- Release binaries embed the tag version (`v0.1.0` → `0.1.0`) via `-ldflags -X github.com/novr/utsusemi/internal/version.Version=...`.
- Formula dispatch must pass `desc`, `test_match`, and `service_run_args: run` so the first release can create `utsusemi.rb` in `novr/homebrew-taps` (upsert when `desc` is set).
- `Formula/utsusemi.rb` `install` must include `generate_completions_from_executable(bin/"utsusemi", shell_parameter_format: :cobra)` (zsh completions on `brew install` / `brew reinstall`).
- `release-macos` uses workspace-local `GOMODCACHE` / `GOCACHE`.

### Removed / no migration

Legacy paths (`register`, bare JWT, `api_key`, `own_app`) are gone. Product is unreleased; breaking credential or storage changes are acceptable when noted here or in release notes.

**Release note (pending):** default `reclaim_policy` changed from `soft` to `grace`. Configs without an explicit policy now reclaim stale VMs after `reclaim_grace`. Set `reclaim_policy: soft` for prior behavior.

**Release note (pending):** runner names now include a host identifier
(`{vm_name_prefix}{host_id}-{random}`). Runners created before this change
(named `{vm_name_prefix}{random}`) are no longer managed by reclaim or `clean`
and should be removed manually via the GitHub UI before upgrading.
