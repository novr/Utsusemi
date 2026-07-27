# Utsusemi

Ephemeral self-hosted GitHub Actions runners for Apple Silicon Macs. Utsusemi
discards the Tart VM after each job completes.

## Requirements

- Apple Silicon Mac running macOS 15 or later
- [Tart](https://tart.run/)

## Installation

```bash
brew tap novr/taps
brew install utsusemi
brew install tart
```

Run the setup commands as the same user that runs the Homebrew service.
Credentials are stored in that user's Keychain. The default config path is
`~/.config/utsusemi/config.yaml`.

## Public App: organization runner

Install the Utsusemi GitHub App in the organization, then authorize the host:

```bash
utsusemi register --org my-org
```

The Public App supports organization runners only. It does not request the
repository `Administration` permission.

## Fine-grained PAT: repository runner

Create a fine-grained PAT with repository `Administration: Read and write`
permission and expose it as `GITHUB_PAT`:

```bash
utsusemi configure \
  --pat "$GITHUB_PAT" \
  --repo owner/repo
```

## Fine-grained PAT: organization runner

Create a fine-grained PAT with organization `Self-hosted runners: Read and
write` permission and expose it as `GITHUB_PAT`:

```bash
utsusemi configure \
  --pat "$GITHUB_PAT" \
  --org my-org
```

For either PAT target, `configure` writes the runner configuration and stores
the token in the current user's Keychain. The token is not written to the
configuration file.

## Start the service

```bash
utsusemi validate
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
