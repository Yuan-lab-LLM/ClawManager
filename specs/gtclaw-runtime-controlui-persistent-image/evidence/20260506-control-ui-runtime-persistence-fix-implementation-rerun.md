# Control UI Runtime Persistence Fix Implementation Rerun - 2026-05-06

Worker: ControlUIRuntimePersistenceFixImplementationRerunWorker

Verdict: `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_RERUN_DONE`

Not `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_RERUN_BLOCKED`: the recovered repo-owned control-ui artifact existed, the persisted gatewayUrl restore logic was located, a minimal patch was applied only to the recovered bundle JS, and syntax plus marker checks passed without build/deploy/browser work.

This gate modified only:

`specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js`

This gate wrote only this new evidence file besides the approved artifact modification. It did not modify backend, frontend GTManager source, runtime-startup-artifact, deployments, docs, longterm, AgentTeam, spec/plan/tasks, existing evidence, or `/tmp/gtclaw-runtime-patch/**`.

## Dependency Gate Record

| Dependency | Status used |
| --- | --- |
| Runtime Control UI Source Artifact Recovery Gate | `RUNTIME_CONTROL_UI_SOURCE_ARTIFACT_RECOVERY_DONE` |
| Control UI Runtime Persistence Fix Approval Packet | `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_APPROVAL_PACKET_DONE` |
| Control UI Stale Route Root Cause Investigation | `CONTROL_UI_STALE_ROUTE_ROOT_CAUSE_DONE` |
| Browser/Manual E2E Origin Allowlist Gate | `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_BLOCKED` |

Root cause used:

- A persisted gatewayUrl can override the current GTManager-mediated page-derived WebSocket URL.
- The observed failure shape was an instance `11` page route with a stale instance `10` WebSocket route in the control-ui form.
- The required mediated route shape is `instances/<id>/control-ui`, including chat paths under `/api/v1/instances/<id>/control-ui/chat?session=main`.

## Target File

Target file path:

`specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js`

No change was made to:

- `index.html`
- `assets/i18n-B06L7jQN.js`
- `assets/zh-CN-B26mMdbY.js`

Reason `index-M4TNVXB3.js` was sufficient:

- The stale-route investigation and recovered bundle inspection located the persisted gatewayUrl initialization/restore logic in this bundle.
- `index.html` only loads static assets.
- The i18n bundles do not own the startup precedence decision for `settings.gatewayUrl`.

## Before / After Metadata

| File | before size | after size | before sha256 | after sha256 |
| --- | ---: | ---: | --- | --- |
| `assets/index-M4TNVXB3.js` | `707959` | `708145` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |

Unmodified recovered files retained their recovery hashes:

