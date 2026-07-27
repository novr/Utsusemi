# Manual E2E checklist

## PAT + Repo

1. Create a fine-grained PAT with `Administration: Read and write` on the target repository.
2. Install Tart and build/install `utsusemi`.
3. Run `utsusemi configure --pat --repo owner/repo --output /tmp/utsusemi/config.yaml`.
4. Run `utsusemi run --config /tmp/utsusemi/config.yaml`.
5. Trigger a workflow with `runs-on: [self-hosted, macOS]`.
6. Confirm the job completes and `tart list` has no lingering `utsusemi-` VMs.
7. Restart the agent and confirm reconciliation removes orphan runners if any remain.

## Broker modes

- Own App: deploy `worker/` with `BROKER_API_KEY` and `ALLOWED_TARGETS`.
- Public App: run `utsusemi register --broker <url> --repo owner/repo`.
