# Control Plane Backend Build/Deploy Gate

Date: 2026-05-05

Worker: ControlPlaneBackendBuildDeployWorker

Topology: serial

## Verdict

CONTROL_PLANE_BACKEND_BUILD_DEPLOY_DONE

The approved backend/control-plane source changes were built into a backend-only derivative of the current ClawManager control-plane image, imported into the local k3d cluster, and deployed only to `Deployment/clawmanager-app` in namespace `clawmanager-system`, container `clawmanager-app`.

No fresh instance was created. No browser E2E was run. No runtime image rebuild, frontend rebuild, database migration, manual pod patch, manual Service patch, `kubectl cp`, `passes:true`, Close, longterm write-back, or Mem0 write occurred.

## Dependency Gates

| Gate | Evidence | Status used |
| --- | --- | --- |
| Control Plane Backend Build/Deploy Approval Packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-plane-backend-build-deploy-approval-packet.md` | `CONTROL_PLANE_BACKEND_BUILD_DEPLOY_APPROVAL_PACKET_DONE`; user explicitly approved this gate |
| Backend Runtime Listener Follow-up Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md` | `BACKEND_RUNTIME_LISTENER_FOLLOWUP_IMPLEMENTATION_DONE` |
| Fresh rerun after cluster recovery | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md` | still blocked until this deploy; running backend previously exposed `3001` but not `18789` |
| WS Auth Bridge Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md` | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE` |
| Control UI 18789 A1 Source Implementation | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | A1 source changes present for Service `18789`, control-ui auth, and proxy path |

## Source Scope Deployed

The deployed backend source includes:

- A1 Service `18789` source changes for OpenClaw Service `3001` plus `18789`.
- WS Auth Bridge source changes for control-ui WebSocket auth injection.
- OpenClaw short runtime hostname/label fix, where the tested decorated label `clawreef-9 (OpenClaw)` is `21` bytes and within the `63 bytes` runtime limit.

## Exact Commands Run

### Read-only rules and workflow discovery

```sh
sed -n '1,220p' AGENTS.md
sed -n '1,260p' .specify/memory/constitution.md
sed -n '1,220p' backend/AGENTS.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-plane-backend-build-deploy-approval-packet.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md
sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md
sed -n '1,180p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md
sed -n '1,180p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md
rg --files -g 'Dockerfile*' -g 'Makefile' -g '*.mk' -g 'scripts/**' -g 'docker-compose*.yml' -g 'docker-compose*.yaml'
sed -n '1,220p' Dockerfile
sed -n '1,260p' backend/Makefile
sed -n '1,240p' backend/deployments/docker/Dockerfile
sed -n '1,240p' backend/deployments/docker/Dockerfile.incluster
sed -n '1,220p' backend/deployments/docker/docker-compose.yml
sed -n '600,720p' deployments/k3s/clawmanager.yaml
sed -n '1,220p' deployments/container/start.sh
sed -n '1,220p' deployments/nginx/nginx.conf
```

### Required preflight tests

```sh
go test -count=1 ./internal/services ./internal/services/k8s ./internal/handlers
```

Result: exit `0`.

```text
ok  	clawreef/internal/services
ok  	clawreef/internal/services/k8s
ok  	clawreef/internal/handlers
```

### Live workload and rollback target preflight

```sh
kubectl get deploy -n clawmanager-system clawmanager-app -o json
kubectl get pods -n clawmanager-system -l app=clawmanager -o wide
kubectl get pods -n clawmanager-system -l app=clawmanager-app -o wide
kubectl get svc -n clawmanager-system
kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath='image=... generation=... revision=... replicas=...'
kubectl get rs -n clawmanager-system -l app=clawmanager-app -o jsonpath='...'
kubectl get pods -n clawmanager-system -l app=clawmanager-app -o jsonpath='...'
kubectl get pods -n clawmanager-user-1 -l app=clawreef -o jsonpath='...'
kubectl get pod -n clawmanager-user-1 clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -o jsonpath='...'
docker image inspect clawmanager:backend-proxy-icon-rewrite-20260502231228 --format '...'
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format '...'
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format '...'
curl -sk --max-time 10 --noproxy '*' -w 'healthz_http=%{http_code} err=%{errormsg}\n' -o /tmp/clawmanager-healthz-before.txt https://localhost:30443/healthz
```

### Backend build

```sh
STAMP="$(date +%Y%m%d%H%M%S)"
TAG="clawmanager:control-plane-backend-gtclaw-${STAMP}"
CTX="/tmp/clawmanager-control-plane-backend-gtclaw-${STAMP}"
mkdir -p "$CTX"
(
  cd backend
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o "$CTX/clawreef-server" ./cmd/server
)
cat > "$CTX/Dockerfile" <<EOF
FROM clawmanager:backend-proxy-icon-rewrite-20260502231228
COPY clawreef-server /usr/local/bin/clawreef-server
EOF
docker build --pull=false -t "$TAG" "$CTX"
docker image inspect "$TAG" --format '...'
```

Actual tag:

```text
clawmanager:control-plane-backend-gtclaw-20260505183733
```

### Backend deploy

```sh
k3d image import clawmanager:control-plane-backend-gtclaw-20260505183733 -c clawmanager
kubectl set image deployment/clawmanager-app -n clawmanager-system clawmanager-app=clawmanager:control-plane-backend-gtclaw-20260505183733
kubectl rollout status deployment/clawmanager-app -n clawmanager-system --timeout=240s
```

### Post-deploy smoke checks

```sh
kubectl rollout status deployment/clawmanager-app -n clawmanager-system --timeout=120s
kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath='image=... generation=... revision=... replicas=...'
kubectl get pods -n clawmanager-system -l app=clawmanager-app -o jsonpath='...'
kubectl get pods -n clawmanager-system -l app=clawmanager-app -o wide
kubectl get rs -n clawmanager-system -l app=clawmanager-app -o jsonpath='...'
kubectl get svc -n clawmanager-system
curl -sk --max-time 10 --noproxy '*' -w 'healthz_http=%{http_code} err=%{errormsg}\n' -o /tmp/clawmanager-healthz-after.txt https://localhost:30443/healthz
curl -sk --max-time 10 --noproxy '*' -w 'auth_me_http=%{http_code} err=%{errormsg}\n' -o /tmp/clawmanager-auth-me-noauth-after.txt https://localhost:30443/api/v1/auth/me
kubectl logs -n clawmanager-system deployment/clawmanager-app --tail=80
kubectl get pods -n clawmanager-user-1 -l app=clawreef -o jsonpath='...'
kubectl get pod -n clawmanager-user-1 clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 -o jsonpath='...'
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format '...'
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format '...'
```

## Preflight Results

### Backend tests

Required backend source tests passed before build/deploy:

```text
go test -count=1 ./internal/services ./internal/services/k8s ./internal/handlers
exit=0
```

### Workload identity

The exact workload was identified:

| Field | Value |
| --- | --- |
| workload | `Deployment/clawmanager-app` |
| namespace | `clawmanager-system` |
| container | `clawmanager-app` |
| pre-deploy image | `clawmanager:backend-proxy-icon-rewrite-20260502231228` |
| pre-deploy generation | `10` |
| pre-deploy revision | `10` |
| pre-deploy replicas | `1` |
| pre-deploy ready replicas | `1` |
| pre-deploy pod | `clawmanager-app-6c985497f5-f479h` |
| pre-deploy pod restart count | `2` |

The user-requested selector `app=clawmanager` returned no pods. The deployment's actual selector and pod labels use `app=clawmanager-app`, which was then used for workload-specific checks.

### Rollback target

Rollback target recorded before deploy:

```text
kubectl set image deployment/clawmanager-app -n clawmanager-system clawmanager-app=clawmanager:backend-proxy-icon-rewrite-20260502231228
```

Previous ReplicaSet:

```text
clawmanager-app-6c985497f5 revision=10 image=clawmanager:backend-proxy-icon-rewrite-20260502231228
```

### Runtime image digest preflight

Runtime image digest before deploy:

```text
sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Instance 9 runtime pod before deploy:

