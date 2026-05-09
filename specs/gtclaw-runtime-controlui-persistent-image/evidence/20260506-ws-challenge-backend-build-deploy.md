# WS Challenge Backend Build/Deploy Gate - 2026-05-06

Worker: WSChallengeBackendBuildDeployWorker

Verdict: `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_DONE`

Not `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_BLOCKED`: the approved backend-only
source fix was tested, built into a control-plane backend derivative image,
imported into the local k3d cluster, deployed only to
`Deployment/clawmanager-app`, rolled out successfully, and passed the approved
no-auth smoke checks.

## Dependency Gates

| Gate | Evidence | Status used |
| --- | --- | --- |
| Backend Build/Deploy Approval Packet | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-backend-build-deploy-approval-packet.md` | `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_APPROVAL_PACKET_DONE`; user approved `APPROVE_WS_CHALLENGE_BACKEND_BUILD_DEPLOY_GATE`. |
| Root Cause | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-ws-challenge-root-cause.md` | `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_DONE`. |
| Bridge Implementation | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation.md` | `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_DONE`. |
| Safety Hardening | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-safety-hardening.md` | `WS_CHALLENGE_BRIDGE_SAFETY_HARDENING_DONE`. |
| Review | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-review.md` | `WS_CHALLENGE_BRIDGE_REVIEW_DONE`. |

## Tests

Required backend tests ran before build/deploy:

```text
go test -count=1 ./internal/services ./internal/handlers
exit 0
ok      clawreef/internal/services    1.041s
ok      clawreef/internal/handlers    0.607s
```

## Build/Deploy Target

| Field | Value |
| --- | --- |
| workload | `Deployment/clawmanager-app` |
| namespace | `clawmanager-system` |
| container | `clawmanager-app` |
| old backend image | `clawmanager:control-plane-backend-gtclaw-20260505183733` |
| old deployed pod | `clawmanager-app-55d7847759-xn8kh` |
| old deployed pod ready/restarts | `ready=true`, `restarts=0` |
| old deployment generation/revision | `generation=11`, `revision=11` |
| new backend image | `clawmanager:control-plane-backend-ws-challenge-20260506091557` |
| new backend image digest | `clawmanager@sha256:0901270113c188be7f47e56bd23e04f6989a48d609836e34a3f3477ecbc02ecf` |
| new backend image platform | `linux/arm64` |

## Exact Build Commands

The repo root `Dockerfile` rebuilds frontend assets, so it was not used. The
existing backend-only deploy path was reused: compile only the backend server
binary, use the old live control-plane image as base, and replace only
`/usr/local/bin/clawreef-server`.

```sh
set -euo pipefail
STAMP="$(date +%Y%m%d%H%M%S)"
OLD_IMAGE="clawmanager:control-plane-backend-gtclaw-20260505183733"
TAG="clawmanager:control-plane-backend-ws-challenge-${STAMP}"
CTX="/tmp/clawmanager-ws-challenge-backend-${STAMP}"
mkdir -p "$CTX"
(
  cd backend
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o "$CTX/clawreef-server" ./cmd/server
)
printf 'FROM %s\nCOPY clawreef-server /usr/local/bin/clawreef-server\n' "$OLD_IMAGE" > "$CTX/Dockerfile"
docker build --pull=false -t "$TAG" "$CTX"
docker image inspect "$TAG" --format 'tag={{index .RepoTags 0}} id={{.Id}} repoDigests={{json .RepoDigests}} os={{.Os}} arch={{.Architecture}} created={{.Created}}'
printf 'TAG=%s\nCTX=%s\nOLD_IMAGE=%s\n' "$TAG" "$CTX" "$OLD_IMAGE" > /tmp/ws-challenge-backend-build-deploy.env
cat /tmp/ws-challenge-backend-build-deploy.env
```

Build result:

```text
TAG=clawmanager:control-plane-backend-ws-challenge-20260506091557
CTX=/tmp/clawmanager-ws-challenge-backend-20260506091557
OLD_IMAGE=clawmanager:control-plane-backend-gtclaw-20260505183733
tag=clawmanager:control-plane-backend-ws-challenge-20260506091557 id=sha256:0901270113c188be7f47e56bd23e04f6989a48d609836e34a3f3477ecbc02ecf repoDigests=["clawmanager@sha256:0901270113c188be7f47e56bd23e04f6989a48d609836e34a3f3477ecbc02ecf"] os=linux arch=arm64 created=2026-05-06T01:16:02.813719013Z
```

## Exact Deploy Commands

`k3d image import` was required by the existing local k3d backend deploy path
so the newly built backend-only image was available inside cluster node
containerd. This imported only the backend/control-plane image and did not push
to a registry.

```sh
set -euo pipefail
. /tmp/ws-challenge-backend-build-deploy.env
k3d image import "$TAG" -c clawmanager
kubectl set image deployment/clawmanager-app -n clawmanager-system clawmanager-app="$TAG"
kubectl rollout status deployment/clawmanager-app -n clawmanager-system --timeout=240s
```

Deploy output summary:

```text
Successfully imported 1 image(s) into 1 cluster(s)
deployment.apps/clawmanager-app image updated
deployment "clawmanager-app" successfully rolled out
```

## Rollout Result

| Field | Value |
| --- | --- |
| rollout | `deployment "clawmanager-app" successfully rolled out` |
| image | `clawmanager:control-plane-backend-ws-challenge-20260506091557` |
| generation | `12` |
| observed generation | `12` |
| revision | `12` |
| replicas | `1` |
| ready replicas | `1` |
| updated replicas | `1` |
| available replicas | `1` |

New pod:

```text
clawmanager-app-656f5b479d-d7c47 phase=Running ready=true restarts=0 image=docker.io/library/clawmanager:control-plane-backend-ws-challenge-20260506091557 imageID=sha256:851ee5c0d356707e3088954c32ebfe098eb4365400636cbcfa3c1d495134fff6
```

ReplicaSet evidence:

```text
clawmanager-app-55d7847759 revision=11 replicas=0 image=clawmanager:control-plane-backend-gtclaw-20260505183733
clawmanager-app-656f5b479d revision=12 replicas=1 ready=1 image=clawmanager:control-plane-backend-ws-challenge-20260506091557
```

## Smoke Checks

| Check | Result |
| --- | --- |
| `https://localhost:30443/healthz` | HTTP `200`, body `ok` |
| `https://localhost:30443/api/v1/auth/me` no-auth | HTTP `401`, body `{"error":"Authorization header required","success":false}` |

