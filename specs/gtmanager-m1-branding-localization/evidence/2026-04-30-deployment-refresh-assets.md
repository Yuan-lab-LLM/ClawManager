# Deployment Refresh Assets Evidence - GTManager M1

Date: 2026-04-30T12:07:22+08:00

Worker: DeploymentRefreshWorker

Scope: refresh local K3S runtime candidate image for `clawmanager-app` only. No source, manifest, docs, longterm, task/spec/plan, feature-list, or close/write-back changes are authorized in this worker scope.

Important scope caveat: this deployment candidate is the cumulative M1 candidate, including Phase A + Frontend B + Docs D + Assets C. It is not an Assets C-only worktree.

PNG caveat known before refresh: AssetsReviewWorker reported browser decode OK for `frontend/public/gtmanager-logo.png` at 115x120, while strict decoders still warn/fail. This refresh records deployed browser-load evidence rather than treating the strict-decoder warning as resolved.

## Candidate Image Tag

```text
clawmanager:m1-gtmanager-assets-local-20260430120731
```

## Before State

### `git status --short` (exit 0)

```text
 M .specify/memory/constitution.md
 M AGENTS.md
 M README.md
 M docs/k3s-local-setup.md
 M docs/manual-skill-import.md
 M docs/manual-skill-import_en.md
 M frontend/index.html
 M frontend/src/components/AdminLayout.tsx
 M frontend/src/components/InstanceAccess.tsx
 M frontend/src/components/OpenClawDesktopOverlay.tsx
 M frontend/src/components/UserLayout.tsx
 M frontend/src/contexts/I18nContext.tsx
 M frontend/src/lib/i18n.ts
 M frontend/src/pages/instances/CreateInstancePage.tsx
 M frontend/src/stores/authStore.ts
 M frontend/src/types/instance.ts
 M longterm/CHECKLIST.md
 M longterm/workspace/app_spec.md
 M longterm/workspace/init.sh
?? backend/AGENTS.md
?? clawmanagerArm/
?? dist/
?? frontend/AGENTS.md
?? frontend/public/gtmanager-logo.png
?? specs/
```

### Dirty Paths Summary (exit 0)

```text
   1 .specify
   1 AGENTS.md
   1 README.md
   1 backend
   1 clawmanagerArm
   1 dist
   3 docs
  12 frontend
   3 longterm
   1 specs
```

Interpretation: the dirty tree is cumulative M1 candidate state plus local generated/excluded artifacts. This log does not claim an Assets C-only diff.

### Current Deployment Image (exit 0)

```text
clawmanager:m1-gtmanager-local-20260430102258
```

### Current Pods (exit 0)

```text
NAME                              READY   STATUS    RESTARTS   AGE   IP           NODE                       NOMINATED NODE   READINESS GATES
clawmanager-app-fdd4dccdd-xsfj4   1/1     Running   0          98m   10.42.0.43   k3d-clawmanager-server-0   <none>           <none>
```

## Commands and Results

