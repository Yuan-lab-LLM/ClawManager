# GTManager Deployed Logo Root Cause & Patch — 20260510

Gate: `GTMANAGER_DEPLOYED_LOGO_NOT_RENDERING_ROOT_CAUSE_AND_PATCH_GATE`
Symptom: After full deploy, `https://localhost:30443/` admin sidebar shows only the
text “管理后台 / GTManager”; the official GTManager logo image does not render at
the brand area at the top of the sidebar.

Verdict: **GTMANAGER_DEPLOYED_LOGO_NOT_RENDERING_DELIVERY_GAP_BLOCKED**
Reason: source + local `frontend/dist` already contain the correct logo and the
correct components, but the running pod is serving an older container image
whose embedded `/usr/share/nginx/html/gtmanager-logo.png` is a stale,
3099-byte file with a different sha256. No source-side patch is required;
making the deployed UI render the new logo requires a fresh image
build/push/rollout, which this gate explicitly forbids.

---

## 1. Source-side state (workspace)

### 1.1 Components actually used at the brand area

- `frontend/src/components/AdminLayout.tsx:5` imports `BrandLogo`.
- `frontend/src/components/AdminLayout.tsx:65,150` renders `<BrandLogo .../>`
  inside both the mobile topbar (line 65) and the desktop sidebar brand block
  (line 150). The desktop block (lines 142–161) is the area visible in the
  user's screenshot.
- `frontend/src/components/UserLayout.tsx:5,59,144` mirrors the same usage for
  user-side layouts.
- `frontend/src/components/BrandLogo.tsx:14` requests `/gtmanager-logo.png`
  and falls back to a `GT` text badge only when the `<img>` element fires
  `onError`.

→ Source-side wiring is correct. Both layouts use `BrandLogo`; the brand
section is not just plain text.

### 1.2 Source assets

```
file frontend/public/gtmanager-logo.png frontend/public/gtmanager-logo.svg
  frontend/public/gtmanager-logo.png: PNG image data, 115 x 120, 8-bit/color RGBA, non-interlaced
  frontend/public/gtmanager-logo.svg: SVG Scalable Vector Graphics image

shasum -a 256 frontend/public/gtmanager-logo.{png,svg}
  cc9c3fa0b396b3f1756f6e8be16019cf893161c9ec525e04228414f452fdc9c2  frontend/public/gtmanager-logo.png
  af13cf5839585e691fd2320d9fc01d49e852888cf9a84e574e8d884b6e265cc2  frontend/public/gtmanager-logo.svg

wc -c frontend/public/gtmanager-logo.{png,svg}
  6114  frontend/public/gtmanager-logo.png
  1120  frontend/public/gtmanager-logo.svg
```

Both files are valid images. PNG is 115×120 RGBA, 6114 bytes.

### 1.3 Local build output (`frontend/dist`)

`frontend/dist` from the most recent local build (mtime `5 10 19:53`) contains
identical assets:

```
shasum -a 256 frontend/dist/gtmanager-logo.{png,svg}
  cc9c3fa0b396b3f1756f6e8be16019cf893161c9ec525e04228414f452fdc9c2  frontend/dist/gtmanager-logo.png
  af13cf5839585e691fd2320d9fc01d49e852888cf9a84e574e8d884b6e265cc2  frontend/dist/gtmanager-logo.svg
```

Hashes match the source `frontend/public/` byte-for-byte. A fresh `npm run build`
step was therefore not re-executed in this gate (the existing dist is already
authoritative for the local source).

### 1.4 Image / nginx wiring

- `Dockerfile:30`: `COPY --from=frontend-builder /app/frontend/dist /usr/share/nginx/html`
- `deployments/nginx/nginx.conf:38`: `root /usr/share/nginx/html;`
- `deployments/nginx/nginx.conf:59-61`: `location / { try_files $uri $uri/ /index.html; }`

Static asset path `/gtmanager-logo.png` therefore resolves to
`/usr/share/nginx/html/gtmanager-logo.png` inside the container. Wiring is
correct; no nginx/base-path change needed.

---

## 2. Runtime evidence (deployed pod)

### 2.1 Pod / image identity

