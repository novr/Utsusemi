#!/bin/bash
set -euo pipefail
RUNNER_VERSION="${RUNNER_VERSION:?RUNNER_VERSION is required}"
RUNNER_HOME="${RUNNER_HOME:-/Users/admin/actions-runner}"
JIT_CONFIG="$(cat)"
if [ -z "${JIT_CONFIG}" ]; then
  echo "jit config is required on stdin" >&2
  exit 1
fi
mkdir -p "$RUNNER_HOME"
cd "$RUNNER_HOME"
if [ ! -f ./run.sh ]; then
  curl -fsSL -o actions-runner.tar.gz "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-osx-arm64-${RUNNER_VERSION}.tar.gz"
  tar xzf actions-runner.tar.gz
  rm actions-runner.tar.gz
fi
./run.sh --jitconfig "${JIT_CONFIG}"
