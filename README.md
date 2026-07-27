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
utsusemi configure token "$TOKEN" --repo owner/repo
```

## Fine-grained PAT: organization runner

Create a fine-grained personal access token with organization
`Self-hosted runners: Read and write` permission:

```bash
utsusemi configure token "$TOKEN" --org my-org
```

## Configure

Both configure commands write the runner configuration and store credentials in
the current user's Keychain:

```text
utsusemi configure app [flags]
utsusemi configure token TOKEN [flags]
```

`app` uses the GitHub device flow and supports organization runners. `token`
accepts a fine-grained personal access token and supports organization and
repository runners. Tokens and App credentials are not written to the
configuration file.

Runner options shared by both commands:

- `--base-image`: Tart image used to create runner VMs
- `--pool-size`: Number of runner VMs maintained in the pool
- `--labels`: Comma-separated runner labels
- `--runner-version`: GitHub Actions runner version
- `--output`: Configuration output path

Organization targets also accept `--runner-group-id`; its default is `1`.
Run `utsusemi configure app --help` or
`utsusemi configure token --help` for all options.

## Start the service

```bash
utsusemi validate
brew services start utsusemi
```

See [examples/config.pat.yaml](examples/config.pat.yaml) for a repository
token configuration example.

## Development

```bash
make test
make build
```

## License

MIT