```
kubectl -n clawmanager-system get pods -l app=clawmanager
NAME                               READY   STATUS    RESTARTS   AGE
clawmanager-app-8599bd6548-8p89f   1/1     Running   0          2d4h

Image:    k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-diagnostic-backend-20260508174053
Image ID: sha256:63084136990d16d1536006260bfe09a910231aef69d5102948d186bc80a3369b
Started:  Fri, 08 May 2026 17:59:45 +0800
```

The running pod was started on **2026-05-08 17:59 CST**, ~2 days before this
gate runs. The image tag `gtclaw-diagnostic-backend-20260508174053` is
explicitly a 2026-05-08 build.

### 2.2 Static files inside the running container

```
kubectl -n clawmanager-system exec deploy/clawmanager-app -- \
  sh -lc 'ls -la /usr/share/nginx/html/gtmanager-logo.*'
-rw-r--r-- 1 root root 3099 May  8 03:54 /usr/share/nginx/html/gtmanager-logo.png
```

- Only the PNG exists in the live image (`gtmanager-logo.svg` is **not**
  shipped in the deployed image — it was added after the 5/8 build).
- File mtime is `May 8 03:54`, matching the image build time.
- Size is **3099 bytes**, not 6114.

```
kubectl -n clawmanager-system exec deploy/clawmanager-app -- \
  sh -lc 'sha256sum /usr/share/nginx/html/gtmanager-logo.png'
0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0  /usr/share/nginx/html/gtmanager-logo.png
```

→ Pod's PNG sha256 = `0d738a03…` (3099 B). Local source/dist sha256 =
`cc9c3fa0…` (6114 B). **Different file content.**

### 2.3 Pod's bundled JS still references the logo correctly

```
kubectl -n clawmanager-system exec deploy/clawmanager-app -- \
  sh -lc 'grep -o "gtmanager-logo" /usr/share/nginx/html/assets/index-*.js | wc -l'
# (4 hits inside index-Cce3cBw3.js, including BrandLogo render sites)
```

So the deployed bundle does include `BrandLogo` and emits an `<img
src="/gtmanager-logo.png" />`. The image area is *being requested*; the
fallback `GT` badge would only be visible if the request 404'd or errored.

### 2.4 HTTP fetch through the live entrypoint

```
curl -k -sS -D /tmp/h https://localhost:30443/gtmanager-logo.png -o /tmp/p
cat /tmp/h
HTTP/2 200
server: nginx
date: Sun, 10 May 2026 14:37:54 GMT
content-type: image/png
content-length: 3099
last-modified: Fri, 08 May 2026 03:54:01 GMT
etag: "69fd5e59-c1b"
accept-ranges: bytes

file /tmp/p
/tmp/p: PNG image data, 115 x 120, 8-bit/color RGBA, non-interlaced

shasum -a 256 /tmp/p
0d738a0371f3fac37bd71ff21d0027bc46958d98e5f5ff1c3e2bc36e93f8e7c0  /tmp/p

wc -c /tmp/p
3099 /tmp/p
```

The browser definitely receives **a real PNG** (200, valid PNG header,
115×120) — so `onError` is not firing, and the fallback “GT” badge is not the
explanation. What it receives is the **old logo file** (3099 B, sha
`0d738a03…`), not the current source's 6114 B `cc9c3fa0…` PNG. If that old
PNG visually does not look like the official GTManager logo (e.g. blank /
near-blank / placeholder), the user perceives "no logo, only text".

---

## 3. Root cause

The *deployed* container image
`k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-diagnostic-backend-20260508174053`
was built on 2026-05-08 from an earlier state of `frontend/public/`. Since
then, the local repo has updated the official logo asset
(`frontend/public/gtmanager-logo.png` with sha `cc9c3fa0…`, 6114 B) and added
`gtmanager-logo.svg` plus the `BrandLogo` component, but **no new image has
been built or rolled out**.

Therefore the running pod still serves the previous, byte-different
`gtmanager-logo.png` from inside its container's `/usr/share/nginx/html`
layer. nginx, paths, base href, MIME, caching, and `BrandLogo` source code
are all functioning correctly.

