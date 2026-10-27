# Utsusemi

## Quick start

Install [Tart](#requirements) first, then:

```bash
brew tap novr/taps
brew trust novr/taps
brew install utsusemi
utsusemi configure app --org my-org
utsusemi validate
utsusemi run
# brew services start utsusemi   # background
```

## Requirements

- Apple Silicon Mac, macOS 15+
- [Tart](https://tart.run/) 2.34+ — `brew tap openai/tools && brew trust openai/tools && brew install openai/tools/tart`

## Paths

| Path | Default |
|------|---------|
| Config | `~/.config/utsusemi/config.yaml` |
| State | `~/.local/state/utsusemi` |
| Agent log | `{state_dir}/agent.log` (`utsusemi run --log` / `brew services`; JSON lines) |
| Credentials | Keychain (same macOS user as setup and service) |

Use `--config` or `UTSUSEMI_CONFIG` when multiple agents on one Mac must not share state (see [Personal account](#personal-account-no-org)).

## Configuration

### Setup commands

```text
utsusemi configure app [flags]
utsusemi configure token [flags]
utsusemi configure alerts [flags]
utsusemi configure edit|path|show
```

| | `configure app` | `configure token` |
|--|-----------------|-------------------|
| Auth | GitHub device flow when needed | PAT on stdin or `--token` when provided |
| Targets | organization | organization or repository |
| Credential refresh | automatic | manual |

Shared knobs (`--base-image`, `--pool-size`, `--labels`, `--runner-version`, `--mounts`, `--softnet`, `--reclaim-policy`, `--reclaim-grace`, `--min-free-disk-gb`, `--force`) are available on `configure app`, `configure token`, and `configure edit`. For knob-only updates prefer **`utsusemi configure edit --…`** (mode-agnostic; no Keychain). `--output` is app/token bootstrap only; `edit` / `show` / `path` always use `--config`.

`--runner-group-id` (default `1`) for organization and repository targets.

Re-running `configure` merges **only changed flags** so hand-edited keys (`state_dir`, `vm_name_prefix`, timeouts, …) survive. Existing config: prompt on TTY; `--force` for non-interactive.

| Case | Rationale |
|------|-----------|
| `configure edit --pool-size` / `--runner-version latest` | Update shared knobs without picking app vs token; no editor when flags are set |
| `configure edit` (no shared flags) | Open `$EDITOR` on `config.yaml` |
| `configure token` without a token | Change org/repo/runner-group (auth-adjacent YAML) without rotating Keychain |
| `configure app`, same `--org` / `--broker` | Skip device flow when the stored credential still matches |
| `configure app --runner-group-id` (value change) | Host JWT embeds the group — re-exchange via OAuth refresh (device flow if refresh fails) |
| `configure app --refresh` | Renew OAuth while stopped; device flow only if the refresh token is dead |
| Credential-only `--refresh` | OAuth does not change YAML — no overwrite prompt, no `config.yaml` rewrite |
| `edit` / `show` / `path` use `--config` only | That path is what `run` loads; `--output` is for writing elsewhere during setup |
| `registration.mode` change | Different Keychain layout and registrar — use `configure edit` (editor) deliberately |

If `configure edit` (editor) changes `org` / `runner_group_id`, the next `validate` / `run` / `list` re-exchanges the host JWT when the embedded target no longer matches. Use `configure app --refresh` to update the credential immediately.

`--runner-version` defaults to `latest` on app/token configure: resolves the current [actions/runner](https://github.com/actions/runner/releases) release and pins a concrete version into `config.yaml` (config itself must not contain `latest`). Offline or API failures: pass an explicit `--runner-version x.y.z`. Re-pin later with `utsusemi configure edit --runner-version latest`.

Examples: [examples/config.template.yaml](examples/config.template.yaml), [examples/config.pat.yaml](examples/config.pat.yaml), [examples/config.local-broker.yaml](examples/config.local-broker.yaml).

### Choosing authentication

| Target | GitHub IP allowlist | Use |
|--------|---------------------|-----|
| Repository | Does not matter | PAT (`configure token --repo`). A GitHub App is not required. |
| Organization | None | Hosted GitHub App (`configure app`) |
| Organization | Enabled | PAT first (`configure token --org`). App mode needs a broker whose egress IP GitHub allows: the hosted broker (`configure app`, reserved IPv4 on the UtsusemiApp IP list), a loopback broker on this Mac, or your own deployment (`deploy/oracle/`). |

Org JIT still needs an org admin to install a GitHub App. Repo Admin on a member org is enough for repo PAT runners, not org runners.

### GitHub App

Organization runners only. Install the [Utsusemi GitHub App](https://github.com/apps/utsusemiapp). Enable **User-to-server token expiration** (Opt-in).

```bash
utsusemi configure app --org my-org
```

Re-run when:

- authorized user leaves the org or revokes the App
- host offline >6 months
- automatic refresh fails (`utsusemi validate`, logs)

```bash
brew services stop utsusemi   # before re-configuring
```

While the agent is stopped: `utsusemi configure app --refresh` — same OAuth path as automatic refresh, without re-entering org/broker flags.

#### Credential lifecycle

| Item | Lifetime | Renews |
|------|----------|--------|
| Host credential | 30 days | ≤7 days left, `validate`/`run` startup, or auth error |
| GitHub refresh token | 6 months idle | Each automatic refresh (~23 days while running) |
| GitHub user access token | 8 hours | Not stored |

#### Broker

`hosted_app` mode routes host credential issuance and refresh through a broker service (`broker_url`). Default hosted URL is `https://utsusemi.duckdns.org` (see [`deploy/oracle/`](deploy/oracle/)).

**Availability.** A running agent holds a 30-day host credential locally; the broker is only contacted during `configure app` and at refresh time (≤7 days remaining). If the broker is unreachable at refresh, the existing credential continues to work until it expires. A monitoring system that watches `utsusemi validate` exit code will surface the failure before the credential window closes.

**Local broker.** For an allowlisted org, or to stop depending on the hosted broker, run a loopback broker on this Mac. It talks to `api.github.com` from this host (or the VPN/egress IP GitHub already allows). Bind is IPv4 `127.0.0.1` only.

1. Create a GitHub App (org owner). Enable GitHub Actions, including org self-hosted runners, and install it on the org. Copy the App ID and download the private key PEM.
2. Start the broker (leave this process running; it does **not** replace `utsusemi run`):

```bash
utsusemi broker --app-id 123456 --app-private-key-file ./app.pem
```

App ID, PEM, and the host-JWT signing key are stored in Keychain (`utsusemi-broker`). They are not written to `config.yaml`. Later starts can omit the flags.

3. Point `hosted_app` at loopback and (for your App) pass the App’s OAuth client ID:

```bash
utsusemi configure app --org my-org --broker http://127.0.0.1:8787 --oauth-client-id Iv23…
```

`validate` / `run` / `doctor` fail if `broker_url` is loopback and nothing is listening. Example: [examples/config.local-broker.yaml](examples/config.local-broker.yaml).

**Data.** During the exchange the broker receives a short-lived GitHub user access token (8 h lifetime) and the target identifier (org or repo name). It uses these to mint a scoped host credential and does not retain them. The refresh token is stored only in the local keychain and is never sent to the broker.

### Fine-grained PAT

Repository (`Administration: Read and write`):

```bash
printf '%s' "$TOKEN" | utsusemi configure token --repo owner/repo
```

Organization (`Self-hosted runners: Read and write`):

```bash
printf '%s' "$TOKEN" | utsusemi configure token --org my-org
```

### Personal account (no org)

PAT mode supports `--repo`, so Utsusemi can run against personal repositories. One agent serves one repo, so each additional repo needs its own isolated instance:

| Isolate per instance | Why |
|----------------------|-----|
| `--config` / `UTSUSEMI_CONFIG` | Separate config file |
| `state_dir` | Lock file and lease state live here; collision prevents startup |
| `vm_name_prefix` | Prevents VM name collisions across instances |
| `credential_keychain_service` | Prevents keychain entry collisions |

`brew services` manages one service definition; a second instance needs a hand-written launchd plist. With Tart capped at 2 concurrent VMs, at most two single-slot pools can run on one host. Credential refresh is manual (GitHub App mode is organization-only).

**Recommended path for multiple repos: create a free GitHub organization.**

Free orgs include the default runner group (matches `--runner-group-id 1`), enable GitHub App mode (single agent for all repos, automatic credential refresh), and existing repo URLs redirect after transfer. Cost: one org creation, one transfer per repo.

### Runtime options

Edit `config.yaml` after `configure` with `utsusemi configure edit` (shared flags patch without opening an editor, or `$EDITOR` with no flags). `configure app` / `token` still merge changed flags on re-run.

| Key | Default | Notes |
|-----|---------|-------|
| `pool_size` | `1` | Upper bound is provider-specific (`utsusemi status` shows `max`; Tart is 2). Status `draining` is lease-less running VMs — not counted toward `pool_size`. Operator recycle is `pool_drain` (`utsusemi drain`). Cold starts are staggered (at most one overlapping clone/boot) so Tart boots do not pile up. |
| `pool_check_interval` | `30s` | How often the pool looks for a free slot. Lengthening this alone does not prevent mid-bootstrap overlap; stagger (above) does. |
| `reclaim_policy` | `grace` | `soft` — do not reclaim running VMs for staleness (restart-friendly; at capacity may still reclaim); `grace` — reclaim stale after `reclaim_grace`; `hard` — reclaim stale immediately |
| `reclaim_grace` | `15m` | When `reclaim_policy` is `grace` |
| `reconciliation_interval` | `5m` | Reclaim interval during `run` |

## Provider

### Tart

`provider: tart`

- **base_image** / `--base-image` — pull on agent start ([Tart](https://tart.run/))
- **min_free_disk_gb** — default `50`
- **Keychain** — unlock: `security unlock-keychain login.keychain`; headless: `security set-keychain-settings -t 0 ~/Library/Keychains/login.keychain-db`
- **Networking** — [Tart FAQ](https://tart.run/faq/); optional `softnet: true` for NAT / lease isolation — `brew install cirruslabs/cli/softnet` and grant root (SUID or passwordless sudo). Softnet does **not** stabilize Tart guest-agent GRPC / `tart exec`.
- **Directory shares** — `mounts` passes host directories into the VM as Tart `--dir` flags (see below)

#### Directory shares (host → VM)

`mounts` is a list of host paths passed to Tart's `--dir` flag. `~/` and Tart's `name:~/…` form expand to the home directory of the user running `utsusemi` (the same user as `brew services`). Each path must already exist as a directory — missing mounts make Tart refuse to start the VM. Use `utsusemi status` to see resolved paths and `utsusemi doctor` to catch missing directories before a job starts. Create the directories first, then:

```bash
utsusemi configure edit --mounts ~/utsusemi-cache/swiftpm --mounts ~/utsusemi-toolchains:ro
brew services restart utsusemi   # mounts/softnet apply after agent restart
```

`--mounts=` clears the list. Repeat `--mounts` to replace the whole list (merge is not append).

```yaml
mounts:
  - ~/utsusemi-cache/swiftpm        # read-write (default)
  - ~/utsusemi-toolchains:ro        # read-only
```

> **Warning:** A shared directory persists across jobs and survives VM recycling, which partially gives up the "every job starts from a pristine VM" guarantee. Use `:ro` for anything the job should only read (toolchains, SDKs). A poisoned cache in a writable share affects every future job until it is manually cleaned.
>
> The workspace path inside every VM is identical (`/Users/admin/actions-runner/_work/<repo>/<repo>`), so even build products with absolute paths baked in restore correctly from a host-side cache.

#### Pre-installed runner (low-latency)

On agent start, Utsusemi caches `runner_version` under `{state_dir}/runner-cache/` and mounts it read-only (`utsusemi-runner-cache`) so each spawn does not download from GitHub. A matching runner already in the image skips install. Restart the agent after changing `runner_version` so the cache refreshes.

Keep `runner_version` at GitHub’s current [Actions runner](https://github.com/actions/runner/releases) release (re-pin with `utsusemi configure edit --runner-version latest`). A stale runner can register then exit without claiming a job (`utsusemi doctor` **warns** when older than latest). Repeated `runner finished quickly without claiming a job` means that unclaimed-exit window — short successful jobs are not treated as this failure. `base_image:…:latest` refreshes the Tart image on agent start only; bump `runner_version` separately (host cache installs it even when the image ships an older runner).

> **Note:** Optional `env:` in `config.yaml` sets literal variables on the guest bootstrap / `run.sh` process (e.g. `http_proxy`). Keys must be `[A-Za-z_][A-Za-z0-9_]*`. No `$VAR` expansion; `RUNNER_*` keys are reserved. Do not put secrets in `config.yaml` — values appear on the guest process argv and are visible to every job on that agent.
>
> The Actions runner inherits that process environment into **host** job step processes (`printenv` / `$http_proxy`). It does **not** add those keys to the workflow `env` context (`${{ env.NAME }}`), and job containers do not receive arbitrary host env automatically.
>
> Keep app/SDK paths and repo-portable settings in workflow `env:` (even if a process `printenv` would also show a `config.env` value):
> ```yaml
> jobs:
>   build:
>     env:
>       ANDROID_HOME: /opt/homebrew/share/android-commandlinetools
> ```
>
> `{runner}/.env` is also loaded when `Runner.Listener` starts (including `run.sh --jitconfig`). Prefer `config.env` so host-wide settings are reapplied every spawn from the agent config instead of baking them into the VM image.
>
> `env` is process environment only. URLSession / CFNetwork (SwiftPM `binaryTarget`) does not read `http_proxy`. Optional `system_proxy` sets the guest macOS system HTTP(S) proxy for those clients and does not change `config.env`. curl, git, and the runner tarball download still use `env`, or the host runner cache when the tarball is already there.
>
> Values are `host:port`. At least one of `http` or `https` is required. Quote an IPv6 endpoint (`"[2001:db8::1]:443"`); unquoted `[` is a YAML sequence. `bypass` entries are hostnames or IPs, not CIDR; quote `"*.local"` and `"<local>"`. Omitting `bypass` leaves the image's existing exclusions. The guest user needs passwordless `sudo`. With `softnet: true` the guest cannot reach the host, so a proxy running there is unreachable. Restart the agent after changing this. It does not fix a missing host route, DNS, or a proxy that hangs after CONNECT.
>
> ```yaml
> system_proxy:
>   http: proxy.example:8080
>   https: proxy.example:8080
>   bypass:
>     - localhost
>     - 127.0.0.1
>     - "<local>"
>     - "*.local"
> ```

## Multi-host setup

Multiple Macs can target the same org or repo. Reclaim and `clean` only touch runners named `{vm_name_prefix}{host_id}-…` for this host, so hosts do not delete each other's runners.

`host_id` is stored in `{state_dir}/host_id` on first start (`scutil --get LocalHostName`, else hostname, else random).

- Give each host a unique `LocalHostName`, or a unique `vm_name_prefix` in config.

## Operations

```bash
utsusemi --version
utsusemi validate
utsusemi doctor   # preflight (fail → exit 1)
utsusemi status   # live agent / runtime vs next-start config
utsusemi list
utsusemi run
utsusemi run --log              # also append to {state_dir}/agent.log
utsusemi run --log=/path/to.log
utsusemi drain                  # recycle pool; agent stays up
utsusemi drain --timeout=30m
utsusemi drain --force
brew services start utsusemi
```

`validate` / `configure edit` change `config.yaml` for the **next** agent start; `status` shows the running agent’s start-time settings under `runtime:`. Restart (or `brew services restart utsusemi`) to apply YAML changes.

### Alerts

Optional webhook for sustained outages while `utsusemi run` is up (URL in Keychain, or `UTSUSEMI_ALERT_WEBHOOK`):

```bash
utsusemi configure alerts --webhook-url https://hooks.example/xxx
utsusemi configure alerts --clear
```

| Code | When |
|------|------|
| `agent_fatal` | Agent stops for a non-cancel reason (auth failure, repeated unclaimed exits, base-image sync failure, …) |
| `disk_blocked` | Low disk has paused spawn for 15 minutes |
| `pool_stuck` | No warm capacity for 15 minutes while the pool is empty **or full with ongoing ready-check failures** (partial fill / healthy full pool do not alert; cleared when a job is claimed or runs long enough) |
| `ready_failing` | Three consecutive VM ready-check failures (`waitUntilReady`); cleared when a spawn reaches ready |

Single spawn failures and normal backoff do not notify (except the sustained cases above). Process crashes still need launchd / `brew services` monitoring. Broker credential expiry before refresh remains best watched via `utsusemi validate` exit code (see Broker Availability).

Payload is JSON for generic receivers and Slack Incoming Webhooks (`text` is what Slack shows):

```json
{
  "text": "[pool_stuck] host=abc target=org:acme utsusemi pool: no warm capacity (empty or saturated): clone failed",
  "code": "pool_stuck",
  "host_id": "abc",
  "target": "org:acme",
  "at": "2026-01-02T03:04:05Z",
  "detail": "clone failed"
}
```

Reclaim (automatic during `run`, per Runtime options) vs `drain` vs `clean`:

| | `drain` | `clean` |
|--|---------|---------|
| Agent | must be **running** | must be **stopped** |
| Effect | stop new jobs, wait/force in-flight, then refill `pool_size` | delete all managed VMs/runners |
| Use when | stuck / desynced occupancy without restart | last-resort wipe |

```bash
utsusemi drain
utsusemi drain --timeout=30m
utsusemi drain --force          # reclaim in-flight immediately (may fail a claimed Actions job)
utsusemi clean
utsusemi clean --dry-run
```

`drain` tears down idle (unclaimed) runners immediately so they cannot pick up new jobs; claimed jobs finish unless `--timeout` elapses or `--force` is set. A short race remains between GitHub assignment and the runner log markers Utsusemi uses to detect a claim. Status field `pool_drain` shows an in-progress recycle; `draining` still means lease-less running VMs (not counted toward `pool_size`).

### Service logs

`utsusemi run --log` mirrors structured logs to `{state_dir}/agent.log` (or `--log=/path`) so foreground runs leave a trail without relying on the terminal buffer. Tart subprocess lines are captured in the file even on a TTY (console format may differ from the `  | ` prefix).

Homebrew's launchd formula runs `utsusemi run --log`, so brew services also append to `{state_dir}/agent.log`. Launchd still captures stdout/stderr separately:

- `$(brew --prefix)/var/log/utsusemi.log`
- `$(brew --prefix)/var/log/utsusemi.error.log`

`newsyslog` below applies to those brew launchd logs only, not `{state_dir}/agent.log`.

```bash
BREW_PREFIX="$(brew --prefix)"
curl -fsSL https://raw.githubusercontent.com/novr/Utsusemi/main/examples/utsusemi.newsyslog.conf \
  | sed "s|@HOMEBREW_PREFIX@|${BREW_PREFIX}|g" \
  | sudo tee /etc/newsyslog.d/utsusemi.conf
sudo newsyslog -nv
```

## License

MIT
