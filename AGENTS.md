# AGENTS.md

Maintainer and agent reference. User-facing docs live in [README.md](README.md) only.

## Concerns

- **Ephemeral job VMs** — each `spawn.Run` clones a VM, runs one Actions job, then deletes the VM and GitHub runner. The pool only keeps up to `pool_size` such cycles in flight; it does not retain long-lived runner VMs.
- **Pool availability** — maintain warm capacity without leaking VMs, runners, disk, or leases.
- **Single agent process** — at most one `utsusemi run` / `brew services` instance per config (`StateDir/utsusemi.lock`).
- **Credential safety** — secrets in Keychain only; `config.yaml` is non-secret.
- **Hosted app renewability** — device flow once; OAuth refresh + broker exchange thereafter while the host runs.
- **Registration modes** — `hosted_app` (organization + broker) and `github_pat` (organization or repository, direct GitHub API). Keep code paths separate.
- **macOS / Tart** — Keychain, NAT lease limits, optional Softnet (`cirruslabs/cli/softnet` + root); Tart CLI stays behind `internal/provider`.

## Responsibilities

| Area | Package / path | Owns |
|------|----------------|------|
| CLI | `cmd/utsusemi` | Cobra commands, prompts, `loadConfigRuntime` / `loadValidatedRuntime` / `buildAgentFromRuntime`, configure flows |
| Agent | `internal/agent` | `utsusemi.lock` for `run`, signal drain, delegates to pool |
| Pool | `internal/pool` | Spawn loop, backoff, in-flight tracking, fill staggered until idle-ready, **reclaim**, **purge** (`clean`), **poolDrain** (`drain`) |
| Spawn | `internal/spawn` | One job lifecycle: clone → register → bootstrap → wait → teardown; `SlotControl` for poolDrain |
| Drain IPC | `internal/drainctl` | `{StateDir}/drain.request` / `drain.status` (CLI ↔ running agent) |
| Host ID | `internal/hostid` | `{StateDir}/host_id`; LocalHostName → hostname → random |
| VMs | `internal/provider` | Tart / executor abstraction |
| Runners | `internal/registrar` | `RunnerRegistrar` interface |
| PAT mode | `internal/registrar/pat.go` | GitHub REST with PAT from Keychain |
| Broker mode | `internal/registrar/broker.go` | Broker HTTP; `requestWithCredential` + `hostcredential.Manager` |
| Host credentials | `internal/hostcredential` | Bundle (hosted app), device flow, OAuth refresh, exchange, `EnsureFresh` |
| Local broker | `internal/brokerhttp` | Loopback HTTP (`127.0.0.1`); Worker-compatible routes |
| Targets | `internal/target` | Org/repo target types, parsing, `RequireOrg`, lowercase org |
| Config | `internal/config` | YAML model, defaults, `Validate`, `ValidateBrokerURL` |
| Leases | `internal/lease` | On-disk VM ↔ runner ↔ agent session |
| Status | `internal/status` | Read-only aggregation + text format for `utsusemi status` |
| Runtime config | `internal/runtimeconfig` | Agent-frozen knobs snapshot (`runtime_config.json`) for status |
| Listing | `internal/listing` | VM/runner rows for `utsusemi list` |
| Credential view | `internal/credentialview` | Keychain credential summary (no refresh) |
| Logging | `internal/logging` | slog + redaction; `run --log` fans out to stdout and a JSON log file |
| Runner releases | `internal/runnerrelease` | GitHub rejects stale runners; `latest` resolve + doctor warn when older |
| Runner cache | `internal/runnercache` | Host tarball cache under `StateDir/runner-cache`; Tart `:ro` mount injection |
| Action cache | `internal/actioncache` | Opt-in local Actions CacheService v2 (`action_cache`); guest `cache-serve` + node hook; data under `StateDir/action-cache` |
| Alerts | `internal/notify` | Webhook push (`agent_fatal` / `disk_blocked` / `pool_stuck` / `ready_failing`); Clock-based rules; Keychain or `UTSUSEMI_ALERT_WEBHOOK` |
| Keychain | `internal/keychain` | Platform secret store |
| Locks | `internal/instancelock` | `utsusemi.lock` (agent/clean), used with blocking flock for credential refresh |
| Broker (cloud) | `worker/` | Host JWT issue/verify, GitHub App calls, JIT/list/delete proxy |
| Broker (Oracle) | `deploy/oracle/` | Always Free A1 + Caddy; linux/arm64 `utsusemi broker` on loopback; secrets on disk |
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
- `drain` must **not** take `utsusemi.lock`; agent must be running. CLI writes `drain.request`; pool’s 1s control tick owns execution. Shutdown `p.drain` / `drainAndWait` (process exit + purge) is separate from operator `poolDrain`.
- `validate` and `run` can call `EnsureFresh` concurrently; credential refresh serializes on `credential.refresh.lock`, not `utsusemi.lock`.
- **Config freeze** — `agent.New` deep-copies `config.Config` for pool/spawn; respawns use that clone only (no mid-run YAML reload). On session start the freeze is written to `{StateDir}/runtime_config.json` for `utsusemi status`.
- **disk vs runtime** — `validate` / `configure` / `list` / `doctor` read `config.yaml` (next start). `status` shows `runtime` from the snapshot when the agent session matches. Apply YAML changes by restarting the agent.

