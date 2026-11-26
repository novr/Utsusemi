#!/bin/bash
# Inject host-resolved proxy hostnames into /etc/hosts, then verify guest lookup.
# stdin: lines of "<ip> <hostname>" (already validated by the host agent).
set -euo pipefail

host_re='^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$|^[A-Za-z0-9]$'
ip_re='^[0-9a-fA-F:.]+$'

entries=()
while IFS= read -r line || [ -n "${line:-}" ]; do
  line="${line#"${line%%[![:space:]]*}"}"
  line="${line%"${line##*[![:space:]]}"}"
  if [ -z "$line" ]; then
    continue
  fi
  # Exactly two fields; reject anything with extra whitespace or comments from stdin.
  set -f
  # shellcheck disable=SC2086
  set -- $line
  set +f
  if [ "$#" -ne 2 ]; then
    echo "hosts_setup: expected '<ip> <host>', got: ${line}" >&2
    exit 1
  fi
  ip="$1"
  host="$2"
  if ! [[ "$ip" =~ $ip_re ]]; then
    echo "hosts_setup: invalid ip" >&2
    exit 1
  fi
  if ! [[ "$host" =~ $host_re ]]; then
    echo "hosts_setup: invalid host" >&2
    exit 1
  fi
  entries+=("${ip} ${host}")
done

if [ "${#entries[@]}" -eq 0 ]; then
  echo "hosts_setup: no entries" >&2
  exit 1
fi

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
{
  echo "# utsusemi proxy hosts"
  for e in "${entries[@]}"; do
    printf '%s # utsusemi\n' "$e"
  done
} >"$tmp"

if ! sudo -n tee -a /etc/hosts <"$tmp" >/dev/null; then
  echo "hosts_setup: failed to append /etc/hosts (guest user needs passwordless sudo)" >&2
  exit 1
fi

# Best-effort cache flush; verification below is authoritative.
sudo -n dscacheutil -flushcache >/dev/null 2>&1 || true
sudo -n killall -HUP mDNSResponder >/dev/null 2>&1 || true

for e in "${entries[@]}"; do
  set -f
  # shellcheck disable=SC2086
  set -- $e
  set +f
  ip="$1"
  host="$2"
  found=0
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    out="$(dscacheutil -q host -a name "$host" 2>/dev/null || true)"
    # Fixed-string match; avoid treating '.' in IPv4 as a regex metacharacter.
    if printf '%s\n' "$out" | grep -F -q "ip_address: ${ip}" || printf '%s\n' "$out" | grep -F -q "ipv6_address: ${ip}"; then
      found=1
      break
    fi
    sleep 0.2
  done
  if [ "$found" != "1" ]; then
    echo "hosts_setup: guest lookup for ${host} did not return ${ip}" >&2
    exit 1
  fi
done

echo "hosts_setup: injected ${#entries[@]} entries" >&2
