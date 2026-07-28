# Utsusemi

Ephemeral self-hosted GitHub Actions runners for Apple Silicon Macs. Each job
runs in a fresh Tart VM that is discarded when the job finishes.

## Quick start

```bash
brew tap novr/taps
brew install utsusemi tart
utsusemi configure app --org my-org
utsusemi validate
utsusemi run
```

For a background service, use `brew services start utsusemi` instead of
`utsusemi run`.

## Requirements

- Apple Silicon Mac running macOS 15 or later
- [Tart](https://tart.run/) 2.34 or later

## Paths and credentials

Run setup and the service as the same macOS user. Credentials live in that
user's Keychain and are not written to the config file.

| Path | Default |
|------|---------|
| Config | `~/.config/utsusemi/config.yaml` |
| State | `~/.local/state/utsusemi` |
| Tart images | `~/.tart/` (or `TART_HOME`) |

## Configuration

```text
utsusemi configure app [flags]    # Utsusemi GitHub App (organization runners)
utsusemi configure token [flags]  # fine-grained PAT (organization or repository)
```

| | `configure app` | `configure token` |
|--|-----------------|-------------------|
| Auth | GitHub device flow | fine-grained PAT on stdin or `--token` |
| Targets | organization | organization or repository |
| Credential refresh | automatic (see below) | manual (re-run when the PAT expires) |

Shared runner options:

- `--base-image`, `--pool-size`, `--labels`, `--runner-version`
- `--output` (config path), `--force` (overwrite without prompting)

Organization targets also accept `--runner-group-id` (default `1`).

If the output file already exists, `configure` prompts on an interactive
terminal. Non-interactive runs require `--force`.

Run `utsusemi configure app --help` or `utsusemi configure token --help` for
all flags. See [examples/config.pat.yaml](examples/config.pat.yaml) for a PAT
configuration example.

### GitHub App (organization)

Install the [Utsusemi GitHub App](https://github.com/apps/utsusemiapp) in the
organization, then:

```bash
utsusemi configure app --org my-org
```

The App supports organization runners only and does not request repository
`Administration` permission.

Enable the App optional feature **User-to-server token expiration** (Opt-in).
The host stores a GitHub refresh token in Keychain so credentials can renew
without repeating device flow. `configure app` records which GitHub user
authorized the host.

Re-run `utsusemi configure app` when:

- that user leaves the organization or revokes the App
- the host was offline for more than six months
- automatic refresh fails (check logs; `utsusemi validate` shows the authorized
  user and how long the current credential remains valid)

Stop the service before re-configuring: `brew services stop utsusemi`.

#### Credential lifecycle

| Item | Lifetime | When it renews |
|------|----------|----------------|
| Host credential | 30 days | ≤7 days remain, on `validate`/`run` startup, or after an auth error from the service |
| GitHub refresh token | 6 months of inactivity | Rotates on each automatic refresh (~23 days while running) |
| GitHub user access token | 8 hours | Not stored; used only during refresh |

### Fine-grained PAT

**Repository runner** — token needs repository `Administration: Read and write`:

```bash
printf '%s' "$TOKEN" | utsusemi configure token --repo owner/repo
```

**Organization runner** — token needs organization `Self-hosted runners: Read and write`:

```bash
printf '%s' "$TOKEN" | utsusemi configure token --org my-org
```

Prefer stdin for scripts. `--token` is available when stdin is not; command-line
arguments may be visible to other processes.

## Operations

```bash
utsusemi validate          # check config and credentials
utsusemi status            # local operational summary (no network)
utsusemi list              # list managed VMs and GitHub runners
utsusemi run               # foreground agent (Ctrl+C to stop)
brew services start utsusemi # background service
```

Service logs (Homebrew): `$(brew --prefix)/var/log/utsusemi.log`

### Clean up

Stop Utsusemi, then remove every managed VM and runner for the current config:

```bash
utsusemi clean              # delete all
utsusemi clean --dry-run    # preview only
```

`clean` purges all resources matching the configured VM name prefix. This is
separate from **reclaim**, which runs during `utsusemi run` and removes only
stale or orphaned resources per `reclaim_policy` and `reclaim_grace` in the
config.

## FAQ

### Tart VMs do not start on a headless host

macOS 15+ requires an unlocked `login.keychain` before Tart can start a VM. Log
in once via Screen Sharing (optional: enable automatic login), or unlock the
keychain before starting Utsusemi:

```bash
security unlock-keychain login.keychain
```

### The host creates more than 253 VMs per day

Built-in NAT uses one-day DHCP leases, which exhaust quickly under high churn.
Shorten the lease once per host (see the [Tart FAQ](https://tart.run/faq/)):

```bash
sudo defaults write /Library/Preferences/SystemConfiguration/com.apple.InternetSharing.default.plist bootpd -dict DHCPLeaseTimeSecs -int 600
```

Or set `softnet: true` in the config to use [Softnet](https://github.com/cirruslabs/softnet), which manages leases and isolates VM networking. Install Softnet and grant root (SUID or passwordless sudo):

```bash
brew install cirruslabs/cli/softnet
sudo chown root "$(which softnet)"
sudo chmod +s "$(which softnet)"
```

## License

MIT