| Command | Exit | Evidence |
| --- | ---: | --- |
| `date +m1-gtmanager-assets-local-%Y%m%d%H%M%S` | 0 | Produced `m1-gtmanager-assets-local-20260430120731`. |
| `DOCKER_BUILDKIT=0 docker build -f Dockerfile -t clawmanager:m1-gtmanager-assets-local-20260430120731 .` | 0 | Legacy builder succeeded. Frontend production build generated `dist/assets/index-BcCrhHyr.js`; image tagged successfully as `clawmanager:m1-gtmanager-assets-local-20260430120731`. |
| `k3d image import clawmanager:m1-gtmanager-assets-local-20260430120731 -c clawmanager` | 0 | Imported 1 image into k3d cluster `clawmanager`. |
| `kubectl set image -n clawmanager-system deployment/clawmanager-app clawmanager-app=clawmanager:m1-gtmanager-assets-local-20260430120731` | 0 | Updated only `deployment/clawmanager-app`. |
| `kubectl rollout status -n clawmanager-system deployment/clawmanager-app --timeout=240s` | 0 | `deployment "clawmanager-app" successfully rolled out`. |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o wide` | 0 | See after state. |
| `kubectl get pods -n clawmanager-system -l app=clawmanager-app -o wide` | 0 | See after state. |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath='{.spec.template.spec.containers[?(@.name=="clawmanager-app")].image}{"\n"}'` | 0 | Returned `clawmanager:m1-gtmanager-assets-local-20260430120731`. |
| `kubectl get svc,endpoints -n clawmanager-system clawmanager-gateway` | 0 | Gateway endpoints point to new pod IP `10.42.0.44`. |
| `kubectl logs -n clawmanager-system deploy/clawmanager-app --tail=80` | 0 | Logs showed server startup, Gin routes, sync loop, and successful agent heartbeat/command polling. No token or secret value was recorded. |
| `curl -sk https://localhost:30443/healthz` | 0 | Returned `ok`. |
| `curl -sk https://localhost:30443/login \| head -40` | 0 | HTML showed `lang="zh"`, favicon `/gtmanager-logo.png`, title `GTManager`, JS `/assets/index-BcCrhHyr.js`. |
| `curl -sk https://localhost:30443/login -o /tmp/gtmanager-assets-login.html` | 0 | Saved deployed login HTML for asset path extraction. |
| `rg -o '/assets/[^"[:space:]]+\.js' /tmp/gtmanager-assets-login.html` | 0 | Returned `/assets/index-BcCrhHyr.js`. |
| `curl -sk https://localhost:30443/assets/index-BcCrhHyr.js -o /tmp/gtmanager-assets-app.js` | 0 | Saved deployed JS bundle. |
| `grep -q 'GTManager' /tmp/gtmanager-assets-app.js` | 0 | `GTManager` present. |
| `grep -q 'ClawManager' /tmp/gtmanager-assets-app.js` | 1 | `ClawManager` absent from deployed JS. |
| `grep -q 'clawmanager_locale' /tmp/gtmanager-assets-app.js` | 0 | Protected technical/localStorage key present. |
| `grep -q '登录 GTManager' /tmp/gtmanager-assets-app.js` | 0 | Chinese GTManager login copy present. |
| `grep -q 'Sign in to GTManager' /tmp/gtmanager-assets-app.js` | 0 | English GTManager login copy present. |
| `grep -q 'Sign in to ClawManager' /tmp/gtmanager-assets-app.js` | 1 | Old product-facing login copy absent. |
| `curl -skI https://localhost:30443/gtmanager-logo.png` | 0 | HTTP/2 200, `content-type: image/png`, `content-length: 3099`. |
| `curl -sk https://localhost:30443/gtmanager-logo.png -o /tmp/gtmanager-logo-deployed.png` | 0 | Saved deployed logo PNG. |
| `file /tmp/gtmanager-logo-deployed.png` | 0 | PNG image data, 115 x 120, 8-bit/color RGBA, non-interlaced. |
| `shasum -a 256 /tmp/gtmanager-logo-deployed.png` | 0 | `0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0`. |
| `node --input-type=module -e "...playwright chromium load logo..."` from `/tmp/clawmanager-e2e-rerun.lusuW7` | 0 | Browser loaded `https://localhost:30443/gtmanager-logo.png` with status 200, content type `image/png`, complete image, natural size 115 x 120. |

## After State

### Deployment (exit 0)

```text
NAME              READY   UP-TO-DATE   AVAILABLE   AGE   CONTAINERS        IMAGES                                                 SELECTOR
clawmanager-app   1/1     1            1           14d   clawmanager-app   clawmanager:m1-gtmanager-assets-local-20260430120731   app=clawmanager-app
```

### Pods (exit 0)

