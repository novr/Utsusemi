# Oracle Always Free hosted broker

Terraform for a single `VM.Standard.A1.Flex` instance (1 OCPU / 1 GB) running:

- `utsusemi broker` on `127.0.0.1:8787` (linux/arm64)
- Caddy on `:443` terminating TLS for `broker_fqdn`

State and secrets stay outside git (`terraform.tfvars`, `*.tfstate`, PEM files).

## Prerequisites

- OCI tenancy home region with Always Free A1 capacity
- Existing VCN public subnet (Internet Gateway route)
- Reserved public IPv4 OCID (no ephemeral public IP on boot)
- DNS zone you control: **A record → reserved IPv4** before Caddy can obtain a certificate
- GitHub org IP allow list: **reserved IPv4** (domains are not accepted)
- Existing GitHub App PEM and **existing** host JWT signing key (same as the Workers broker; do not generate a new one)
- A GitHub release that already ships `utsusemi_<version>_linux_arm64.tar.gz`, or set `utsusemi_binary_url` in tfvars to a custom tarball URL
- **Ubuntu 22.04 arm64** image (`ssh_user = ubuntu`). cloud-init installs Caddy via apt.

## Apply

```bash
cd deploy/oracle
cp terraform.tfvars.example terraform.tfvars
# edit OCIDs, FQDN, operator_ssh_cidr, utsusemi_version

terraform init
terraform plan
terraform apply
```

If apply fails with A1 capacity errors, change `availability_domain` to another AD in the same region and re-apply.

## Post-apply

1. Point DNS `A` at `terraform output -raw reserved_public_ip`.
2. Wait for propagation, then confirm Caddy serves HTTPS for `broker_fqdn` (TLS-ALPN on :443 only; port 80 is not opened).
3. Copy secrets onto the instance (not via cloud-init):

   ```bash
   SSH_USER="$(terraform output -raw ssh_user)"
   IP="$(terraform output -raw reserved_public_ip)"
   ssh "${SSH_USER}@${IP}"
   sudo cp /etc/utsusemi-broker/env.example /etc/utsusemi-broker/env
   sudoedit /etc/utsusemi-broker/env          # set GITHUB_APP_ID
   sudoedit /etc/utsusemi-broker/app.pem      # GitHub App private key
   sudoedit /etc/utsusemi-broker/signing.pem  # existing Workers host-JWT signing key
   sudo chmod 0600 /etc/utsusemi-broker/env /etc/utsusemi-broker/*.pem
   sudo systemctl enable --now utsusemi-broker # unit waits for env + both PEM paths
   ```

4. On each Mac: `utsusemi configure app --broker "$(terraform output -raw broker_url)"`.
5. Add the reserved IPv4 to the org IP allow list in GitHub.
6. After cutover, stop the Cloudflare Worker deployment and update `DefaultHostedAppBrokerURL` / README.

## Operator notes

- `registration.broker_url` must be the **hostname** (`https://broker.example.com`), not `https://<IPv4>`.
- Broker listens on loopback only; Caddy is the only public entrypoint.
- SSH (22) is restricted to `operator_ssh_cidr` in tfvars.
