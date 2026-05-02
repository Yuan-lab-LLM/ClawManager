#!/bin/bash
set -euo pipefail
# ClawManager Agent Entrypoint Wrapper
# 在原始 bootstrap 之前启动 agent shim 守护进程

SHIM_SCRIPT="/opt/company/clawmanager-agent-shim.js"

if [ "${CLAWMANAGER_AGENT_ENABLED:-}" = "true" ] && [ -f "$SHIM_SCRIPT" ]; then
    echo "[agent-entrypoint] starting agent shim in background..."
    node "$SHIM_SCRIPT" &
fi

# 委托给原始 bootstrap
exec /usr/local/bin/bootstrap-openclaw "$@"

