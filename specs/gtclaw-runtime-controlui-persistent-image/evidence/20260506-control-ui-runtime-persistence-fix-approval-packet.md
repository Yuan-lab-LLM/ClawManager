# Control UI Runtime Persistence Fix Approval Packet - 2026-05-06

Worker: ControlUIRuntimePersistenceFixApprovalPacketWorker

Verdict: `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_APPROVAL_PACKET_DONE`

Not `CONTROL_UI_RUNTIME_PERSISTENCE_FIX_APPROVAL_PACKET_BLOCKED`: the prerequisite Control UI Stale Route Root Cause Investigation is complete, and the evidence is sufficient to request approval for a narrow future runtime control-ui persistence fix implementation gate.

This packet is an approval request only. It performed no implementation, no build/tag/push/pull, no deploy, no backend restart, no fresh instance creation/deletion/modification, no K8S/runtime/database/registry mutation, no browser E2E, no browser storage/cache/cookie cleanup, no manual URL edit, no gateway token/password entry, no manual pod/service patch, no `kubectl cp`, no Mem0 write, no longterm write-back, no `passes:true`, no Close, and no git stage/commit/push.

## Approval Request

Please approve or reject whether a future worker may execute:

`Control UI Runtime Persistence Fix Implementation Gate`

Recommended response options:

- `APPROVE_CONTROL_UI_RUNTIME_PERSISTENCE_FIX_IMPLEMENTATION_GATE`: authorize the future implementation gate with only the scope and prohibitions below.
- `REJECT_OR_BLOCK`: do not implement this runtime control-ui persistence fix; provide the blocking concern or revised scope.

No approval is implied by this packet. The future implementation gate must not start unless the user explicitly approves it.

## Dependency Gate Record

| Dependency | Status used |
| --- | --- |
| Control UI Stale Route Root Cause Investigation | `CONTROL_UI_STALE_ROUTE_ROOT_CAUSE_DONE` |
| Browser/Manual E2E Origin Allowlist Gate | `BROWSER_MANUAL_E2E_ORIGIN_ALLOWLIST_BLOCKED` |
| Fresh Instance Runtime Deployment Origin Allowlist Gate | `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_DONE` |
| Runtime image build/tag/push origin allowlist gate | `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE` |
| Runtime startup artifact origin allowlist implementation rerun | `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_RERUN_DONE` |

Root cause used by this packet:

- GTManager opens the correct instance `11` page route.
- The expected mediated route shape is `/api/v1/instances/11/control-ui`.
- The failed page still displayed a stale form WebSocket URL shaped as `/api/v1/instances/10/control-ui`.
- Static runtime bundle review found that a persisted gatewayUrl can override the current page-derived WebSocket URL.
- Therefore the current blocker is a control-ui bundle persisted gatewayUrl precedence bug, not a need for the user to type a URL, key, token, or password.

## Future Implementation Target

The future implementation should make the GTManager-mediated `/api/v1/instances/<id>/control-ui` page route authoritative for the active WebSocket URL.

Required semantics:

- On a GTManager-mediated route, the current page-derived WebSocket URL is the authoritative URL.
- A persisted gatewayUrl must not override the current mediated route.
- The browser should not need a manual URL, key, token, or password entry.
- The fix must not expose the OpenClaw gateway token.
- The fix must preserve non-GTManager / standalone gateway use cases unless evidence explicitly records a reviewed tradeoff.
- The fix must not change backend WebSocket bridge server-side token injection semantics.

Acceptable future implementation directions include one of the following, subject to evidence:

| Option | Summary | Required care |
| --- | --- | --- |
| Ignore persisted gatewayUrl on mediated routes | When the page is under `/api/v1/instances/<id>/control-ui`, always initialize active gateway URL from the current page-derived WebSocket URL. | Preserve standalone gateway persistence behavior outside mediated routes. |
| Isolate persisted settings by normalized current route | Persist settings under a key scoped to the normalized current route or instance route so instance `10` cannot override instance `11`. | Prove route normalization cannot collide across instances and does not leak token-bearing values. |
| Reset on route mismatch | If persisted gatewayUrl route does not match the current page-derived route, reset the active gatewayUrl to the page-derived WebSocket URL. | Record mismatch handling without clearing unrelated localStorage or sessionStorage entries. |

## Future Scope Recommendation

The future implementation should be limited to runtime control-ui bundle/source artifact only.

Recommended source priority:

1. Prefer an approved or recovered runtime control-ui source artifact if available for this bundle.
2. Use `/tmp/gtclaw-runtime-patch/**` only as read-only reference unless a future approval explicitly names a writable copy.
3. If source-level repair is not available and a minified bundle patch is required, the implementation evidence must record:
   - before hash, after hash, before size, and after size
   - exact target path
   - reason source-level repair was not available
   - exact patch method
   - exact route/persistence behavior changed