```text
clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724
imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
ready=true
restartCount=0
```

### Build/deploy workflow decision

The repo root `Dockerfile` rebuilds the frontend, so it was not used. The future gate approval forbids frontend rebuild unless separately justified.

The deployed image is a full control-plane image containing nginx, static frontend files, and `/usr/local/bin/clawreef-server`. To avoid frontend rebuild, this gate:

- built only the Linux ARM64 backend binary from `backend/cmd/server`;
- used the current live app image as the Docker base;
- replaced only `/usr/local/bin/clawreef-server`;
- did not modify any repo deployment manifest.

## Build Artifact Identity

| Field | Value |
| --- | --- |
| tag | `clawmanager:control-plane-backend-gtclaw-20260505183733` |
| local image id / digest | `sha256:51d68f2d370e970c3c32c020a50343cb6557cbe89f47cdea0e58cca6e2bba360` |
| repo digest | `clawmanager@sha256:51d68f2d370e970c3c32c020a50343cb6557cbe89f47cdea0e58cca6e2bba360` |
| platform | `linux/arm64` |
| base image | `clawmanager:backend-proxy-icon-rewrite-20260502231228` |
| base image id | `sha256:da171e815194a5228290cc10b8404113c08e6bc901db33d1dbd0fd49dfceff3f` |
| build context | `/tmp/clawmanager-control-plane-backend-gtclaw-20260505183733` |

