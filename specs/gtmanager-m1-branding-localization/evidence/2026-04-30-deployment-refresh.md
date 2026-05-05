# Deployment Refresh Evidence - GTManager M1

Date: 2026-04-30T10:29:45+08:00

Worker: DeploymentRefreshWorker

Scope: refresh local K3S runtime candidate image for `clawmanager-app` only. No source, manifest, docs, longterm, task/spec/plan, or `passes:true` changes were made.

## Image Tag

```text
clawmanager:m1-gtmanager-local-20260430102258
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
?? specs/
```

### Deployment (exit 0)

```text
NAME              READY   UP-TO-DATE   AVAILABLE   AGE   CONTAINERS        IMAGES                                                               SELECTOR
clawmanager-app   1/1     1            1           13d   clawmanager-app   docker.io/library/clawmanager:dev-skilllive-overlay-20260416144525   app=clawmanager-app
```

### Pods (exit 0)

```text
NAME                               READY   STATUS    RESTARTS        AGE     IP           NODE                       NOMINATED NODE   READINESS GATES
clawmanager-app-7985f576c9-z66hd   1/1     Running   2 (2d10h ago)   2d10h   10.42.0.28   k3d-clawmanager-server-0   <none>           <none>
```

### Deployment Image (exit 0)

```text
docker.io/library/clawmanager:dev-skilllive-overlay-20260416144525
```

### Before Bundle Evidence

`curl -sk https://localhost:30443/login -o /tmp/gtmanager-before-login.html` exited 0.

`head -40 /tmp/gtmanager-before-login.html` showed:

```html
<html lang="en">
...
<title>ClawManager</title>
<script type="module" crossorigin src="/assets/index-5ikfSU1Q.js"></script>
```

`curl -sk https://localhost:30443/assets/index-5ikfSU1Q.js -o /tmp/gtmanager-before-app.js` exited 0.

Before JS grep checks:

| Check | Command | Exit |
| --- | --- | ---: |
| GTManager absent | `grep -q 'GTManager' /tmp/gtmanager-before-app.js` | 1 |
| ClawManager present | `grep -q 'ClawManager' /tmp/gtmanager-before-app.js` | 0 |
| locale storage key present | `grep -q 'clawmanager_locale' /tmp/gtmanager-before-app.js` | 0 |
| Chinese GTManager login absent | `grep -q '登录 GTManager' /tmp/gtmanager-before-app.js` | 1 |
| English GTManager login absent | `grep -q 'Sign in to GTManager' /tmp/gtmanager-before-app.js` | 1 |
| Old English ClawManager login present | `grep -q 'Sign in to ClawManager' /tmp/gtmanager-before-app.js` | 0 |

## Commands Run

