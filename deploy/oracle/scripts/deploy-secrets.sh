#!/bin/bash
set -euo pipefail
export LC_ALL=C

usage() {
	cat <<'EOF'
Usage: deploy-secrets.sh --app-pem PATH --signing-pem PATH [--github-app-id ID]

Copy broker secrets onto the Terraform-managed instance and start utsusemi-broker.
Run from deploy/oracle after terraform apply. PEM paths stay on the operator machine;
they are not written to Terraform state or cloud-init.

Environment:
  GITHUB_APP_ID        Used when --github-app-id is omitted
  SSH_IDENTITY_FILE    SSH private key (default: ~/.ssh/id_ecdsa)

Options:
  --app-pem PATH       GitHub App private key PEM
  --signing-pem PATH   Host JWT signing key PEM (reuse the Workers broker key)
  --github-app-id ID   GitHub App ID for /etc/utsusemi-broker/env
  --utsusemi-version V Override binary URL (default: terraform output utsusemi_binary_url)
  -h, --help           Show this help
EOF
}

root_dir="$(cd "$(dirname "$0")/.." && pwd)"
app_pem=""
signing_pem=""
github_app_id="${GITHUB_APP_ID:-}"
utsusemi_version=""
ssh_identity="${SSH_IDENTITY_FILE:-$HOME/.ssh/id_ecdsa}"
unit_file="${root_dir}/scripts/utsusemi-broker.service"
ports_script="${root_dir}/scripts/open-broker-ports.sh"

while [[ $# -gt 0 ]]; do
	case "$1" in
	--app-pem)
		app_pem="$2"
		shift 2
		;;
	--signing-pem)
		signing_pem="$2"
		shift 2
		;;
	--github-app-id)
		github_app_id="$2"
		shift 2
		;;
	--utsusemi-version)
		utsusemi_version="$2"
		shift 2
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		echo "deploy-secrets: unknown argument: $1" >&2
		usage >&2
		exit 1
		;;
	esac
done

if [[ -z "${app_pem}" || -z "${signing_pem}" ]]; then
	echo "deploy-secrets: --app-pem and --signing-pem are required" >&2
	usage >&2
	exit 1
fi
if [[ -z "${github_app_id}" ]]; then
	echo "deploy-secrets: set --github-app-id or GITHUB_APP_ID" >&2
	exit 1
fi
if [[ ! -f "${app_pem}" ]]; then
	echo "deploy-secrets: app PEM not found: ${app_pem}" >&2
	exit 1
fi
if [[ ! -f "${signing_pem}" ]]; then
	echo "deploy-secrets: signing PEM not found: ${signing_pem}" >&2
	exit 1
fi
if [[ ! -f "${ssh_identity}" ]]; then
	echo "deploy-secrets: SSH key not found: ${ssh_identity}" >&2
	exit 1
fi
if [[ ! -f "${unit_file}" ]]; then
	echo "deploy-secrets: missing ${unit_file}" >&2
	exit 1
fi

if [[ -n "${utsusemi_version}" ]]; then
	binary_url="https://github.com/novr/utsusemi/releases/download/v${utsusemi_version}/utsusemi_${utsusemi_version}_linux_arm64.tar.gz"
elif binary_url="$(terraform -chdir="${root_dir}" output -raw utsusemi_binary_url 2>/dev/null)"; then
	:
else
	echo "deploy-secrets: terraform output utsusemi_binary_url missing; pass --utsusemi-version or run terraform apply" >&2
	exit 1
fi

ssh_user="$(terraform -chdir="${root_dir}" output -raw ssh_user)"
ip="$(terraform -chdir="${root_dir}" output -raw reserved_public_ip)"
broker_fqdn="$(terraform -chdir="${root_dir}" output -raw broker_url | sed 's#^https://##')"
ssh_target="${ssh_user}@${ip}"
ssh_opts=(-i "${ssh_identity}" -o StrictHostKeyChecking=accept-new)

tmp_app="$(mktemp -t utsusemi-app.XXXXXX.pem)"
tmp_signing="$(mktemp -t utsusemi-signing.XXXXXX.pem)"
trap 'rm -f "${tmp_app}" "${tmp_signing}"' EXIT

install -m 0600 "${app_pem}" "${tmp_app}"
install -m 0600 "${signing_pem}" "${tmp_signing}"

scp "${ssh_opts[@]}" "${tmp_app}" "${tmp_signing}" "${unit_file}" "${ports_script}" "${ssh_target}:/tmp/"

remote_app="$(basename "${tmp_app}")"
remote_signing="$(basename "${tmp_signing}")"

ssh "${ssh_opts[@]}" "${ssh_target}" env LC_ALL=C bash -s -- \
	"${remote_app}" "${remote_signing}" "${github_app_id}" "${binary_url}" "${broker_fqdn}" <<'EOF'
