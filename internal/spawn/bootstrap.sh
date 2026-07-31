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
# The .runner-version sentinel is written after each install, and must also be
# present in pre-baked base images (see README § "Pre-installed runner").
if [ -f ./run.sh ] && [ -f ./.runner-version ] && [ "$(cat ./.runner-version)" = "${RUNNER_VERSION}" ]; then
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
