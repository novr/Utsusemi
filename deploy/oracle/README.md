# Oracle Always Free hosted broker

Terraform for a single `VM.Standard.A1.Flex` instance (1 OCPU / 1 GB) running:

- `utsusemi broker` on `127.0.0.1:8787` (linux/arm64)
- Caddy on `:443` terminating TLS for `broker_fqdn`

State and secrets stay outside git (`terraform.tfvars`, `*.tfstate`, PEM files).

## Prerequisites

- OCI tenancy home region with Always Free A1 capacity
- Reserved public IPv4 OCID (no ephemeral public IP on boot)
- **A domain you control** with DNS **A record → reserved IPv4** before Caddy can obtain a certificate
- VCN: set `create_vcn = true` (default) or supply an existing public `subnet_id`
- GitHub org IP allow list (if enabled): add the **UtsusemiApp** broker IPv4 on the App IP allow list
  (orgs can inherit it), plus each Mac host egress IPv4 for device flow and runner jobs
- Existing GitHub App PEM and **existing** host JWT signing key (same as the Workers broker; do not generate a new one)
- A GitHub release that already ships `utsusemi_<version>_linux_arm64.tar.gz`, or set `utsusemi_binary_url` in tfvars to a custom tarball URL
- **Ubuntu 22.04 arm64** image (`ssh_user = ubuntu`). cloud-init installs Caddy via apt.

## Apply

```bash
cd deploy/oracle
terraform init          # required once (provider lock file is committed)

cp terraform.tfvars.example terraform.tfvars
# edit OCIDs, FQDN, operator_ssh_cidr, utsusemi_version

terraform plan
terraform apply
```

`terraform plan` without `terraform.tfvars` prompts for every variable interactively.
Provider auth uses `~/.oci/config` (default profile).

If apply fails with **Out of host capacity** on `VM.Standard.A1.Flex`, wait and retry `terraform apply`
(spacing retries by 15+ minutes; do not hammer the API). Multi-AD home regions can try another
`availability_domain`; **ap-tokyo-1 has only `kIam:AP-TOKYO-1-AD-1`**. A partial apply (VCN up,
instance missing) is safe to re-apply — Terraform only creates what is still absent.

## Post-apply

1. Point DNS `A` at `terraform output -raw reserved_public_ip`.
2. Wait for propagation, then confirm ports and HTTPS:

   ```bash
   IP="$(terraform output -raw reserved_public_ip)"
   nc -zv "${IP}" 80 443
   curl -I "$(terraform output -raw broker_url)"
   ```

   NSG opens :80/:443. Canonical Ubuntu on OCI also blocks inbound in host `iptables` except SSH;
   cloud-init runs `utsusemi-open-broker-ports.sh` before Caddy starts. If you applied an older
   cloud-init revision, fix the host firewall without replacing the instance:

   ```bash
   scp scripts/open-broker-ports.sh "ubuntu@${IP}:/tmp/"
   ssh "ubuntu@${IP}" 'sudo install -m 0755 /tmp/open-broker-ports.sh /usr/local/sbin/utsusemi-open-broker-ports.sh && sudo /usr/local/sbin/utsusemi-open-broker-ports.sh'
   ```

   After repeated failed ACME attempts, Let's Encrypt may rate-limit the hostname for up to an hour;
   wait, then `sudo systemctl restart caddy`.

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
5. If the org uses a GitHub IP allow list: enable **IP allow list configuration for installed
   GitHub Apps** (UtsusemiApp lists the broker reserved IPv4), and add each Mac egress `/32`.
6. After cutover, stop the Cloudflare Worker deployment (`worker/` is legacy once Oracle is live).

## Operator notes

- `registration.broker_url` must be `https://` with a hostname (e.g. `https://broker.example.com`);
  bare `https://<IPv4>` is rejected by `utsusemi configure app`.
- Broker listens on loopback only; Caddy is the only public entrypoint.
- SSH (22) is restricted to `operator_ssh_cidr` in the NSG. When `create_vcn = true`, the subnet uses a
  permissive security list so NSG rules are effective (OCI requires both to allow traffic).
- Host `iptables` ACCEPT rules must sit **before** the image `REJECT` rule. Rules appended after `REJECT`
  have no effect (a common manual mistake). Use `scripts/open-broker-ports.sh` or re-run
  `/usr/local/sbin/utsusemi-open-broker-ports.sh` on the instance.
- Changing `cloud-init.yaml.tpl` replaces the compute instance on the next `terraform apply` (OCI
  `user_data` is create-only). Prefer the port script on running VMs when you only need a firewall fix.

## No domain yet?

Caddy needs a resolvable FQDN with DNS pointing at the reserved IPv4 before it can obtain a certificate.

Until you have one:

1. **Terraform apply** can still create VCN + instance (set a placeholder `broker_fqdn` for the Caddyfile; Caddy will not get a cert until DNS exists).
2. **GitHub org IP allow list** uses the broker **reserved IPv4** on the App IP list (no domain).
3. **`utsusemi configure app --broker`** waits until `https://<your-fqdn>` works.

Minimal path: register a cheap domain, add `A` → `terraform output -raw reserved_public_ip`, update `broker_fqdn` and re-apply or edit `/etc/caddy/Caddyfile` on the instance.

Without any domain, use the **Workers broker** or a **loopback broker on your Mac** (`utsusemi broker`) instead of this stack.