Tag/publish details:

- `docker build --pull=false` was used.
- `k3d image import` imported only `clawmanager:control-plane-backend-gtclaw-20260505183733` into cluster `clawmanager`.
- No registry push was used.
- No runtime image tag was built, pushed, pulled, or changed.

## Deploy Result

Deploy target:

| Field | Value |
| --- | --- |
| workload | `Deployment/clawmanager-app` |
| namespace | `clawmanager-system` |
| container | `clawmanager-app` |
| deployed image | `clawmanager:control-plane-backend-gtclaw-20260505183733` |
| post-deploy generation | `11` |
| post-deploy observed generation | `11` |
| post-deploy revision | `11` |
| ready replicas | `1/1` |
| updated replicas | `1` |
| available replicas | `1` |
| post-deploy pod | `clawmanager-app-55d7847759-xn8kh` |
| post-deploy pod restart count | `0` |

Rollout:

```text
deployment "clawmanager-app" successfully rolled out
```

Post-deploy pod:

```text
clawmanager-app-55d7847759-xn8kh phase=Running ready=true restarts=0 image=docker.io/library/clawmanager:control-plane-backend-gtclaw-20260505183733 imageID=sha256:ac750b52206e6ca1ce70d63fef464137e49e1d00bd90cbd6ad6ea07e34d82670
```

## Post-deploy Smoke Results

| Check | Result |
| --- | --- |
| `kubectl rollout status deployment/clawmanager-app -n clawmanager-system --timeout=120s` | exit `0`; successfully rolled out |
| deployment readiness | generation `11`, observed generation `11`, ready replicas `1/1` |
| pod status | `Running`, ready `true`, restart count `0` |
| `/healthz` | HTTP `200`, body `ok` |
| `/api/v1/auth/me` without auth | HTTP `401`, body reported missing auth header; this proves backend API routing without exposing auth material |
| backend logs | new pod served runtime agent API calls and scheduled syncs; no crash loop seen |

## No Fresh Instance Proof

Runtime pod list before deploy:

```text
clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 phase=Running ready=desktop:true restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Runtime pod list after deploy:

```text
clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 phase=Running ready=desktop:true restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

No additional `app=clawreef` runtime pod appeared during this gate.

## Runtime Image Digest Unchanged

runtime image digest unchanged:

```text
sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Before and after, instance 9 still used:

```text
k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Host and in-cluster runtime image tags still resolved to the same digest:

```text
localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

## Blockers

None for this control-plane backend build/deploy gate.

This gate does not prove the fresh runtime listener behavior. That proof belongs to the next fresh-instance rerun gate.

## Recommendation

Recommended next step:

`Rerun Isolated 2Gi Fresh Instance Approval/Gate`

That gate should create a new isolated 2Gi fresh instance and prove the new instance Service exposes both `3001` and `18789`, `18789` is reachable on PodIP/ServiceIP, and the OpenClaw hostname/label no longer exceeds `63 bytes`.

## Explicit Negatives

- no fresh instance
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no runtime image rebuild
- no runtime image build/tag/push/pull/rebuild
- no frontend rebuild
- no frontend deploy
- no database migration
- no database write outside normal deployment health side effects
- no registry mutation unrelated to backend/control-plane deploy artifact
- no cluster/server/serverlb recovery
- no k3d cluster create/delete/start/stop
- no manual pod patch
- no manual Service patch
- no kubectl cp
- no deployment manifest repo modification
- no backend source modification
- no frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modification
- no cleanup/delete instance 9
- no token value, cookie value, credential, secret, or access URL plaintext
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
