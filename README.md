# Utsusemi

## Quick start

```bash
brew tap novr/taps
brew install utsusemi
utsusemi configure app --org my-org
utsusemi validate
utsusemi run
```

```bash
brew services start utsusemi
```

## Requirements

- Apple Silicon Mac, macOS 15+
- [Tart](https://tart.run/) 2.34+

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

Install the [Utsusemi GitHub App](https://github.com/apps/utsusemiapp). Enable **User-to-server token expiration** (Opt-in).

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

| Key | Default | Notes |
|-----|---------|-------|
| `reclaim_policy` | `grace` | `soft` — local dev; `hard` — immediate |
| `reclaim_grace` | `15m` | When `reclaim_policy` is `grace` |
| `reconciliation_interval` | `5m` | Reclaim interval during `run` |

## Provider

### Tart

`provider: tart`

- **base_image** / `--base-image` — pull on agent start ([Tart](https://tart.run/))
- **min_free_disk_gb** — default `50`
- **Keychain**:
  ```bash
  security unlock-keychain login.keychain
  security set-keychain-settings -t 0 ~/Library/Keychains/login.keychain-db
  ```
- **Networking** — [Tart FAQ](https://tart.run/faq/); `softnet: true` → [Softnet](https://github.com/cirruslabs/softnet)

## Operations

```bash
utsusemi --version
utsusemi validate
utsusemi status
utsusemi list
utsusemi run
brew services start utsusemi
```

```bash
utsusemi clean
utsusemi clean --dry-run
```

Stop the agent before `clean`.

### Service logs

- `$(brew --prefix)/var/log/utsusemi.log`
- `$(brew --prefix)/var/log/utsusemi.error.log`

```bash
BREW_PREFIX="$(brew --prefix)"
sudo tee /etc/newsyslog.d/utsusemi.conf <<EOF
${BREW_PREFIX}/var/log/utsusemi.log       644  7  10240  *  J
${BREW_PREFIX}/var/log/utsusemi.error.log 644  7  10240  *  J
EOF
sudo newsyslog -nv
```

Template: [examples/utsusemi.newsyslog.conf](examples/utsusemi.newsyslog.conf).

## License

MIT