set -euo pipefail
remote_app="$1"
remote_signing="$2"
github_app_id="$3"
binary_url="$4"
broker_fqdn="$5"

if command -v cloud-init >/dev/null; then
	sudo cloud-init status --wait || true
fi

if [[ -x /usr/local/sbin/utsusemi-open-broker-ports.sh ]]; then
	sudo /usr/local/sbin/utsusemi-open-broker-ports.sh
elif [[ -f /tmp/open-broker-ports.sh ]]; then
	sudo install -m 0755 /tmp/open-broker-ports.sh /usr/local/sbin/utsusemi-open-broker-ports.sh
	sudo /usr/local/sbin/utsusemi-open-broker-ports.sh
	rm -f /tmp/open-broker-ports.sh
fi

ensure_caddy() {
	sudo install -d -m 0755 /etc/caddy
	if [[ ! -f /etc/caddy/Caddyfile ]]; then
		sudo tee /etc/caddy/Caddyfile >/dev/null <<CADDYEOF
${broker_fqdn} {
  reverse_proxy 127.0.0.1:8787
}
CADDYEOF
	fi
	if ! command -v caddy >/dev/null; then
		echo "deploy-secrets: installing Caddy"
		sudo bash -euxc 'install -d -m 0755 /usr/share/keyrings && curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/gpg.key" | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg && curl -1sLf "https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt" | tee /etc/apt/sources.list.d/caddy-stable.list && apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y -o Dpkg::Options::="--force-confold" caddy'
	fi
	caddy validate --config /etc/caddy/Caddyfile
	sudo systemctl enable caddy
	sudo systemctl restart caddy
}
ensure_caddy

sudo install -d -m 0750 -o root -g root /etc/utsusemi-broker
sudo install -m 0640 -o root -g root "/tmp/${remote_app}" /etc/utsusemi-broker/app.pem
sudo install -m 0640 -o root -g root "/tmp/${remote_signing}" /etc/utsusemi-broker/signing.pem
rm -f "/tmp/${remote_app}" "/tmp/${remote_signing}"

if [[ ! -f /etc/utsusemi-broker/env ]]; then
	if [[ -f /etc/utsusemi-broker/env.example ]]; then
		sudo cp /etc/utsusemi-broker/env.example /etc/utsusemi-broker/env
	else
		sudo tee /etc/utsusemi-broker/env >/dev/null <<ENVEOF
GITHUB_APP_ID=
UTSUSEMI_GITHUB_APP_PRIVATE_KEY_FILE=/etc/utsusemi-broker/app.pem
UTSUSEMI_CREDENTIAL_SIGNING_KEY_FILE=/etc/utsusemi-broker/signing.pem
JWT_ISSUER=utsusemi-broker
JWT_VERSION=1
ENVEOF
	fi
	sudo chmod 0640 /etc/utsusemi-broker/env
fi
if sudo grep -q '^GITHUB_APP_ID=' /etc/utsusemi-broker/env; then
	sudo sed -i "s/^GITHUB_APP_ID=.*/GITHUB_APP_ID=${github_app_id}/" /etc/utsusemi-broker/env
else
	echo "GITHUB_APP_ID=${github_app_id}" | sudo tee -a /etc/utsusemi-broker/env >/dev/null
fi

if [[ ! -x /usr/local/bin/utsusemi ]]; then
	echo "deploy-secrets: installing utsusemi from ${binary_url}"
	if ! curl -fsSL "${binary_url}" -o /tmp/utsusemi.tar.gz; then
		echo "deploy-secrets: download failed" >&2
		sudo tail -30 /var/log/cloud-init-output.log >&2 || true
		exit 1
	fi
	sudo tar -xzf /tmp/utsusemi.tar.gz -C /usr/local/bin utsusemi
	sudo chmod 0755 /usr/local/bin/utsusemi
	rm -f /tmp/utsusemi.tar.gz
fi

sudo install -m 0644 /tmp/utsusemi-broker.service /etc/systemd/system/utsusemi-broker.service
rm -f /tmp/utsusemi-broker.service
sudo systemctl daemon-reload

sudo systemctl enable utsusemi-broker
sudo systemctl restart utsusemi-broker
sudo systemctl --no-pager status utsusemi-broker
# Root path returns 404; any HTTP response means the broker is listening.
for _ in $(seq 1 30); do
	if curl -sSI http://127.0.0.1:8787/ | head -1 | grep -qE '^HTTP/'; then
		exit 0
	fi
	sleep 1
done
echo "deploy-secrets: broker did not become ready on :8787" >&2
exit 1
EOF

echo "deploy-secrets: broker secrets installed on ${ssh_target}"
