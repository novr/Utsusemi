#!/bin/bash
set -euo pipefail
RUNNER_VERSION="${RUNNER_VERSION:?RUNNER_VERSION is required}"
RUNNER_ARCH="${RUNNER_ARCH:?RUNNER_ARCH is required}"
RUNNER_HOME="${RUNNER_HOME:-/Users/admin/actions-runner}"
JIT_CONFIG="$(cat)"
if [ -z "${JIT_CONFIG}" ]; then
  echo "jit config is required on stdin" >&2
  exit 1
fi
mkdir -p "$RUNNER_HOME"
cd "$RUNNER_HOME"

# Skip download when the runner at the requested version is already installed.
# Prefer asking the binary directly; fall back to the sentinel file written by
# previous installs. This lets stock cirruslabs images (which ship Runner.Listener
# but not the sentinel) work without a custom bake step.
installed="$(./bin/Runner.Listener --version 2>/dev/null || cat ./.runner-version 2>/dev/null || true)"
if [ -f ./run.sh ] && [ "$installed" = "${RUNNER_VERSION}" ]; then
  echo "bootstrap: runner v${RUNNER_VERSION} already installed, skipping download" >&2
else
  echo "bootstrap: installing runner v${RUNNER_VERSION}" >&2
  t0=$SECONDS
  curl -fsSL -o actions-runner.tar.gz "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-${RUNNER_ARCH}-${RUNNER_VERSION}.tar.gz"
  tar xzf actions-runner.tar.gz
  rm actions-runner.tar.gz
  echo "${RUNNER_VERSION}" > ./.runner-version
  echo "bootstrap: runner download+install took $((SECONDS - t0))s" >&2
fi

./run.sh --jitconfig "${JIT_CONFIG}"
