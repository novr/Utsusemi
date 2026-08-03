# Utsusemi

## Quick start

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
- [Tart](https://tart.run/) 2.34+ — install via `brew tap openai/tools && brew trust openai/tools && brew install openai/tools/tart`

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

#### Pre-installed runner (low-latency base image)

By default bootstrap downloads the Actions runner tarball on every job start (~30–60 s).
To eliminate that latency, pre-install the runner in the base image:

```bash
# Run once inside the base image VM, then snapshot / push the image.
RUNNER_VERSION="2.336.0"   # must match runner_version in config.yaml
RUNNER_HOME="/Users/admin/actions-runner"   # must match bootstrap.sh default
mkdir -p "$RUNNER_HOME" && cd "$RUNNER_HOME"
curl -fsSL -o actions-runner.tar.gz \
  "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-osx-arm64-${RUNNER_VERSION}.tar.gz"
tar xzf actions-runner.tar.gz && rm actions-runner.tar.gz
# Write the version sentinel so bootstrap knows to skip the download.
echo "${RUNNER_VERSION}" > .runner-version
```

Bootstrap reads `.runner-version` and skips the download when the installed version matches `runner_version` in `config.yaml`. A mismatch (or a missing sentinel) causes bootstrap to re-download — safe but slower.

Keep `runner_version` in `config.yaml` in sync with the pre-installed version. When you upgrade the runner, rebuild the base image and update `config.yaml` together.

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
