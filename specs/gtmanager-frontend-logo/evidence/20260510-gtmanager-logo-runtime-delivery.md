# GTManager Frontend Logo Runtime Delivery Evidence

Date: 2026-05-10

Gate: `GTMANAGER_FRONTEND_LOGO_RUNTIME_DELIVERY_GATE`

Approval phrase received: `APPROVE_GTMANAGER_FRONTEND_LOGO_RUNTIME_DELIVERY_GATE`

Verdict: `GTMANAGER_FRONTEND_LOGO_RUNTIME_DELIVERY_DONE`

## Scope

- Built the current worktree into a new ClawManager image.
- Pushed the image to the local k3d registry repository.
- Updated only `deployment/clawmanager-app` in namespace `clawmanager-system`.
- Waited for rollout completion.
- Verified logo static resource delivery and admin sidebar rendering.
- No source files were edited by this gate.
- No git stage, commit, git push, passes:true, Close, database changes, auth login, runtime changes, OpenClaw/GTClaw/OpenSparrow technical naming changes, old image cleanup, PVC cleanup, session cleanup, or browser cache cleanup were performed.

## Image

Tag:

```text
gtmanager-logo-runtime-20260510231558
```

Cluster image reference:

```text
k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtmanager-logo-runtime-20260510231558
```

Local registry push note:

```text
docker push k3d-clawmanager-registry:5000/... failed from the host because the cluster-side hostname is not resolvable on macOS.
The same repository/tag was pushed through the host-published local registry endpoint localhost:5001.
The deployment uses the cluster-side image reference k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtmanager-logo-runtime-20260510231558.
```

Push digest:

```text
sha256:450188eda244c71d2cd82f83d8ae0679b23f41e682f7db2863a263150ac0680d
```

Local image asset hash before push:

```text
cc9c3fa0b396b3f1756f6e8be16019cf893161c9ec525e04228414f452fdc9c2  /usr/share/nginx/html/gtmanager-logo.png
```

## Rollout

Previous deployment image:

```text
k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-diagnostic-backend-20260508174053
```

Rollout command result:

```text
deployment.apps/clawmanager-app image updated
deployment "clawmanager-app" successfully rolled out
```

Deployment image and readiness:

```text
k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtmanager-logo-runtime-20260510231558
1/1 ready
```

Requested pod selector result:

```text
kubectl -n clawmanager-system get pod -l app=clawmanager -o wide
No resources found in clawmanager-system namespace.
```

Actual app pod selector result:

```text
NAME                               READY   STATUS    RESTARTS   AGE   IP            NODE                       NOMINATED NODE   READINESS GATES
clawmanager-app-7f99fd5785-fcpfm   1/1     Running   0          37s   10.42.0.129   k3d-clawmanager-server-0   <none>           <none>
```

Actual labels:

```text
clawmanager-app-7f99fd5785-fcpfm   app=clawmanager-app,pod-template-hash=7f99fd5785
```

## Hashes

Source public PNG:

```text
cc9c3fa0b396b3f1756f6e8be16019cf893161c9ec525e04228414f452fdc9c2  frontend/public/gtmanager-logo.png
```

Local dist PNG:

```text
cc9c3fa0b396b3f1756f6e8be16019cf893161c9ec525e04228414f452fdc9c2  frontend/dist/gtmanager-logo.png
```

Pod PNG:

```text
cc9c3fa0b396b3f1756f6e8be16019cf893161c9ec525e04228414f452fdc9c2  /usr/share/nginx/html/gtmanager-logo.png
-rw-r--r--    1 root     root          6114 May 10 11:53 /usr/share/nginx/html/gtmanager-logo.png
```

Pod SVG presence:

```text
-rw-r--r--    1 root     root          1120 May 10 11:53 /usr/share/nginx/html/gtmanager-logo.svg
```

HTTPS static resource:

```text
HTTP/2 200
server: nginx
content-type: image/png
content-length: 6114
```

Downloaded HTTPS PNG:

```text
/tmp/gtmanager-logo.png: PNG image data, 115 x 120, 8-bit/color RGBA, non-interlaced
cc9c3fa0b396b3f1756f6e8be16019cf893161c9ec525e04228414f452fdc9c2  /tmp/gtmanager-logo.png
pixelWidth: 115
pixelHeight: 120
6114 bytes
```

## Admin Sidebar Render

Browser plugin Node REPL was not exposed in this session, and Computer Use returned a macOS Apple event permission error. Fallback render verification used temporary Playwright CLI state against `https://localhost:30443/admin`.

To avoid writing auth state through `/api/v1/auth/login`, no login request was made. The render check used a temporary locally signed admin access token stored only in `/tmp/gtmanager-admin-storage-state.json`.

Command result:

```text
Navigating to https://localhost:30443/admin
Waiting for selector aside img[src="/gtmanager-logo.png"]...
Capturing screenshot into /tmp/gtmanager-admin-sidebar.png
```

Visual result:

```text
Admin desktop sidebar brand area rendered GTManager with /gtmanager-logo.png visible.
No broken-image icon was observed in the sidebar brand area.
```

## BrandLogo Fallback

`frontend/src/components/BrandLogo.tsx` still contains the fallback span path for image load errors. This gate did not change it.

## Git Checks

`git diff --check`:

```text
exit 0, no output
```

`git status --short`:

```text
 M frontend/public/gtmanager-logo.png
 M frontend/src/components/AdminLayout.tsx
 M frontend/src/components/UserLayout.tsx
?? frontend/public/gtmanager-logo.svg
?? frontend/src/components/BrandLogo.tsx
?? specs/gtmanager-frontend-logo/
?? tmp-gtmanager-logo-diag-summary.html
```

These worktree changes were present as delivery inputs for this runtime gate; this gate did not stage, commit, push git, or modify source files.
