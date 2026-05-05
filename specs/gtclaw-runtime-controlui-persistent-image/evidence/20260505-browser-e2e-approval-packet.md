# Browser E2E Approval Packet - 2026-05-05

Worker: BrowserE2EApprovalPacketWorker

Topology: serial

## Verdict

BROWSER_E2E_APPROVAL_PACKET_DONE

This is an approval packet only. It requests user approval for a future Browser E2E Gate, but it does not authorize or execute Browser E2E by itself.

No Chrome DevTools MCP, Playwright, browser navigation, browser screenshot, browser click, browser DOM inspection, build/tag/push/pull, backend deploy/restart, runtime image rebuild, fresh instance creation, Kubernetes mutation, runtime mutation, database mutation, registry mutation, `passes:true`, Close, longterm write-back, or Mem0 write occurred in this approval-packet gate.

## User Approval Request

Please approve or reject the next gate:

`Browser E2E Gate`

Approval is required before any Chrome DevTools MCP or Playwright/browser automation is used against the target below.

## Dependency Gates

| Gate | Evidence | Result used |
| --- | --- | --- |
| Rerun Isolated 2Gi Fresh Instance After Backend Deploy Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy.md` | Commander read-only review accepted `ISOLATED_2GI_FRESH_INSTANCE_RERUN_AFTER_BACKEND_DEPLOY_DONE`. |
| Control Plane Backend Build/Deploy Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-plane-backend-build-deploy.md` | `CONTROL_PLANE_BACKEND_BUILD_DEPLOY_DONE`; backend image `clawmanager:control-plane-backend-gtclaw-20260505183733`; `/healthz` returned `200 ok`. |
| WS Auth Bridge Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md` | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE`; backend source includes control-ui WebSocket first-connect auth bridge and browser auth stripping. |
| Runtime Startup Artifact Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md` | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE`; startup artifact established pod-facing bind/auth changes later included in the runtime image. |
| Prior Browser E2E failure routes | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e.md` and `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md` | Historical instance `5` Browser E2E was blocked by `502` / connection refused on target port `18789`; desktop `3001` regression smoke passed. |

## Future E2E Target

If the user approves the Browser E2E Gate, the future worker must target only this already-created fresh instance:

| Field | Value |
| --- | --- |
| instance | `10` / `oc2gi-185707` |
| pod | `clawmanager-user-1/clawreef-10-oc2gi-185707` |
| backend image | `clawmanager:control-plane-backend-gtclaw-20260505183733` |
| runtime image digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| runtime image tag label | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| runtime pod status dependency | pod Ready, restart count `0`, no OOM evidence |
| Service dependency | Service exposes `3001` and `18789`; `18789` reachable on PodIP and ServiceIP; `3001` reachable |

The future Browser E2E worker must not create a new fresh instance and must not retarget to another pod unless the user separately approves a new packet.

## Future E2E Must Verify

The future Browser E2E Gate must verify all of the following through GTManager, not direct pod access:

| Requirement | Expected evidence shape |
| --- | --- |
| GTManager route for `control-ui` returns non-502 | Browser/network summary may record status class and redacted route label only. |
| `control-ui` page loads through GTManager | Evidence must prove the route was GTManager-mediated and not direct PodIP, ServiceIP, port-forward, or pod-local access. |
| WebSocket connects through backend bridge without browser-visible upstream token | Browser console/network evidence must show no upstream token exposure; WebSocket/session bootstrap should reach the expected authenticated ready state or clearly record the authenticated-ready equivalent. |
| no 502 / connection refused on `/control-ui` | Evidence must distinguish successful GTManager route loading from the prior 502/connection-refused failure class. |
| desktop `/proxy` or `3001` route still works at regression-smoke level | Evidence must confirm desktop behavior remains usable and is not replaced by control-ui. |
| no token/cookie/access URL plaintext in evidence | Evidence may record redacted route labels and boolean presence metadata only, never values or access URLs. |

## Future E2E Allowed Tools

Only after explicit user approval, the future Browser E2E Gate may use:

- Chrome DevTools MCP or Playwright/browser automation.
- Normal browser read, navigation, clicks, console/network inspection, and screenshots for the approved target only.
- Transient authenticated browser state needed for GTManager access, provided token values, cookie values, credentials, secrets, and access URLs are not recorded.

## Future E2E Forbidden

The future Browser E2E Gate remains forbidden from:

- no build/tag/push/pull
- no backend deploy/restart
- no runtime image rebuild
- no fresh instance creation
- no manual pod/service patch
- no database/K8S/runtime/registry mutation
- no Kubernetes Secret or ConfigMap mutation
- no runtime pod/container restart, delete, repair, or file write
- no `kubectl cp` write
- no direct pod access as the browser route under test
- no secrets/tokens/cookies/access URLs plaintext in evidence
- no backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modification
- no passes:true
- no Close
- no longterm write-back
- no Mem0 write

## Future DONE Conditions

The future Browser E2E Gate may report done only if all of these are true:

| Condition | Required result |
| --- | --- |
| `/control-ui` through GTManager | Loads successfully via GTManager and returns non-502. |
| WebSocket/session bootstrap | Succeeds or reaches the expected authenticated ready state through the backend bridge. |
| desktop regression | `/proxy` or `3001` desktop route remains usable at regression-smoke level. |
| upstream token exposure | No browser console/network evidence exposes the upstream token. |
| secret hygiene | No token value, cookie value, credential, secret, or access URL plaintext is recorded. |

If any condition is missing, the future gate must report `BROWSER_E2E_BLOCKED` or an equivalent blocked verdict and must not repair the environment.

## Follow-up Gate Order

1. If user approves this packet: Browser E2E Gate.
2. Then: E2E Commander review.
3. Only after successful fresh E2E and explicit user approval: `passes:true` / Close / longterm write-back.

## Explicit Non-Actions In This Packet Gate

- no Browser E2E
- no Chrome DevTools MCP
- no Playwright
- no browser navigation
- no browser screenshots
- no browser clicks
- no build/tag/push/pull
- no backend deploy/restart
- no runtime image rebuild
- no fresh instance
- no manual pod patch
- no manual Service patch
- no Kubernetes mutation
- no runtime mutation
- no database mutation
- no registry mutation
- no backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modification
- no secrets/tokens/cookies/access URLs plaintext
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close

## Verification Commands

Commands used to verify this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-browser-e2e-approval-packet.md
rg -n "BROWSER_E2E_APPROVAL_PACKET_DONE|BROWSER_E2E_APPROVAL_PACKET_BLOCKED|Browser E2E|control-ui|WebSocket|GTManager|18789|3001|oc2gi-185707|clawreef-10-oc2gi-185707|no build/tag/push/pull|no fresh instance|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-browser-e2e-approval-packet.md
secret-shape scan on new evidence
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-browser-e2e-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-browser-e2e-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including `BROWSER_E2E_APPROVAL_PACKET_DONE`, Browser E2E, `control-ui`, WebSocket, GTManager, `18789`, `3001`, `oc2gi-185707`, `clawreef-10-oc2gi-185707`, `no build/tag/push/pull`, `no fresh instance`, `no passes:true`, and `no Close`. |
| secret-shape scan on this new evidence | `1` | No matches. |
| path-limited `git status --short` | `0` | Shows this new approval packet and the dependency rerun evidence in the requested path scope. |