### CLI (`cmd/utsusemi`)

- Wire dependencies only; no pool logic, Tart commands, or OAuth/device-flow implementation.
- `configure app` — device flow when credential missing or org/broker/oauth-client-id **value** changes; `--runner-group-id` **value** change → OAuth refresh / `EnsureFresh(force)` (same as `--refresh`), device flow only if that fails. Credential-only `--refresh` skips confirm/`writeConfig` because YAML is unchanged.
- `configure token` — token optional so target / auth-adjacent YAML merges do not require Keychain writes. Shared-knob-only updates prefer `configure edit`.
- **`configure edit`** — always `configPath` (`--config`). No shared flags → `$EDITOR` (new file seeds from embedded template). Any shared knob flag Changed → flag patch (no editor, no Keychain, mode-agnostic; existing config required). `--output` is not used (`run` would never load it). Save does not write `applyDefaults` back: validation uses an in-memory copy, and the file keeps keys that are already present even when they equal the current defaults. Empty fields follow that binary's `applyDefaults` on the next start. Flag patch replaces only the changed key's value in the stored file (other keys, comments, and blank lines stay). `--mounts=` deletes `mounts`. `--softnet=false` writes `false`.
- **`configure path|show`** — always `configPath` (`--config`). `show` prints the file bytes after a parse check and does not expand defaults.
- Cross-mode `configure app|token` rejected — Keychain shape and registrar differ; mode flip via `configure edit` (editor) only.
- **`broker`** — local GitHub App broker on `127.0.0.1`; does **not** take `utsusemi.lock` (run it beside `utsusemi run`).
- **`validate`** — disk `config.yaml` + credential API check (`loadValidatedRuntime`); does not inspect a running agent's freeze. Loopback `broker_url` must be reachable first.
- **`status`** — local ops summary via `internal/status.Collect` (`loadConfigRuntime`, no network), including `runtime` from `{StateDir}/runtime_config.json` when present. Display stays the effective `Load` values; it does not list which keys were filled by `applyDefaults`. `pool_drain` reflects `drain.status`; `draining` remains lease-less running VMs.
- **`drain`** — `loadConfigRuntime` only; writes `drain.request` with `request_id` + `agent_id`, polls `drain.status`. Does not take `utsusemi.lock`.
- **`pull image <image>`** — `loadConfigRuntime` (no credential check). Does not write config. Does not hold `utsusemi.lock` across `Provider.SyncImage`. A per-ref image lock serializes that sync with startup `SyncImage` and `Clone` of the same name. When the agent is running, pulls only if the live `base_image` is a different non-empty string; the same name, or an unconfirmed snapshot, is an error. Startup `SyncImage` remains.
- **`pull runner <latest|version>`** — `loadConfigRuntime`. `latest` resolves through GitHub and is printed; it does not pin `runner_version`. Adds a missing tarball and does not replace or prune existing ones. Does not take `utsusemi.lock`. Startup `Ensure` still prunes after it downloads a missing frozen version.
- **`list [vms|runners]`** — VM and/or runner rows via `internal/listing.Collect` (`loadValidatedRuntime`, network).
- **`run --log`** — `ResolveAgentLog` picks `{state_dir}/agent.log` when the flag has no path; file sink is JSON regardless of TTY. Homebrew `service_run_args` is `run,--log` (comma-separated for the taps dispatch) so `brew services` uses the same file.

### Shell completion

- Cobra `completion` generates zsh/bash/fish scripts. The Homebrew formula installs them via `generate_completions_from_executable(..., shell_parameter_format: :cobra)`.
- When adding, removing, or renaming commands, subcommands, or positional args, update `cmd/utsusemi/completion.go` (`registerListCompletions`, etc.) and `completion_test.go`. Do not document end-user setup in README; leave that to the formula.