The future implementation evidence should be the only repository write besides the approved runtime control-ui artifact change. It must not modify backend, frontend GTManager, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence.

## Future Implementation Must Preserve

| Requirement | Preservation rule |
| --- | --- |
| no manual URL | The browser user must not need to hand-edit the WebSocket URL. |
| no gateway token | The user must not enter or see the OpenClaw gateway token, password, or key. |
| GTManager-mediated route | `/api/v1/instances/<id>/control-ui` remains the mediated route shape for browser access. |
| standalone gateway | Non-GTManager / standalone gateway behavior remains compatible unless later evidence explicitly justifies a scoped tradeoff. |
| backend bridge | Backend WS bridge server-side token injection semantics remain unchanged. |
| storage hygiene | The fix must not clear localStorage, sessionStorage, cache, or cookies as the repair mechanism. |

## Future Gate Allowed Scope

If approved, the future Control UI Runtime Persistence Fix Implementation Gate may only:

- inspect the relevant runtime control-ui bundle/source artifact needed to implement this precedence fix
- edit only the approved runtime control-ui source/bundle artifact path required for this fix
- write one new implementation evidence file for that gate
- record before/after hashes, sizes, paths, exact patch method, and semantic summary
- run file-level validation commands that do not build, deploy, mutate runtime state, or open a browser
- run secret-shape scans with matched values suppressed

The future gate must stop and write a blocked evidence packet if it cannot repair the persisted gatewayUrl behavior without expanding into backend, frontend GTManager, deployment, runtime environment, browser, K8S, database, registry, or storage-cleanup scope.

## Future Gate Explicitly Forbidden

Approval of this packet must not authorize:

- clearing localStorage as a fix
- clearing sessionStorage as a fix
- clearing browser cache or cookies as a fix
- requiring the user to manually change the WebSocket URL
- requiring the user to input a gateway token, key, or password
- backend Origin rewrite
- backend access/proxy unrelated changes
- frontend GTManager unrelated changes
- build/tag/push/pull
- backend deploy/restart
- fresh instance creation/deletion/modification
- K8S/runtime/database/registry mutation
- browser E2E
- Chrome DevTools MCP
- Playwright
- manual pod patch
- manual Service patch
- `kubectl cp`
- secrets/token/cookie/access URL plaintext output
- Mem0 write
- longterm write-back
- `passes:true`
- Close
- git stage/commit/push

Required shorthand for downstream checks:

- no implementation by this packet
- no build/tag/push/pull
- no browser E2E
- no passes:true
- no Close

## Follow-up Gate Order

1. If the user approves: `Control UI Runtime Persistence Fix Implementation Gate`.
2. Then `Runtime Image Build/Tag/Push Approval Packet`.
3. Then fresh instance / runtime deployment approval.
4. Then Browser/Manual E2E approval.
5. Then commit/write-back only after passing evidence exists and the user explicitly approves commit, `passes:true`, Close, and longterm write-back scope.

The implementation gate must not skip directly to image build, deploy, fresh instance work, browser E2E, commit, write-back, `passes:true`, or Close.

## Current Packet Explicit Negatives

- no implementation
- no runtime control-ui artifact modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no backend modification
- no frontend modification
- no runtime source modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no build/tag/push/pull
- no backend deploy/restart
- no fresh instance
- no fresh instance creation/deletion/modification
- no K8S mutation
- no runtime mutation
- no database mutation
- no registry mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no browser storage/cache/cookie cleanup
- no manual URL edit
- no gateway token/password entry
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no secrets/token/cookie/access URL plaintext output
- no Mem0
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-approval-packet.md
rg -n "CONTROL_UI_RUNTIME_PERSISTENCE_FIX_APPROVAL_PACKET_DONE|CONTROL_UI_RUNTIME_PERSISTENCE_FIX_APPROVAL_PACKET_BLOCKED|persisted gatewayUrl|page-derived WebSocket URL|GTManager-mediated|instances/<id>/control-ui|localStorage|sessionStorage|no manual URL|no gateway token|no implementation|no build/tag/push/pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-approval-packet.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-approval-packet.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including DONE/BLOCKED verdicts, `persisted gatewayUrl`, `page-derived WebSocket URL`, `GTManager-mediated`, `instances/<id>/control-ui`, `localStorage`, `sessionStorage`, `no manual URL`, `no gateway token`, `no implementation`, `no build/tag/push/pull`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-runtime-persistence-fix-approval-packet.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |
