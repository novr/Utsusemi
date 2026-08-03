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
- [Tart](https://tart.run/) 2.34+ — install via `brew tap openai/tools && brew trust openai/tools && brew install openai/tools/tart`. The minimum matches the current `openai/tools` distribution (the `tart exec -i` fix shipped in 2.33.0, but the stale `cirruslabs/cli` tap stops at 2.32.1). Upgrading from `cirruslabs/cli` may require `brew uninstall softnet` first if softnet was installed from that tap.

## Paths

| Path | Default |
|------|---------|
| Config | `~/.config/utsusemi/config.yaml` |
| State | `~/.local/state/utsusemi` |
| Credentials | Keychain (same macOS user as setup and service) |

## Configuration

### Setup commands

```text
utsusemi configure app [flags]
utsusemi configure token [flags]
```

| | `configure app` | `configure token` |
|--|-----------------|-------------------|
| Auth | GitHub device flow | fine-grained PAT on stdin or `--token` |
| Targets | organization | organization or repository |
| Credential refresh | automatic | manual |

Shared flags: `--base-image`, `--pool-size`, `--labels`, `--runner-version`, `--output`, `--force`.

Organization: `--runner-group-id` (default `1`).

Existing config: prompt on TTY; `--force` for non-interactive.

Example: [examples/config.pat.yaml](examples/config.pat.yaml).

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

#### Credential lifecycle

| Item | Lifetime | Renews |
|------|----------|--------|
| Host credential | 30 days | ≤7 days left, `validate`/`run` startup, or auth error |
| GitHub refresh token | 6 months idle | Each automatic refresh (~23 days while running) |
| GitHub user access token | 8 hours | Not stored |

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

Edit `config.yaml` after `configure` (not written by `configure`).

| Key | Default | Notes |
|-----|---------|-------|
| `pool_size` | `1` | Upper bound is provider-specific (`utsusemi status` shows `max`; Tart is 2) |
| `reclaim_policy` | `grace` | `soft` — local dev; `hard` — immediate |
| `reclaim_grace` | `15m` | When `reclaim_policy` is `grace` |
| `reconciliation_interval` | `5m` | Reclaim interval during `run` |

## Provider

### Tart

`provider: tart`

- **base_image** / `--base-image` — pull on agent start ([Tart](https://tart.run/))
- **min_free_disk_gb** — default `50`
- **Keychain** — unlock: `security unlock-keychain login.keychain`; headless: `security set-keychain-settings -t 0 ~/Library/Keychains/login.keychain-db`
- **Networking** — [Tart FAQ](https://tart.run/faq/); `softnet: true` → [Softnet](https://github.com/cirruslabs/softnet)
- **Directory shares** — `mounts` passes host directories into the VM as Tart `--dir` flags (see below)

#### Directory shares (host → VM)

`mounts` is a list of host paths passed to Tart's `--dir` flag. Paths starting with `~/` (or `name:~/…` in Tart's tagged form) are expanded to the home directory of the user running `utsusemi` (the same user as `brew services`). Empty entries are ignored. `utsusemi status` shows the resolved paths; `utsusemi doctor` warns when a configured path is missing on disk.

```yaml
mounts:
  - ~/utsusemi-cache/swiftpm        # read-write (default)
  - ~/utsusemi-toolchains:ro        # read-only
```

> **Warning:** A shared directory persists across jobs and survives VM recycling, which partially gives up the "every job starts from a pristine VM" guarantee. Use `:ro` for anything the job should only read (toolchains, SDKs). A poisoned cache in a writable share affects every future job until it is manually cleaned.
>
> The workspace path inside every VM is identical (`/Users/admin/actions-runner/_work/<repo>/<repo>`), so even build products with absolute paths baked in restore correctly from a host-side cache.

#### Pre-installed runner (low-latency base image)

By default bootstrap downloads the Actions runner tarball on every job start (~30–60 s).
To eliminate that latency, pre-install the runner in the base image.

**Stock cirruslabs images** (`ghcr.io/cirruslabs/macos-*-xcode`) already ship the runner at `/Users/admin/actions-runner`. Bootstrap detects the installed version by running `Runner.Listener --version` and skips the download automatically — no custom bake step required. Just set `runner_version` in `config.yaml` to match what the image ships.

**Custom images** — if `./bin/Runner.Listener` is missing under `RUNNER_HOME` (default `/Users/admin/actions-runner`), bootstrap falls back to a `.runner-version` sentinel file:

```bash
# Run once inside the base image VM, then snapshot / push the image.
RUNNER_VERSION="2.336.0"   # must match runner_version in config.yaml
RUNNER_HOME="/Users/admin/actions-runner"   # must match bootstrap.sh default
mkdir -p "$RUNNER_HOME" && cd "$RUNNER_HOME"
curl -fsSL -o actions-runner.tar.gz \
  "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-osx-arm64-${RUNNER_VERSION}.tar.gz"
tar xzf actions-runner.tar.gz && rm actions-runner.tar.gz
echo "${RUNNER_VERSION}" > .runner-version
sync   # flush writes before tart stop; skipping this silently loses recent changes
```

Keep `runner_version` in `config.yaml` in sync with the pre-installed version. When you upgrade the runner, rebuild the base image and update `config.yaml` together.

> **Warning:** do not assume the runner version bundled in your base image is acceptable to GitHub. GitHub periodically deprecates old runner versions; a JIT runner that is too old will register and then exit immediately (after ~28 s) without ever claiming a job, causing the pool to respawn with growing backoffs until the agent stops. Always install a current runner from [github.com/actions/runner/releases](https://github.com/actions/runner/releases) and set `runner_version` in `config.yaml` to match. If you see repeated `runner finished quickly without claiming a job` warnings in the logs, this is the likely cause.

> **Note:** The runner's `.env` file is consumed by the service wrapper (`svc.sh`), not by `run.sh --jitconfig`. Utsusemi invokes `run.sh` directly, so `.env` is never read. Expose environment variables to jobs at the workflow level instead:
> ```yaml
> jobs:
>   build:
>     env:
>       ANDROID_HOME: /opt/homebrew/share/android-commandlinetools
> ```

## Multi-host setup

You can run Utsusemi on multiple Mac hosts pointing at the same GitHub org or
repository. Each host automatically scopes its reclaim to only the runners it
created, so Host A will never delete Host B's runners.

**How it works** — on first start, Utsusemi derives a *host identifier* from
`os.Hostname()` and stores it in `{StateDir}/host_id`. All runner VMs created
by that host are named `{vm_name_prefix}{host_id}-{random}`. Reclaim and
`clean` only touch names that start with that combined prefix.

**Requirements for safe multi-host operation:**

- Each host must have a unique hostname (`scutil --get LocalHostName`). Two
  hosts with the same hostname will still interfere.
- Alternatively, set a unique `vm_name_prefix` per host in `config.yaml`
  (e.g. `vm_name_prefix: utsusemi-mac-a-`); the host identifier is appended
  automatically so names remain distinct.

## Operations

```bash
utsusemi --version
utsusemi validate
utsusemi doctor
utsusemi status
utsusemi list
utsusemi run
brew services start utsusemi
```

Reclaim (automatic during `run`, per Runtime options) vs `clean` (manual purge; stop agent first):

```bash
utsusemi clean
utsusemi clean --dry-run
```

### Service logs

- `$(brew --prefix)/var/log/utsusemi.log`
- `$(brew --prefix)/var/log/utsusemi.error.log`

```bash
BREW_PREFIX="$(brew --prefix)"
curl -fsSL https://raw.githubusercontent.com/novr/Utsusemi/main/examples/utsusemi.newsyslog.conf \
  | sed "s|@HOMEBREW_PREFIX@|${BREW_PREFIX}|g" \
  | sudo tee /etc/newsyslog.d/utsusemi.conf
sudo newsyslog -nv
```

## License

MIT
