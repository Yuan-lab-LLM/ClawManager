# OpenCode Lite v2 local upgrade

This overlay updates the existing managed Lite runtime to OpenCode 2.0.27.
It preserves the Runtime Agent, supervisor, per-instance ports and workspace
mounts. The current build targets linux/amd64 and uses the locally installed
runtime base image; it is not an independently published multi-architecture image.

## Source changes

- ClawManager recognizes both `/global/event` (v1) and `/api/event` (v2) as
  long-lived event streams.
- `AgentsRuntime/clawmanager-agent/internal/runtime/opencode/health.go` uses
  authenticated `/api/info` when `OPENCODE_RUNTIME_MAJOR=2`. It validates a
  successful JSON response with a v2 version and positive process ID. Error
  responses, redirects and HTML fallbacks fail readiness.
- The image exports `OPENCODE_RUNTIME_MAJOR=2`. The existing
  `start-opencode-web` entrypoint still runs `opencode serve` in the foreground.
- Existing v1 configuration is retained. Separate instance HOME/XDG directories
  and passwords continue to isolate configuration and application state.

## Build

The Runtime Agent health changes and `health_v2_test.go` are in the local
`D:\AgentsRuntime` checkout. From the ClawManager repository:

```powershell
.\deployments\opencode-lite-v2\build.ps1 -RuntimeSource D:\AgentsRuntime
docker build -f deployments/opencode-lite-v2/Dockerfile.clawmanager -t clawmanager-local:opencode-lite-v2-20261012 .
```

The build script verifies the official npm archive's SHA-512 checksum. Runtime
readiness tests and the relevant ClawManager tests run during image builds.
Keep a separate build context from the repository root for the runtime image.

## Applied local deployment

- ClawManager app: `clawmanager-local:opencode-lite-bootstrap-20261014`
- Local entry: `https://127.0.0.1:30443/`, served by
  `clawmanager-frontend` (NodePort 30443) and `clawmanager-app`.
- OpenCode public URL template:
  `https://opencode-{instance_id}.127-0-0-1.nip.io:30443/`.
- OpenCode Lite: `clawmanager-local/opencode-lite:2.0.27-20261012`
- Cluster/context: `docker-desktop`; namespace: `clawmanager-system`
- System image setting ID 5 (`opencode`, `gateway`) was updated to the new Lite
  image during the original upgrade; the browser fix leaves that setting alone.
- The existing `/api/v1/admin/runtime-rollouts` API performs the pool update.
- The Pro default image and Pro workloads were not changed.

The database had no instances and no active models before this update. No
existing OpenCode workspace required migration. A real Lite instance requires
an enabled model in AI Gateway; no dummy model was added to the system catalog.

## Verification

- OpenCode version in the deployed Lite runtime: `opencode v2.0.27`.
- The new pool registers as `ready`; its rollout completes as `finished`.
- Two isolated test gateways start through the Runtime Agent, expose only the
  ClawManager provider, stream events, and reject each other's passwords.
- Browser verification with upstream managed authentication opens the v2 UI
  without a pairing prompt or JavaScript errors.
- Real provider inference and old-session migration remain unverified: the
  local catalog and instance list were empty.

## Rollback

Before-deployment snapshots are retained under `.tmp/opencode-v2/`:
`runtime-before.json` and `app-before.json`. They can contain environment
configuration and must remain local.

To revert the Lite pool, set the Lite default image back to
`ghcr.io/yuan-lab-llm/agentsruntime/opencode-lite:latest`, then use the system's
Lite rolling-upgrade control with the previous actual runtime image:
`clawmanager:opencode-workspace-root-fix-20260825`. The compatible ClawManager
backend can stay installed because it supports v1 event streams as well.

If the app itself needs reverting, its previous image was
`clawmanager-local:egress-20261012-1441`. Once users create v2 instances, back up
their configuration and databases before reverting: image rollback alone does
not guarantee application-data compatibility.

## Browser state isolation (2026-10-13)

OpenCode remembers project entries and drafts in browser storage. Reusing an
instance origin can therefore display stale entries even when the new server
database has no sessions. The ClawManager proxy now inserts a storage bootstrap
before the OpenCode application starts. Local storage, session storage and
IndexedDB use a namespace derived from the owner, instance ID, creation time
and workspace path. Renaming an instance, restarting its runtime or refreshing
access credentials does not change that namespace. Recreating an instance ID
for a different creation time or workspace does.

The bootstrap leaves legacy and other-instance data in place but makes it
invisible to the current instance. Its `clear()` and database deletion calls
only affect its own namespace. The first load after this change initializes
fresh browser UI state; files and server-side sessions remain in their existing
workspaces. Dedicated-origin asset paths remain unchanged, and asset caching
continues normally. Instance-specific HTML is served with `Cache-Control:
no-store` so a cached upstream HTML response cannot skip the bootstrap.