| File | size | sha256 |
| --- | ---: | --- |
| `index.html` | `3398` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `assets/i18n-B06L7jQN.js` | `42617` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `assets/zh-CN-B26mMdbY.js` | `23255` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` |

## Exact Patch Method

The patch was an exact string replacement in the recovered repo-owned bundle:

1. Added helper marker after existing `function w_()` page URL derivation:
   - `function isGTManagerMediatedControlUiRoute()`
   - route test: `/^\/api\/v1\/instances\/[^/]+\/control-ui(?:\/|$)/`
2. Replaced the persisted gatewayUrl selection expression:
   - before marker: `l=c===e?n:c`
   - after marker: `l=isGTManagerMediatedControlUiRoute()?n:c===e?n:c`

Implementation note:

- An initial line-based `apply_patch` attempt did not apply because the target code lives inside a very long minified line.
- The final applied patch used exact substring replacement plus a bounded helper-function correction after marker validation detected a replacement interpolation issue.
- Final verification confirmed exactly one helper marker and exactly one patched selection marker.

## Relevant Code Marker Locations

Concise marker scan:

```text
29:function w_
29:isGTManagerMediatedControlUiRoute
29:function j_
29:l=isGTManagerMediatedControlUiRoute()?n:c===e?n:c
```

No long minified code excerpt is included in this evidence.

## Semantic Summary

Before this rerun, startup settings restore used the persisted `gatewayUrl` unless it exactly equaled the current page-derived URL. That allowed a stale stored instance `10` route to override an instance `11` GTManager-mediated page.

After this rerun:

- If `location.pathname` is under `/api/v1/instances/<id>/control-ui`, including `/api/v1/instances/<id>/control-ui/chat`, the page-derived WebSocket URL is authoritative.
- On that GTManager-mediated route, persisted gatewayUrl no longer overrides the active route.
- An instance `10` persisted URL cannot be selected as the active gatewayUrl for an instance `11` mediated control-ui page by this restore expression.
- Outside the mediated route, the existing persisted gatewayUrl behavior is preserved.

## Standalone Gateway Compatibility Assessment

Standalone gateway compatibility is preserved by gating the change on the route path. Non-mediated paths such as `/control-ui/chat`, `/api/v1/instances/<id>/proxy/`, and similar non-control-ui mediated paths continue to use the previous selection rule:

```text
persisted gatewayUrl equals page URL ? effective page URL : persisted gatewayUrl
```

This means standalone gateway users can still restore their persisted gatewayUrl outside GTManager-mediated `/api/v1/instances/<id>/control-ui` pages.

## Why No Storage Cleanup / Manual URL / Token Is Required

- No localStorage cleanup is required because the bundle now ignores stale persisted gatewayUrl for GTManager-mediated control-ui pages.
- No sessionStorage cleanup is required because the active route is selected before the session token lookup for the selected route key.
- No IndexedDB/cache/cookies cleanup is required or used.
- No manual URL edit is required because the page-derived WebSocket URL becomes authoritative on mediated routes.
- No gateway token, password, or key entry is required because this patch does not change backend bridge server-side token injection semantics and does not expose the OpenClaw gateway token.

## Tests And Checks Run

Root-cause and marker checks:

- Located startup restore logic around `function j_()` and page URL derivation around `function w_()`.
- Confirmed pre-edit RED behavior with a Node check: the old selection logic would keep stale persisted gatewayUrl on `/api/v1/instances/11/control-ui/chat`.
- Confirmed route guard cases:
  - `/api/v1/instances/11/control-ui` -> mediated.
  - `/api/v1/instances/11/control-ui/` -> mediated.
  - `/api/v1/instances/11/control-ui/chat` -> mediated.
  - `/control-ui/chat` -> standalone / non-mediated.
  - `/api/v1/instances/11/proxy/` -> non-mediated.
  - `/api/v1/instances/11/control-uis` -> non-mediated.
- Confirmed post-edit GREEN behavior with a Node check:
  - mediated route uses page-derived WebSocket URL.
  - standalone gateway route keeps persisted gatewayUrl behavior.
- Confirmed marker counts:
  - `isGTManagerMediatedControlUiRoute` helper marker count: `1`.
  - patched restore expression marker count: `1`.
  - old restore expression marker count: `0`.
- Ran `node --check` on the modified bundle: exit `0`.

## Explicit Negatives

- no backend modification
- no frontend GTManager source modification
- no runtime-startup-artifact modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no copied artifact file modification outside `assets/index-M4TNVXB3.js`
- no build/tag/push/pull
- no backend deploy/restart
- no fresh instance create/delete/modify
- no K8S mutation
- no runtime mutation
- no database mutation
- no registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no localStorage cleanup
- no sessionStorage cleanup
- no IndexedDB cleanup
- no cache cleanup
- no cookies cleanup
- no storage cleanup
- no manual URL
- no manual WebSocket URL edit
- no gateway token
- no gateway token/password/key entry
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Recommended Next Gate

Recommended next gate:

`Runtime Image Build/Tag/Push Approval Packet`

This rerun does not authorize image build/tag/push/pull, deploy, fresh instance work, runtime/K8S/database/registry mutation, browser E2E, `passes:true`, Close, longterm write-back, or git stage/commit/push.

## Verification Plan

Required checks for this implementation rerun:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-implementation-rerun.md
shasum -a 256 and wc -c for modified artifact files
node --check specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js
rg marker check for mediated route / persisted gatewayUrl patch markers
rg -n "CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_RERUN_DONE|CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_RERUN_BLOCKED|persisted gatewayUrl|page-derived WebSocket URL|GTManager-mediated|instances/<id>/control-ui|localStorage|sessionStorage|standalone gateway|before sha256|after sha256|no manual URL|no gateway token|no storage cleanup|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-implementation-rerun.md
secret-shape scan with matched values suppressed over modified artifact + evidence
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-implementation-rerun.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source/assets/index-M4TNVXB3.js specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-implementation-rerun.md` | `0` | No whitespace errors. |
| before/after `shasum -a 256` and `wc -c` | `0` | Before reference `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` / `707959`; after artifact `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` / `708145`. |
| `node --check` on modified `assets/index-M4TNVXB3.js` | `0` | Syntax validation accepted the modified browser ESM bundle. |
| artifact marker `rg` scan | `0` | Found `isGTManagerMediatedControlUiRoute`, mediated `/api/v1/instances` / `control-ui` marker, patched selection expression, and existing `gatewayUrl` markers. |
| helper and selection count check | `0` | Helper marker count `1`; patched selection count `1`; old selection count `0`. |
| behavior check | `0` | Mediated `/api/v1/instances/11/control-ui` and `/chat` paths choose page-derived WebSocket URL; standalone/non-mediated paths keep persisted gatewayUrl behavior. |
| required evidence marker `rg` scan | `0` | Required markers found, including rerun DONE/BLOCKED verdicts, `persisted gatewayUrl`, `page-derived WebSocket URL`, `GTManager-mediated`, `instances/<id>/control-ui`, `localStorage`, `sessionStorage`, `standalone gateway`, `before sha256`, `after sha256`, `no manual URL`, `no gateway token`, `no storage cleanup`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed over modified artifact + evidence | `0` | `secret_shape_match_count=24`; matched values were suppressed. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260506-persistence-fix-source specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-implementation-rerun.md` | `0` | Shows the recovered artifact root and this new evidence file as untracked in the requested path scope. |