This is a delivery / image-rollout gap, not a frontend bug. No source-side
fix can make `https://localhost:30443/` render the new logo without a fresh
image build + image push to `k3d-clawmanager-registry:5000` + pod rollout —
all of which are explicitly out of scope for this gate.

---

## 4. Files changed in this gate

None. Source already contains the correct logo asset and component wiring.
Modifying any of the allow-listed files (`BrandLogo.tsx`, `AdminLayout.tsx`,
`UserLayout.tsx`, `frontend/public/gtmanager-logo.{png,svg}`,
`frontend/index.html`, `Dockerfile`) would not change what the pod serves,
because the pod runs a frozen image layer.

```
git status --short -- frontend/public/gtmanager-logo.png \
  frontend/public/gtmanager-logo.svg frontend/src/components/BrandLogo.tsx \
  frontend/src/components/UserLayout.tsx frontend/src/components/AdminLayout.tsx \
  frontend/index.html Dockerfile deployments/nginx/nginx.conf
 M frontend/public/gtmanager-logo.png
 M frontend/src/components/AdminLayout.tsx
 M frontend/src/components/UserLayout.tsx
?? frontend/public/gtmanager-logo.svg
?? frontend/src/components/BrandLogo.tsx
```

These are pre-existing local changes from prior gates (not introduced by this
investigation). `git diff --check` on those paths reports clean.

---

## 5. Validation summary

| Check                                              | Expected                  | Observed                                | Result |
|----------------------------------------------------|---------------------------|-----------------------------------------|--------|
| Source PNG valid                                   | PNG, non-zero             | PNG 115×120, 6114 B                     | PASS   |
| Source SVG valid                                   | SVG                       | SVG, 1120 B                             | PASS   |
| AdminLayout uses BrandLogo                         | yes                       | yes (lines 5, 65, 150)                  | PASS   |
| UserLayout uses BrandLogo                          | yes                       | yes (lines 5, 59, 144)                  | PASS   |
| BrandLogo points at `/gtmanager-logo.png`          | yes                       | yes (line 14)                           | PASS   |
| `frontend/dist` contains logo                      | yes, hash == source       | yes, sha matches                        | PASS   |
| Dockerfile copies `dist` → nginx html              | yes                       | yes (line 30)                           | PASS   |
| nginx serves `/` from `/usr/share/nginx/html`      | yes                       | yes (conf line 38)                      | PASS   |
| `https://localhost:30443/gtmanager-logo.png` 200   | 200 + image/png           | 200 + image/png                         | PASS   |
| Live PNG sha256 == source sha256                   | match                     | mismatch (0d738a03 vs cc9c3fa0)         | **FAIL → root cause** |
| Live PNG size == source size                       | 6114                      | 3099                                    | **FAIL → root cause** |
| Live SVG present                                   | yes                       | absent in pod                           | **FAIL → root cause** |
| Pod image timestamp                                | recent build              | 2026-05-08 build                        | stale  |

---

## 6. Delivery action required (out of scope here)

To actually render the new logo at `https://localhost:30443/`, a follow-up
delivery gate must:

1. Build a new container image from current `HEAD` (which has the new
   `gtmanager-logo.png`/`.svg` and `BrandLogo`).
2. Push it to `k3d-clawmanager-registry:5000/clawmanager/clawmanager:<new-tag>`.
3. Roll the `clawmanager-app` deployment in `clawmanager-system` to that tag.

Browser cache may also keep the old PNG for one user session because the
asset is served from a stable URL (`/gtmanager-logo.png`) without a content
hash; a hard refresh will be needed after rollout. **No cache or asset
purging is performed in this gate.**

---

## 7. Compliance statement

- No `git add`, `git commit`, `git push`, or staging performed.
- No image build, tag, push, or registry mutation performed.
- No `kubectl apply`, `rollout`, `scale`, `delete`, or any cluster mutation
  performed; all `kubectl` calls were read-only (`get`, `describe`, `exec
  ls/sha256sum/cat/grep`).
- No backend, auth, database, or runtime code touched.
- No browser cache, PVC, or session cleared.
- No Mem0 / longterm memory written.
- No global rebrand or rename of OpenClaw / GTClaw / OpenSparrow technical
  identifiers performed.
- No tokens, cookies, or authorization headers recorded in this evidence.
