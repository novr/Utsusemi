# Utsusemi

Ephemeral self-hosted GitHub Actions runners for Apple Silicon Macs. Utsusemi
discards the Tart VM after each job completes.

## Requirements

- Apple Silicon Mac running macOS 15 or later
- [Tart](https://tart.run/) 2.34 or later

## Installation

```bash
brew tap novr/taps
brew install utsusemi
brew install tart
```

Run the setup commands as the same user that runs the Homebrew service.
Credentials are stored in that user's Keychain. The default config path is
`~/.config/utsusemi/config.yaml`. Runtime state defaults to
`~/.local/state/utsusemi`. Tart stores VM images under `~/.tart/` (or
`TART_HOME`).

## Utsusemi GitHub App: organization runner

Install the [Utsusemi GitHub App](https://github.com/apps/utsusemiapp) in the
organization, then authorize the host:

```bash
utsusemi configure app --org my-org
```

`configure app` authorizes the host with the App and writes the runner
configuration. The App supports organization runners only and does not request
the repository `Administration` permission.

## Fine-grained PAT: repository runner

Create a fine-grained personal access token with repository
`Administration: Read and write` permission:

```bash
printf '%s' "$TOKEN" | utsusemi configure token --repo owner/repo
```

## Fine-grained PAT: organization runner

Create a fine-grained personal access token with organization
`Self-hosted runners: Read and write` permission:

```bash
printf '%s' "$TOKEN" | utsusemi configure token --org my-org
```

## Configure

Both configure commands write the runner configuration and store credentials in
the current user's Keychain:

```text
utsusemi configure app [flags]
utsusemi configure token [flags]
```

`app` uses the GitHub device flow and supports organization runners. `token`
accepts a fine-grained personal access token and supports organization and
repository runners. Tokens and App credentials are not written to the
configuration file.

`configure token` reads the token from stdin. For scripts where stdin is not
available, pass it with `--token`; command-line arguments may be visible to
other processes.

Runner options shared by both commands:

- `--base-image`: Base image used to create runner VMs
- `--pool-size`: Number of runner VMs maintained in the pool
- `--labels`: Comma-separated runner labels
- `--runner-version`: GitHub Actions runner version
- `--output`: Configuration output path
- `--force`: Overwrite an existing config without prompting

If the output path already exists, `configure` asks for confirmation on an
interactive terminal. Non-interactive runs require `--force`.

Organization targets also accept `--runner-group-id`; its default is `1`.
Run `utsusemi configure app --help` or
`utsusemi configure token --help` for all options.

See [examples/config.pat.yaml](examples/config.pat.yaml) for a repository
token configuration example.

## Start Utsusemi

```bash
utsusemi validate
utsusemi run
```

`utsusemi run` runs in the foreground and stops with Ctrl+C. To run Utsusemi
as a background service instead:

```bash
brew services start utsusemi
```

## Clean up

Stop Utsusemi, then delete every managed Tart VM and GitHub runner for the
current config:

```bash
utsusemi clean
```

Preview what would be removed:

```bash
utsusemi clean --dry-run
```

## FAQ

### Tart VMs do not start on a headless host

macOS 15+ requires an unlocked `login.keychain` before Tart can start a VM. Log
in once via Screen Sharing (and optionally enable automatic login), or unlock
the keychain before starting Utsusemi:

```bash
security unlock-keychain login.keychain
```

### The host creates more than 253 VMs per day

With the built-in NAT network, macOS hands out one-day DHCP leases, which can be
exhausted under high VM churn. Shorten the lease time once per host (see the
[Tart FAQ](https://tart.run/faq/)):

```bash
sudo defaults write /Library/Preferences/SystemConfiguration/com.apple.InternetSharing.default.plist bootpd -dict DHCPLeaseTimeSecs -int 600
```

Alternatively, set `softnet: true` in the config to run VMs with
[Softnet](https://github.com/cirruslabs/softnet), which manages leases
automatically and isolates VM networking. Softnet must be installed and granted
root (SUID bit or passwordless sudo):

```bash
brew install cirruslabs/cli/softnet
sudo chown root "$(which softnet)"
sudo chmod +s "$(which softnet)"
```

## Development

```bash
make test
make build
```

## License

MIT
