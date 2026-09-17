#!/bin/bash
set -euo pipefail

for port in 80 443; do
	while iptables -D INPUT -p tcp -m tcp --dport "${port}" -j ACCEPT 2>/dev/null; do
		:
	done
done

reject_line="$(iptables -L INPUT -n --line-numbers | awk '/REJECT/ {print $1; exit}')"
if [[ -z "${reject_line}" ]]; then
	echo "open-broker-ports: no REJECT rule in INPUT chain" >&2
	exit 1
fi

open_port() {
	local port="$1"
	iptables -I INPUT "${reject_line}" -p tcp -m tcp --dport "${port}" -j ACCEPT
}

open_port 443
open_port 80

if ! command -v netfilter-persistent >/dev/null 2>&1; then
	DEBIAN_FRONTEND=noninteractive apt-get install -y iptables-persistent
fi
netfilter-persistent save
