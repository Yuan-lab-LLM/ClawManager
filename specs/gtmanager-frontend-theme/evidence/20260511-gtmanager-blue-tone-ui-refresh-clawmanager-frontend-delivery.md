# GTManager blue tone ClawManager frontend delivery evidence

Date: 2026-05-11

Gate: `GTMANAGER_BLUE_TONE_UI_REFRESH_CLAWMANAGER_FRONTEND_DELIVERY_GATE`

Dependency gates:

- `GTMANAGER_BLUE_TONE_UI_REFRESH_IMPLEMENTATION_DONE`
- `GTMANAGER_BLUE_TONE_UI_REFRESH_VISUAL_REVIEW_DONE`

Verdict: `GTMANAGER_BLUE_TONE_UI_REFRESH_CLAWMANAGER_FRONTEND_DELIVERY_DONE`

## Scope

This delivery only built and deployed the ClawManager / GTManager application image from the current worktree frontend changes.

No GTClaw/OpenClaw runtime image, runtime artifact, runtime instance, or instance internal Control UI was entered, modified, built, pushed, deployed, created, stopped, or deleted.

## Branch

Command:

```text
git branch --show-current
```

Result:

```text
dev
```

## Dependency evidence

Found:

- `specs/gtmanager-frontend-theme/evidence/20260511-gtmanager-blue-tone-ui-refresh.md`
  - `Verdict: GTMANAGER_BLUE_TONE_UI_REFRESH_IMPLEMENTATION_DONE`
- `specs/gtmanager-frontend-theme/evidence/20260511-gtmanager-blue-tone-ui-refresh-visual-review.md`
  - `Verdict: GTMANAGER_BLUE_TONE_UI_REFRESH_VISUAL_REVIEW_DONE`

## Frontend build

Command:

```text
cd frontend && npm run build
```

Result:

```text
Exit 0
vite v8.0.0 building client environment for production...
131 modules transformed.
dist/assets/index-DpT26CJF.css
dist/assets/index-BCHzMogy.js
built in 674ms
Warning: Some chunks are larger than 500 kB after minification.
```

## Image build and push

Image tag:

```text
TAG=gtmanager-blue-tone-20260511104246
HOST_IMAGE=localhost:5001/clawmanager/clawmanager:gtmanager-blue-tone-20260511104246
CLUSTER_IMAGE=k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtmanager-blue-tone-20260511104246
```

Build:

```text
docker build -t "$HOST_IMAGE" .
```

Result:

```text
Exit 0
exporting manifest list sha256:0771f1ef1bdefc055382ed634ac8fa9d67b0e0f4699d6bfc87ce55af70935a3a
naming to localhost:5001/clawmanager/clawmanager:gtmanager-blue-tone-20260511104246
```

Push:

```text
docker push "$HOST_IMAGE"
```

Result:

```text
Exit 0
gtmanager-blue-tone-20260511104246: digest: sha256:0771f1ef1bdefc055382ed634ac8fa9d67b0e0f4699d6bfc87ce55af70935a3a size: 856
```

Local image inspect:

```text
image_id=sha256:0771f1ef1bdefc055382ed634ac8fa9d67b0e0f4699d6bfc87ce55af70935a3a
repo_digests=["localhost:5001/clawmanager/clawmanager@sha256:0771f1ef1bdefc055382ed634ac8fa9d67b0e0f4699d6bfc87ce55af70935a3a"]
repo_tags=["localhost:5001/clawmanager/clawmanager:gtmanager-blue-tone-20260511104246"]
```

## Rollout

The suggested container name `clawmanager` did not exist in the live deployment. Read-only inspection showed the actual container name and selector are both `clawmanager-app`.

Corrected delivery command:

```text
kubectl -n clawmanager-system set image deployment/clawmanager-app clawmanager-app="$CLUSTER_IMAGE"
kubectl -n clawmanager-system rollout status deployment/clawmanager-app --timeout=180s
```

Result:

```text
deployment.apps/clawmanager-app image updated
deployment "clawmanager-app" successfully rolled out
```

Current deployment state:

```text
image=k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtmanager-blue-tone-20260511104246
ready=1/1
updated=1
available=1
```

Current pod:

```text
pod/clawmanager-app-66dbbcd5b4-8bfx7
READY 1/1
STATUS Running
RESTARTS 0
IP 10.42.0.132
NODE k3d-clawmanager-server-0
```

## HTTPS entry and asset delivery

Root request:

```text
curl -k -sS -D /tmp/gtmanager-blue-tone-root.headers https://localhost:30443/ -o /tmp/gtmanager-blue-tone-root.html
```

Result:

```text
HTTP/2 200
server: nginx
content-type: text/html
content-length: 462
```

The delivered HTML references the new build assets:

```text
/assets/index-BCHzMogy.js
/assets/index-DpT26CJF.css
```

Served asset hash comparison:

```text
e0f6253a1b4208396b70db24c78ceaee1a99067a9a4c954fd6e3226531c2f4f0  frontend/dist/assets/index-DpT26CJF.css
e0f6253a1b4208396b70db24c78ceaee1a99067a9a4c954fd6e3226531c2f4f0  /tmp/gtmanager-blue-tone-served.css
969a65565291fc54e450470e0d5bbb0f8c615c340e1f54b485ed8c7d567e0e88  frontend/dist/assets/index-BCHzMogy.js
969a65565291fc54e450470e0d5bbb0f8c615c340e1f54b485ed8c7d567e0e88  /tmp/gtmanager-blue-tone-served.js
```

## Browser verification

Browser method:

- Browser plugin Node REPL was not exposed in this session after tool discovery.
- Fallback used local Playwright browser automation against `https://localhost:30443`.
- A temporary isolated browser context was used.
- A short-lived local authenticated browser state was used only in memory for management route rendering and was not recorded in this evidence.
- No secret value, session value, request header value, or complete privileged link is recorded here.

Screenshots:

```text
/tmp/gtmanager-blue-tone-delivery-browser/login.png
/tmp/gtmanager-blue-tone-delivery-browser/admin-dashboard.png
/tmp/gtmanager-blue-tone-delivery-browser/admin-instances.png
/tmp/gtmanager-blue-tone-delivery-browser/admin-risk-rules.png
/tmp/gtmanager-blue-tone-delivery-browser/user-instances.png
/tmp/gtmanager-blue-tone-delivery-browser/results.json
```

Observed browser styles:

```text
Login primary button:
linear-gradient(135deg, rgb(59, 130, 246) 0%, rgb(29, 78, 216) 100%)

Login input focus:
borderColor=rgb(107, 149, 237)
boxShadow includes rgba(96, 165, 250, 0.12)

Admin sidebar selected nav:
color=rgb(29, 78, 216)
background=rgb(219, 234, 254)
boxShadow includes rgba(147, 197, 253, 0.82) and rgba(37, 99, 235, 0.45)

User sidebar selected nav:
color=rgb(29, 78, 216)
background=rgb(219, 234, 254)
```

Semantic colors preserved:

```text
redPreserved=true
amberPreserved=true
greenPreserved=true
```

Representative samples:

```text
Red/delete sample:
text=删除
color=rgb(185, 28, 28)
backgroundColor=rgb(254, 226, 226)

Amber/warning sample:
text=安全路由动作31
backgroundColor=rgb(255, 251, 235)
borderColor=rgb(253, 230, 138)

Green/running sample:
text=已连接
color=rgb(22, 101, 52)
backgroundColor=rgb(220, 252, 231)
```

## Non-actions

- No source file was modified by this delivery gate.
- No backend/auth/database/migration file was modified.
- No `specs/gtclaw-runtime-controlui-persistent-image/**` file was modified.
- No GTClaw/OpenClaw runtime image was built, tagged, pushed, or deployed.
- No runtime instance internal Control UI was entered or modified.
- No runtime instance was created, stopped, deleted, or otherwise mutated.
- No old evidence/session/PVC/image/browser cache cleanup was performed.
- No git stage, commit, or push was executed.
- No Mem0/longterm write was executed.
- No `passes:true` or Close action was executed.
