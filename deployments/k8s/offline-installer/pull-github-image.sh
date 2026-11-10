#!/usr/bin/env bash
set -Eeuo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
python_command=python3
if ! python3 --version >/dev/null 2>&1; then
  python_command=python
fi
exec "${python_command}" "${SCRIPT_DIR}/scripts/pull-github-image.py" "$@"