```text
NAME                              READY   STATUS    RESTARTS   AGE   IP           NODE                       NOMINATED NODE   READINESS GATES
clawmanager-app-8558899f4-jv4w7   1/1     Running   0          30s   10.42.0.44   k3d-clawmanager-server-0   <none>           <none>
```

### Deployment Image (exit 0)

```text
clawmanager:m1-gtmanager-assets-local-20260430120731
```

### Gateway Service and Endpoints (exit 0)

```text
NAME                          TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)             AGE
service/clawmanager-gateway   ClusterIP   10.43.244.223   <none>        8443/TCP,9001/TCP   14d

NAME                            ENDPOINTS                         AGE
endpoints/clawmanager-gateway   10.42.0.44:9001,10.42.0.44:8443   14d
```

### Log Tail Summary (exit 0)

```text
2026/04/30 04:09:07 Server starting on :9001
[GIN-debug] Listening and serving HTTP on :9001
[SyncService] Starting sync loop with interval 5s
[SyncService] Initial sync complete
[GIN] 2026/04/30 - 04:09:22 | 200 |  13.27ms |      10.42.0.42 | POST     "/api/v1/agent/heartbeat"
[GIN] 2026/04/30 - 04:09:28 | 200 |   3.56ms |      10.42.0.42 | GET      "/api/v1/agent/commands/next"
```

## Smoke Checks

### Health

```text
curl -sk https://localhost:30443/healthz
ok
```

### Login HTML

`curl -sk https://localhost:30443/login | head -40` showed:

```html
<html lang="zh">
...
<link rel="icon" type="image/png" href="/gtmanager-logo.png" />
...
<title>GTManager</title>
<script type="module" crossorigin src="/assets/index-BcCrhHyr.js"></script>
```

### Bundle Evidence

Deployed JS path: `/assets/index-BcCrhHyr.js`

Deployed JS size:

```text
904001 /tmp/gtmanager-assets-app.js
```

| Check | Exit | Result |
| --- | ---: | --- |
| `GTManager` | 0 | Present |
| `ClawManager` | 1 | Absent from deployed JS |
| `clawmanager_locale` | 0 | Present as protected technical/localStorage key |
| `登录 GTManager` | 0 | Present |
| `Sign in to GTManager` | 0 | Present |
| `Sign in to ClawManager` | 1 | Absent |

### Deployed Logo HTTP and File Evidence

`curl -skI https://localhost:30443/gtmanager-logo.png`:

```text
HTTP/2 200
server: nginx
content-type: image/png
content-length: 3099
```

Downloaded deployed logo:

```text
/tmp/gtmanager-logo-deployed.png: PNG image data, 115 x 120, 8-bit/color RGBA, non-interlaced
0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0  /tmp/gtmanager-logo-deployed.png
```

### Browser Logo Load

Playwright/Chromium result:

```json
{
  "status": 200,
  "contentType": "image/png",
  "imageCount": 1,
  "complete": true,
  "naturalWidth": 115,
  "naturalHeight": 120,
  "currentSrc": "https://localhost:30443/gtmanager-logo.png"
}
```

Browser can load deployed logo: yes.

Strict-decoder caveat: still open from AssetsReviewWorker. Prior strict decoders warned/failed on the PNG even though macOS `file`/`sips` and Chrome headless can load it. This deployment refresh did not rewrite image bytes and does not claim strict-decoder remediation.

## Deployment Refresh Verdict

Deployment refresh succeeded for the local K3S cumulative M1 candidate image.

Before image:

```text
clawmanager:m1-gtmanager-local-20260430102258
```

After image:

```text
clawmanager:m1-gtmanager-assets-local-20260430120731
```

Final EvidenceVerifier E2E can run against `https://localhost:30443` for the cumulative M1 candidate with Assets C deployed.

This log records deployment refresh evidence only. It does not authorize final write-back, feature-list mutation, or closing the gate.
