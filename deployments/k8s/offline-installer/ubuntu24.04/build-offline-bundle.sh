#!/usr/bin/env bash

set -Eeuo pipefail

VERSION_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
exec bash "${VERSION_DIR}/../build-offline-bundle.sh" \
  --output "${VERSION_DIR}/dist" "$@" --ubuntu-version 24.04
