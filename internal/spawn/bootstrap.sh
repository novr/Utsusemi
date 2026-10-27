#!/bin/bash
set -euo pipefail
RUNNER_VERSION="${RUNNER_VERSION:?RUNNER_VERSION is required}"
RUNNER_ARCH="${RUNNER_ARCH:?RUNNER_ARCH is required}"
RUNNER_HOME="${RUNNER_HOME:-/Users/admin/actions-runner}"
RUNNER_CACHE_DIR="${RUNNER_CACHE_DIR:-/Volumes/My Shared Files/utsusemi-runner-cache}"
JIT_CONFIG="$(cat)"
if [ -z "${JIT_CONFIG}" ]; then
  echo "jit config is required on stdin" >&2
  exit 1
fi

# System proxy is the default-route service only. Other enabled services on a
# Tart guest can reject networksetup and would fail the spawn. sudo -n avoids
# a password prompt on a tty-less tart exec. DHCP can lag the guest agent.
apply_system_proxy() {
  local http_host="${UTSUSEMI_SYSTEM_PROXY_HTTP_HOST:-}"
  local http_port="${UTSUSEMI_SYSTEM_PROXY_HTTP_PORT:-}"
  local https_host="${UTSUSEMI_SYSTEM_PROXY_HTTPS_HOST:-}"
  local https_port="${UTSUSEMI_SYSTEM_PROXY_HTTPS_PORT:-}"
  if [ -z "$http_host" ] && [ -z "$https_host" ]; then
    if [ -n "$http_port" ] || [ -n "$https_port" ]; then
      echo "bootstrap: system proxy port without host" >&2
      exit 1
    fi
    return 0
  fi
  if [ -n "$http_host" ] && [ -z "$http_port" ]; then
    echo "bootstrap: system proxy http host without port" >&2
    exit 1
  fi
  if [ -z "$http_host" ] && [ -n "$http_port" ]; then
    echo "bootstrap: system proxy http port without host" >&2
    exit 1
  fi
  if [ -n "$https_host" ] && [ -z "$https_port" ]; then
    echo "bootstrap: system proxy https host without port" >&2
    exit 1
  fi
  if [ -z "$https_host" ] && [ -n "$https_port" ]; then
    echo "bootstrap: system proxy https port without host" >&2
    exit 1
  fi

  # tart exec's PATH often has no /sbin. Tests put a fake `route` first on PATH.
  local route_bin="route"
  if ! command -v route >/dev/null 2>&1; then
    route_bin="/sbin/route"
  fi
  if [ ! -x "$route_bin" ] && ! command -v "$route_bin" >/dev/null 2>&1; then
    echo "bootstrap: route not found" >&2
    exit 1
  fi
  local iface=""
  local _
  for _ in 1 2 3 4 5 6 7 8 9 10; do
    iface="$("$route_bin" -n get default 2>/dev/null | awk '$1 == "interface:" { print $2; exit }' || true)"
    if [ -n "$iface" ]; then
      break
    fi
    sleep 0.5
  done
  if [ -z "$iface" ]; then
    echo "bootstrap: no default route for system proxy" >&2
    exit 1
  fi

  local order
  order="$(sudo -n networksetup -listnetworkserviceorder)" || exit 1
  local svc
  svc="$(printf '%s\n' "$order" | awk -v dev="$iface" '
/^\([0-9]+\)/ {
  name = $0
  sub(/^\([0-9]+\)[[:space:]]*/, "", name)
  sub(/^\*[[:space:]]*/, "", name)
  gsub(/^[[:space:]]+|[[:space:]]+$/, "", name)
  next
}
/Device:/ {
  line = $0
  sub(/.*Device:[[:space:]]*/, "", line)
  sub(/[),].*/, "", line)
  gsub(/^[[:space:]]+|[[:space:]]+$/, "", line)
  if (line == dev && name != "") {
    print name
    exit
  }
}
')"
  if [ -z "$svc" ]; then
    echo "bootstrap: no network service for ${iface}" >&2
    exit 1
  fi
  echo "bootstrap: system proxy on ${svc} (${iface})" >&2

  sudo -n networksetup -setproxyautodiscovery "$svc" off
  if [ -n "$http_host" ]; then
    sudo -n networksetup -setwebproxy "$svc" "$http_host" "$http_port" off
    sudo -n networksetup -setwebproxystate "$svc" on
  fi
  if [ -n "$https_host" ]; then
    sudo -n networksetup -setsecurewebproxy "$svc" "$https_host" "$https_port" off
    sudo -n networksetup -setsecurewebproxystate "$svc" on
  fi
  if [ -n "${UTSUSEMI_SYSTEM_PROXY_BYPASS:-}" ]; then
    # *.local must not expand to filenames in the guest cwd.
    set -f
    local domains
    IFS=',' read -r -a domains <<< "${UTSUSEMI_SYSTEM_PROXY_BYPASS}"
    sudo -n networksetup -setproxybypassdomains "$svc" "${domains[@]}"
    set +f
  fi
}
apply_system_proxy

