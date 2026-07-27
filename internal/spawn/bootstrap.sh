#!/bin/bash
set -euo pipefail
RUNNER_VERSION="${RUNNER_VERSION:?RUNNER_VERSION is required}"
RUNNER_HOME="${RUNNER_HOME:-/Users/admin/actions-runner}"
mkdir -p "$RUNNER_HOME"
cd "$RUNNER_HOME"
if [ ! -f ./config.sh ]; then
  curl -fsSL -o actions-runner.tar.gz "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-osx-arm64-${RUNNER_VERSION}.tar.gz"
  tar xzf actions-runner.tar.gz
  rm actions-runner.tar.gz
fi
./config.sh --jitconfig --disableupdate --ephemeral