This fix is in the ClawManager proxy; the OpenCode 2.0.27 Runtime image does not
need rebuilding. Build the app update on the previously deployed app image:

```powershell
docker build -f deployments/opencode-lite-v2/Dockerfile.clawmanager --build-arg APP_BASE_IMAGE=clawmanager-local:opencode-lite-v2-20261012 -t clawmanager-local:opencode-browser-isolation-20261013 .
```

Set `CLAWMANAGER_OPENCODE_PUBLIC_URL_TEMPLATE` on `clawmanager-app` to
`https://opencode-{instance_id}.127-0-0-1.nip.io:30443/` for this local
deployment. The access API must generate URLs on the same 30443 service as
the portal. Port 8443 belongs to the old `clawmanager-opencode-web-test`
deployment, which retains its original image
`clawmanager:opencode-web-only-local-20260918`.

Browser-fix deployment snapshots are retained locally in
`.tmp/opencode-browser-fix/app-before.json` and
`main-before-origin-correction.json`. To roll back only the browser storage
fix, restore the main app image to `clawmanager-local:opencode-lite-v2-20261012`.
Keep the corrected public URL port 30443; restoring a complete old deployment
snapshot would incorrectly send instance pages back to the old test service.

Backend tests cover instance recreation, stable identity across restarts,
early HTML injection and preservation of normal asset caching. The standalone
browser regression uses real Chromium storage APIs to verify that legacy data
is hidden, instance state is retained across refreshes, and clearing one
instance does not remove another instance's local storage or IndexedDB data:

```powershell
cd e2e
npm run test:opencode-storage
```

Install the existing e2e dependencies and Playwright browser first. For a local
installed Chrome, set `CLAW_MANAGER_CHROME_EXECUTABLE` to its executable path.

The deployed local instance-2 origin on port 30443 was verified in real Chrome: its server
HTML contains the bootstrap and `Cache-Control: no-store`, legacy storage is
hidden without deleting it, and the same namespace and stored state survive a
refresh. The OpenCode UI loads without JavaScript errors or failed requests.
Evidence is retained in `outputs/opencode-lite-v2/browser-storage-fix.png` and
`browser-storage-fix-verification.json`. The main instance detail page at
`https://127.0.0.1:30443/instances/2` also loads its embedded OpenCode UI from
the instance origin on 30443. Its screenshot and verification are retained as
`browser-storage-fix-portal-30443.png` and
`browser-storage-fix-portal-30443.json`. The existing Lite runtime Pod remains
on OpenCode 2.0.27.

## Default Lite project and first conversation (2026-10-14)

The existing Runtime Agent startup still creates the per-instance `project`
directory. The ClawManager proxy now initializes the first conversation on
the first browser visit to the Lite instance root and opens the native v2
session route. The initial title is `开始使用`. It uses a session ID derived
from the same instance creation identity as browser storage; OpenCode's own
database persists the conversation. Refreshing credentials, clearing browser
storage, or restarting the control plane does not generate another ID.

An instance that already contains an active top-level conversation reuses
that conversation instead of creating a starter. Workspace checks reject
sessions belonging to another instance. Concurrent visits within one service
share the same initialization request; concurrent replicas use the same ID
and recover an already committed session after a failed create response.
Explicit session URLs keep their native behavior. Archived starters remain
archived. OpenCode v1 continues to use its existing landing page.

The server entrypoint enables this behavior with
`WithOpenCodeLiteSessionBootstrap`. This is a control-plane change, so the
OpenCode Runtime image and the old 8443 test deployment need no update. Build
the main-service image using:

```powershell
docker build -f deployments/opencode-lite-v2/Dockerfile.clawmanager --build-arg APP_BASE_IMAGE=clawmanager-local:opencode-browser-isolation-20261013 -t clawmanager-local:opencode-lite-bootstrap-20261014 .
```

OpenCode service tests pass on Windows and in the Linux image build. Tests
cover repeated visits, a fresh control-plane service, concurrent replicas,
lost create responses, existing conversations across pagination, shared-pool
workspace isolation, archived conversations, v1 compatibility, and explicit
session navigation. Real Chrome verification on port 30443 covers the native
conversation input, simultaneous visits, reloads, cleared browser storage,
a fresh browser, and a main-service restart; the session count remains one.

Verification artifacts are `outputs/opencode-lite-v2/lite-bootstrap-30443.png`
and `lite-bootstrap-30443-verification.json`. A temporary model entry and Lite
instance were used because the local model catalog was empty; no model
inference was requested. The temporary conversation, instance, and model entry
were removed after verification. Creating a normal Lite instance still
requires a real enabled model in AI Gateway.

To roll back only this initialization change, restore the `clawmanager-app`
image to `clawmanager-local:opencode-browser-isolation-20261013`, keeping the
public-origin template on port 30443. The local pre-deployment snapshot is
`.tmp/opencode-bootstrap/app-before.json`; keep it private because it contains
environment configuration. Existing conversations remain in OpenCode.