| Command | Exit | Notes |
| --- | ---: | --- |
| `date +m1-gtmanager-local-%Y%m%d%H%M%S` | 0 | Produced `m1-gtmanager-local-20260430102258`. |
| `docker build -f Dockerfile -t clawmanager:m1-gtmanager-local-20260430102258 .` | 1 | Failed before source build: Docker Hub oauth token timeout. |
| `docker build --pull=false -f Dockerfile -t clawmanager:m1-gtmanager-local-20260430102258 .` | 1 | Same Docker Hub oauth token timeout. |
| `DOCKER_BUILDKIT=0 docker build -f Dockerfile -t clawmanager:m1-gtmanager-local-20260430102258 .` | 0 | Legacy builder succeeded. Frontend build generated `dist/assets/index-yeoXoo5V.js`; image tagged successfully. |
| `k3d image import clawmanager:m1-gtmanager-local-20260430102258 -c clawmanager` | 0 | Imported 1 image into cluster `clawmanager`. |
| `kubectl set image -n clawmanager-system deployment/clawmanager-app clawmanager-app=clawmanager:m1-gtmanager-local-20260430102258` | 0 | Updated only `clawmanager-app`. |
| `kubectl rollout status -n clawmanager-system deployment/clawmanager-app --timeout=240s` | 0 | Rollout succeeded. |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o wide` | 0 | See after state. |
| `kubectl get pods -n clawmanager-system -l app=clawmanager-app -o wide` | 0 | See after state. |
| `kubectl get svc,endpoints -n clawmanager-system clawmanager-gateway` | 0 | See after state. |
| `kubectl logs -n clawmanager-system deploy/clawmanager-app --tail=80` | 0 | Logs showed server startup, Gin routes, sync loop, and successful agent command polling; no secret/token values were recorded here. |
| `kubectl get deploy -n clawmanager-system clawmanager-app -o jsonpath='{.spec.template.spec.containers[?(@.name=="clawmanager-app")].image}{"\n"}'` | 0 | Returned new image tag. |
| `curl -sk https://localhost:30443/healthz` | 0 | Returned `ok`. |
| `curl -sk https://localhost:30443/login \| head -40` | 0 | Showed `lang="zh"`, `title` `GTManager`, JS `/assets/index-yeoXoo5V.js`. |
| `curl -sk https://localhost:30443/login -o /tmp/gtmanager-login.html` | 0 | Saved post-refresh login HTML. |
| `rg -o '/assets/[^"[:space:]]+\.js' /tmp/gtmanager-login.html` | 0 | Returned `/assets/index-yeoXoo5V.js`. |
| `curl -sk https://localhost:30443/assets/index-yeoXoo5V.js -o /tmp/gtmanager-app.js` | 0 | Saved post-refresh deployed JS. |
| `grep -q 'GTManager' /tmp/gtmanager-app.js` | 0 | GTManager present. |
| `grep -q 'ClawManager' /tmp/gtmanager-app.js` | 1 | ClawManager absent from deployed JS. |
| `grep -q 'clawmanager_locale' /tmp/gtmanager-app.js` | 0 | Protected storage key still present. |
| `grep -q '登录 GTManager' /tmp/gtmanager-app.js` | 0 | Chinese login copy present. |
| `grep -q 'Sign in to GTManager' /tmp/gtmanager-app.js` | 0 | English GTManager login copy present. |
| `grep -q 'Sign in to ClawManager' /tmp/gtmanager-app.js` | 1 | Old product-facing login copy absent. |

## After State

### Deployment (exit 0)

```text
NAME              READY   UP-TO-DATE   AVAILABLE   AGE   CONTAINERS        IMAGES                                          SELECTOR
clawmanager-app   1/1     1            1           13d   clawmanager-app   clawmanager:m1-gtmanager-local-20260430102258   app=clawmanager-app
```

### Pods (exit 0)

```text
NAME                              READY   STATUS    RESTARTS   AGE   IP           NODE                       NOMINATED NODE   READINESS GATES
clawmanager-app-fdd4dccdd-xsfj4   1/1     Running   0          23s   10.42.0.43   k3d-clawmanager-server-0   <none>           <none>
```

### Gateway Service and Endpoints (exit 0)

```text
NAME                          TYPE        CLUSTER-IP      EXTERNAL-IP   PORT(S)             AGE
service/clawmanager-gateway   ClusterIP   10.43.244.223   <none>        8443/TCP,9001/TCP   13d

NAME                            ENDPOINTS                         AGE
endpoints/clawmanager-gateway   10.42.0.43:9001,10.42.0.43:8443   13d
```

### Deployment Image (exit 0)

```text
clawmanager:m1-gtmanager-local-20260430102258
```

### After Bundle Evidence

`curl -sk https://localhost:30443/login | head -40` showed:

```html
<html lang="zh">
...
<title>GTManager</title>
<script type="module" crossorigin src="/assets/index-yeoXoo5V.js"></script>
```

Post-refresh deployed JS:

```text
/tmp/gtmanager-app.js size: 904021 bytes
```

Post-refresh JS grep checks:

| Check | Exit | Result |
| --- | ---: | --- |
| `GTManager` | 0 | Present |
| `ClawManager` | 1 | Absent |
| `clawmanager_locale` | 0 | Present as protected technical/localStorage key |
| `登录 GTManager` | 0 | Present |
| `Sign in to GTManager` | 0 | Present |
| `Sign in to ClawManager` | 1 | Absent |

## Smoke Result

```text
curl -sk https://localhost:30443/healthz
ok
```

- Healthz result: `ok`
- Rollout result: `deployment "clawmanager-app" successfully rolled out`
- Deployed JS now contains `GTManager`: yes
- Deployed JS still contains product-facing `ClawManager`: no match found in deployed JS
- Old product-facing `Sign in to ClawManager`: no match found in deployed JS

## Blockers

No deployment-refresh blocker remains from the local K3S runtime candidate refresh.

Residual gates remain outside this worker scope:

- This log does not mark `passes:true`.
- This log does not Close M1.
- EvidenceVerifier should rerun E2E against the refreshed deployment before any acceptance/pass/Close decision.
