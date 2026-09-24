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

./run.sh --jitconfig "${JIT_CONFIG}"
