#!/bin/bash
# ClawManager workspace init script
# Run this at the start of each dev session

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
echo "Project root: $PROJECT_ROOT"

# --- Environment check ---
echo ""
echo "=== Environment Check ==="
go version 2>/dev/null && echo "✅ Go available" || echo "❌ Go not found"
node --version 2>/dev/null && echo "✅ Node available" || echo "❌ Node not found"
kubectl version --client --short 2>/dev/null && echo "✅ kubectl available" || echo "❌ kubectl not found"
k3d version 2>/dev/null && echo "✅ k3d available" || echo "❌ k3d not found (install: brew install k3d)"
docker info >/dev/null 2>&1 && echo "✅ Docker running" || echo "❌ Docker not running (required for k3d)"

# --- K3S cluster check ---
echo ""
echo "=== K3S Cluster Status ==="
if k3d cluster list 2>/dev/null | grep -q "clawmanager"; then
    echo "✅ k3d cluster 'clawmanager' exists"
    kubectl get pods -n clawmanager-system 2>/dev/null || echo "(cluster may not be started)"
else
    echo "❌ k3d cluster 'clawmanager' not found"
    echo "   To create: k3d cluster create clawmanager --port '30443:30443@loadbalancer'"
fi

# --- Backend build check ---
echo ""
echo "=== Backend Build Check ==="
cd "$PROJECT_ROOT/backend"
go build ./... && echo "✅ Backend builds successfully" || echo "❌ Backend build failed"

# --- Commands Reference ---
echo ""
echo "=== Quick Command Reference ==="
echo "  Backend test:     cd backend && go test ./..."
echo "  Frontend dev:     cd frontend && npm run dev"
echo "  Create cluster:   k3d cluster create clawmanager --port '30443:30443@loadbalancer'"
echo "  Deploy:           kubectl apply -f deployments/k3s/clawmanager.yaml"
echo "  Watch pods:       kubectl get pods -n clawmanager-system -w"
echo "  Access portal:    https://localhost:30443"
echo ""
