# Utsusemi

Ephemeral self-hosted GitHub Actions runners for Apple Silicon Macs. Utsusemi
discards the Tart VM after each job completes.

## Requirements

- Apple Silicon Mac running macOS 15 or later
- Admin access to the target GitHub organization or repository
- Fine-grained PAT when using direct PAT authentication
- Host dependencies required by the selected VM provider, such as
  [Tart](https://tart.run/) for `provider: tart`

## Installation with direct PAT authentication

```bash
brew tap novr/taps
brew install utsusemi
brew install tart   # Required when provider is tart

sudo utsusemi configure --pat \
  --repo owner/repo \
  --output /etc/utsusemi/config.yaml

utsusemi validate --config /etc/utsusemi/config.yaml
brew services start utsusemi
```

## Organization runner

```bash
utsusemi configure --pat \
  --org my-org \
  --runner-group-id 1 \
  --output /etc/utsusemi/config.yaml
```

## Public App registration

```bash
utsusemi register --broker https://broker.utsusemi.dev \
  --org my-org \
  --runner-group-id 1
brew services start utsusemi
```

The Public App supports organization-level runners only. Repository-level
runner management requires the GitHub App's `Administration: Read and write`
repository permission, so use direct PAT authentication for repository
runners.

See [examples/config.pat.yaml](examples/config.pat.yaml) for an example
configuration. Credentials are stored only in Keychain.

## Development

```bash
make test
make build
```

## License

MIT
