# Utsusemi

Ephemeral self-hosted GitHub Actions runners for Apple Silicon Macs. Utsusemi
discards the Tart VM after each job completes.

## Requirements

- Apple Silicon Mac running macOS 15 or later
- [Tart](https://tart.run/)
- Admin access to the target GitHub organization or repository

## Installation

```bash
brew tap novr/taps
brew install utsusemi
brew install tart

CONFIG="$HOME/.config/utsusemi/config.yaml"
```

Run the setup commands as the same user that runs the Homebrew service.
Credentials are stored in that user's Keychain.

## Public App: organization runner

Install the Utsusemi GitHub App in the organization, then authorize the host:

```bash
utsusemi register \
  --org my-org \
  --runner-group-id 1 \
  --output "$CONFIG"
```

The Public App supports organization runners only. It does not request the
repository `Administration` permission.

## Fine-grained PAT: repository runner

Create a fine-grained PAT with repository `Administration: Read and write`
permission and expose it as `GITHUB_PAT`:

```bash
utsusemi configure \
  --pat "$GITHUB_PAT" \
  --repo owner/repo \
  --output "$CONFIG"
```

## Fine-grained PAT: organization runner

Create a fine-grained PAT with organization `Self-hosted runners: Read and
write` permission and expose it as `GITHUB_PAT`:

```bash
utsusemi configure \
  --pat "$GITHUB_PAT" \
  --org my-org \
  --runner-group-id 1 \
  --output "$CONFIG"
```

## Start the service

```bash
utsusemi validate --config "$CONFIG"
brew services start utsusemi
```

See [examples/config.pat.yaml](examples/config.pat.yaml) for a repository PAT
configuration example.

## Development

```bash
make test
make build
```

## License

MIT