Exact smoke commands:

```sh
curl -sk --max-time 10 --noproxy '*' -w 'healthz_http=%{http_code} err=%{errormsg}\n' -o /tmp/clawmanager-ws-challenge-healthz-after.txt https://localhost:30443/healthz
curl -sk --max-time 10 --noproxy '*' -w 'auth_me_http=%{http_code} err=%{errormsg}\n' -o /tmp/clawmanager-ws-challenge-auth-me-noauth-after.txt https://localhost:30443/api/v1/auth/me
```

Smoke output:

```text
healthz_http=200 err=
healthz_body=ok
auth_me_http=401 err=
auth_me_body={"error":"Authorization header required","success":false}
```

## Runtime Image Drift Check

Runtime image digest before deploy:

```text
sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Runtime pods before deploy:

```text
clawmanager-user-1/clawreef-10-oc2gi-185707 phase=Running ready=desktop:true restarts=0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 phase=Running ready=desktop:true restarts=0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Runtime pods after deploy:

```text
clawmanager-user-1/clawreef-10-oc2gi-185707 phase=Running ready=desktop:true restarts=0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 phase=Running ready=desktop:true restarts=0 image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

runtime image digest unchanged / no drift:

```text
sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

No fresh instance was created: the `app=clawreef` runtime pod list before and
after deploy contained the same two pods, including instance `10`
`clawreef-10-oc2gi-185707`.

## Explicit Negatives

- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no fresh instance
- no fresh instance creation/deletion/modification
- no runtime image rebuild
- no runtime image build/tag/push/pull
- no runtime registry mutation
- no database write/migration
- no database migration
- no manual pod patch
- no manual Service patch
- no kubectl cp
- no frontend changes
- no runtime source/artifact changes
- no deployment manifest changes
- no docs changes
- no longterm changes
- no AgentTeam changes
- no spec/plan/tasks changes
- no existing evidence modification
- no git stage/commit/push
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close

## Recommended Next Gate

Recommended next gate: Browser/Manual E2E Approval Packet.

That packet should request separate authorization to verify instance `10`
control-ui through the browser/manual path and check no manual connect form, no
`invalid connect params`, and no `device.nonce` error.

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-backend-build-deploy.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_DONE`, `WS_CHALLENGE_BACKEND_BUILD_DEPLOY_BLOCKED`, `Deployment/clawmanager-app`, `rollout`, `healthz`, `auth/me`, `old backend image`, `new backend image`, `runtime image digest`, `no browser E2E`, `no fresh instance`, `no runtime image rebuild`, `no database`, `no kubectl cp`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence | `0` | No matches. Matched values would have been suppressed. |
| path-limited `git status --short` | `0` | Shows `backend/internal/services/instance_proxy_service.go` and `backend/internal/services/instance_proxy_service_test.go` modified from prior source gates, and this build/deploy evidence file untracked. |