### Credentials

| Mode | Keychain contents |
|------|-------------------|
| `hosted_app` | JSON bundle (`hostcredential.Bundle` v1): host JWT, refresh token, GitHub user |
| `github_pat` | Raw PAT string |
| Local broker | JSON (`utsusemi-broker`): GitHub App ID, App PEM, Ed25519 host-JWT signing key |
| Alerts webhook | URL string (`utsusemi-alerts` / `webhook`); optional override `UTSUSEMI_ALERT_WEBHOOK` |

Hosted app rules:

- `Load` accepts bundle JSON only; legacy bare JWT is rejected.
- **`hostcredential.Manager`** owns refresh → exchange → Keychain update. `EnsureFresh` also re-exchanges when the host JWT `target` (org / `runner_group_id`) does not match the requested target — broker target mismatch is **403**, which is not retried by `requestWithCredential` (only **401** is).
- After OAuth refresh, persist bundle with the **new refresh token** before exchange (refresh tokens are single-use). The partial write may still carry the previous host JWT until exchange succeeds.
- **`BrokerRegistrar`** uses `Manager.EnsureFresh` via `requestWithCredential`; do not duplicate store/oauth/lock logic there.
- Broker **401** → `EnsureFresh(..., force=true)` and one retry; other HTTP errors do not force refresh.

### Registration modes

- `hosted_app` requires an **organization** target (`config.Validate`).
- Optional `registration.oauth_client_id` for a private App; empty uses `hostcredential.PublicAppClientID`.
- `github_pat` supports organization or repository targets.
- New backends: implement `RunnerRegistrar`; do not branch on `registration.mode` in `pool` or `spawn`.

### Pool: reclaim vs purge vs poolDrain

| | reclaim | purge (`utsusemi clean`) | poolDrain (`utsusemi drain`) |
|--|---------|--------------------------|------------------------------|
| When | Startup + periodic during `run` | Manual; agent must be stopped | Manual; agent must be running |
| Scope | Stale/orphan per `reclaim_policy` | All prefix-matched VMs and runners | Current in-flight; then refill `pool_size` |
| In-flight VMs | Skipped | Not skipped | Abandon unclaimed; wait/force claimed |
| Leases | `RemoveLease` per VM | `ClearLeases` | Per-spawn teardown (no full purge) |
| Agent process | Continues | Must be stopped | Continues |

Shared teardown: `stopAndDeleteManagedVM` (`managed_vm.go`). Shutdown path still uses `drainAndWait` → `purgeAllManaged`; that is not `poolDrain`.

### Multi-host safety

Each `Pool` carries an `effectivePrefix = cfg.VMNamePrefix + hostID + "-"`.

- **hostID** is loaded from `{StateDir}/host_id`. On first run, `internal/hostid`
  derives it from `scutil --get LocalHostName`, else `os.Hostname()`, else random
  hex (sanitized: lowercase, non-alphanumeric → dash, max 24 chars) and writes
  that file for stability across hostname changes.
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
- `internal/brokerhttp`
- `worker/src/routes.ts`