mkdir -p "$RUNNER_HOME"
cd "$RUNNER_HOME"

normalize_version() {
  local v="${1:-}"
  v="${v#"${v%%[![:space:]]*}"}"
  v="${v%"${v##*[![:space:]]}"}"
  v="${v#v}"
  printf '%s' "$v"
}

# Skip download when the runner at the requested version is already installed.
# Prefer asking the binary directly; fall back to the sentinel file written by
# previous installs. This lets stock cirruslabs images (which ship Runner.Listener
# but not the sentinel) work without a custom bake step.
installed="$(normalize_version "$(./bin/Runner.Listener --version 2>/dev/null || cat ./.runner-version 2>/dev/null || true)")"
requested="$(normalize_version "${RUNNER_VERSION}")"
if [ -f ./run.sh ] && [ -n "$installed" ] && [ "$installed" = "$requested" ]; then
  echo "bootstrap: runner v${RUNNER_VERSION} already installed, skipping download" >&2
else
  echo "bootstrap: installing runner v${RUNNER_VERSION}" >&2
  t0=$SECONDS
  tarball_name="actions-runner-${RUNNER_ARCH}-${requested}.tar.gz"
  cache_tarball="${RUNNER_CACHE_DIR}/${tarball_name}"
  # HealthCheck can succeed before virtiofs appears. Wait for the mount dir only —
  # if the dir is already present without the tarball, fall through to curl immediately
  # (do not sleep 5s on every miss when Ensure failed on the host).
  if [ ! -d "$RUNNER_CACHE_DIR" ]; then
    for _ in 1 2 3 4 5 6 7 8 9 10; do
      [ -d "$RUNNER_CACHE_DIR" ] && break
      sleep 0.5
    done
  fi
  if [ -f "$cache_tarball" ]; then
    echo "bootstrap: using host cache" >&2
    cp "$cache_tarball" actions-runner.tar.gz
  else
    echo "bootstrap: downloading from GitHub" >&2
    curl -fsSL --retry 3 --retry-delay 2 --connect-timeout 10 --max-time 300 \
      -o actions-runner.tar.gz \
      "https://github.com/actions/runner/releases/download/v${requested}/actions-runner-${RUNNER_ARCH}-${requested}.tar.gz"
  fi
  tar xzf actions-runner.tar.gz
  rm -f actions-runner.tar.gz
  echo "${RUNNER_VERSION}" > ./.runner-version
  echo "bootstrap: runner download+install took $((SECONDS - t0))s" >&2
fi

unset UTSUSEMI_SYSTEM_PROXY_HTTP_HOST UTSUSEMI_SYSTEM_PROXY_HTTP_PORT \
  UTSUSEMI_SYSTEM_PROXY_HTTPS_HOST UTSUSEMI_SYSTEM_PROXY_HTTPS_PORT \
  UTSUSEMI_SYSTEM_PROXY_BYPASS
./run.sh --jitconfig "${JIT_CONFIG}"