Worker, `utsusemi broker` (macOS Keychain or Linux env/files), and `deploy/oracle` are separate deployments. Do not generate a new host JWT signing key for the hosted Oracle broker; reuse the Workers key. Linux broker secrets use `GITHUB_APP_ID`, `UTSUSEMI_*_KEY_FILE` or inline `GITHUB_APP_PRIVATE_KEY` / `CREDENTIAL_SIGNING_PRIVATE_KEY` (see `internal/brokerhttp/secrets.go`).

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
- **Bootstrap / guest env**: `spawn.GuestEnv` merges literal `config.Env` into `ExecStdin`, then `BootstrapEnv` (`RUNNER_VERSION`, `RUNNER_ARCH`, `RUNNER_HOME`, `RUNNER_CACHE_DIR`) wins. `Validate` requires POSIX-ish keys (`[A-Za-z_][A-Za-z0-9_]*`), rejects `RUNNER_` and NUL/newline in values (leading `-` would confuse guest `env(1)` getopt). Values are not expanded. Utsusemi only puts them on the guest process; the Actions runner then inherits them into host job step process env (not the workflow `env` context, not arbitrary job-container env, not `runtime_config` / `status`). `RUNNER_ARCH` comes from `VMProvider.Capabilities().RunnerArch` (Tart: `osx-arm64`). Host cache via `runnercache.Ensure` on agent start; mount name `utsusemi-runner-cache`. Optional `action_cache` stages `utsusemi`+`hook.js`, mounts `utsusemi-action-cache` (rw → `{StateDir}/action-cache`) and `utsusemi-action-cache-bin` (`:ro` → `{StateDir}/action-cache-bin`), and bootstrap starts guest `cache-serve` on `127.0.0.1:19527` then wraps `externals/node*/bin/node` before `run.sh`. `UTSUSEMI_ACTION_CACHE_{BIN_DIR,DATA_DIR,ADDR}` are unset before `run.sh`; `UTSUSEMI_ACTION_CACHE=1` stays for the job process env (not the workflow `env` context). Cache routing is the node hook, not that flag. Repo namespace from the Actions Bearer is collision avoidance only (no signature verify); all jobs on the host share one trust boundary. `Validate` rejects `UTSUSEMI_ACTION_CACHE*` in `config.env`. Optional `system_proxy` is guest macOS system HTTP(S) proxy only: `Validate` rejects `UTSUSEMI_SYSTEM_PROXY_` in `config.env`; bootstrap passes host and port as those variables; `bootstrap.sh` applies them with `sudo -n networksetup` to the default-route service, then unsets them before `run.sh`. `route` falls back to `/sbin/route` when it is not on PATH. A `networksetup` or `sudo -n` failure fails the spawn. Hosts reject whitespace, controls, `,` `/`, and a leading `-`. Bypass `Empty` is rejected (`networksetup` clears every exclusion). `system_proxy` does not derive `http_proxy`.
- Operator docs in [README.md](README.md) Operations and Provider.
- **Alerts (in scope)**: webhook push via `internal/notify` for `agent_fatal`, `disk_blocked`, `pool_stuck`, and `ready_failing` (`utsusemi configure alerts`; URL in Keychain or `UTSUSEMI_ALERT_WEBHOOK`). `pool_stuck` uses time since last warm capacity; `Clear` on `resetSpawnBackoff` (ClaimedJob, or JobMs ≥ 60s). Fires when empty or saturated (`Active >= PoolSize` with `ConsecutiveReadyFails > 0`; partial fill / healthy full pool do not). In-flight flickers do not reset the warm clock. `ready_failing` fires after 3 consecutive `waitUntilReady` failures (`spawn.ErrWaitReady`); cleared when a spawn reaches ready. Out of scope: process-death detection, fleet dashboards, Slack SDKs, YAML webhook URLs, threshold knobs in config, single spawn failures.
- **`utsusemi doctor`**: preflight via `internal/doctor` (check names and statuses are defined there; alerts unset is `ok` / “not configured”). `runner_version` older than GitHub `latest` is **warn** (stale → JIT failure); skip that comparison on network error. Empty `runner_version` remains **fail**. Both point at `utsusemi configure edit --runner-version latest`. Host runner-cache miss is **warn** only. Exit 1 when any check is `fail`.
- **Spawn metrics**: `spawn.SaveLastSpawn` writes `{StateDir}/last_spawn.json`; `status` and spawn logs expose cold-start phase timings.

### Tests and toolchain

- **Go 1.23** (`go.mod`). Do not use `testing.T.Context()` (Go 1.24+).
- Tests: `provider.FakeExecutor`, `trackingRegistrar`, `keychain.MemoryStore`.
- **Action cache**: `internal/actioncache` integration tests gate `make test`. Real Tart / GitHub E2E is manual only: `.github/workflows/e2e.yaml` + agent [`e2e/config.yaml`](e2e/config.yaml) (`workflow_dispatch`, label `utsusemi-e2e`; see README Local Actions cache). Not a CI gate.

### Release and Homebrew

```bash
make test && make build && make worker-test
cd worker && npm install && npm run deploy
```

- Tag `v*` → `.github/workflows/release.yml`.
- Release binaries embed the tag version (`v0.1.0` → `0.1.0`) via `-ldflags -X github.com/novr/utsusemi/internal/version.Version=...`.
- Formula dispatch must pass `desc`, `test_match`, and `service_run_args: run,--log` (comma-separated args for `novr/homebrew-taps`) so the first release can create `utsusemi.rb` (upsert when `desc` is set).
- `Formula/utsusemi.rb` `install` must include `generate_completions_from_executable(bin/"utsusemi", shell_parameter_format: :cobra)` (zsh completions on `brew install` / `brew reinstall`).
- `release-macos` uses workspace-local `GOMODCACHE` / `GOCACHE`.

### Removed / no migration

Legacy paths (`register`, bare JWT, `api_key`, `own_app`) are gone.
