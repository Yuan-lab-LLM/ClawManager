# GPT Pro Review Notes: GTClaw Control-UI Design Approval

Date: 2026-05-01

Feature: `gtmanager-gtclaw-m1-runtime-localization`  
Gate: Design Approval Review  
Current evidence status: `E2E EVIDENCE FAIL`  
Implementation approval: not granted by this review.

## Verdict

`APPROVE WITH CONDITIONS`

The design is directionally correct and can proceed to the next plan/tasks amendment gate. It must not proceed directly to implementation until the plan/tasks are amended to include the conditions below.

## Critical Findings

No blocking concerns found.

## Non-blocking Concerns

1. The original Service-port wording was too easy to implement as a loose `port OR targetPort` match. It has been tightened so a Service is usable only when a concrete ServicePort actually maps to the selected pod target port.
2. Mode-scoped token/cookie design is reasonable, but implementation must enforce route prefix, access mode, instance/user subject, token type, and target port in the route handler. Cookie path alone is not a security boundary.
3. `/control-ui/` must prove full runtime UI load, not just root HTML/title. Static assets, history fallback, redirects, and WebSocket/SSE/streaming paths if used are now required review/test items.
4. The frontend deployed-bundle mismatch remains a material risk. Source scans are insufficient unless browser-loaded asset URLs/hashes match the intended build output.
5. The missing exact command/output for the failed `podIP:18789` probe should not block this design approval, but it is mandatory evidence in the next rerun.

## Recommended Approval Conditions

- Preserve `/api/v1/instances/:id/proxy/` as the desktop/webtop route.
- Add `/api/v1/instances/:id/control-ui/` as a separate authorized route for the runtime control-ui.
- Use `POST /api/v1/instances/:id/access?mode=control-ui` as query-only API contract for this amendment.
- Keep missing `mode` defaulting to desktop for backward compatibility.
- Return `400 Bad Request` for invalid `mode` with no access token and no route cookie.
- Return deterministic no-token/no-cookie client error for `mode=control-ui` on unsupported runtime types.
- Enforce mode-scoped token/cookie checks in the backend route handler, not only in frontend routing.
- Patch or create the instance Service idempotently so it exposes the selected control-ui target port without breaking the existing `3001` desktop port.
- Treat direct `127.0.0.1:18789` and local port-forward checks as supplementary only.
- Require final E2E through the authorized GTManager route and deployed wrapper bundle.

## Required Implementation Tasks

The next plan/tasks amendment should include at least:

1. `BackendAccessContractWorker`: access mode query contract, token claims, route-specific cookies, invalid/unsupported mode behavior, and negative authorization tests.
2. `BackendControlUIProxyWorker`: `/control-ui/` route, route-specific proxy config, prefix stripping, base/redirect rewriting, static asset loading, and streaming/WebSocket/SSE if used.
3. `ServicePortEnsureWorker`: exact ServicePort validation and idempotent add/patch for the selected control-ui port while preserving desktop `3001`.
4. `RuntimeReachabilityWorker`: preferred `0.0.0.0:18789` bind, or bridge fallback with explicit selected target port and supervision evidence.
5. `FrontendControlUIAccessWorker`: frontend access API support for `mode=control-ui`, control-ui entry, and no replacement of desktop iframe unless separately approved.
6. `WrapperBundleWorker`: deployed frontend bundle refresh and proof that browser-loaded assets match the intended build.
7. `ReviewWorker`: read-only review of backend route scope, frontend copy, Service patch behavior, protected literals, token/cookie scope, and deployed bundle evidence.
8. `EvidenceRerunWorker`: final E2E rerun through authorized GTManager proxy routes and deployed wrapper routes.

## Required Tests / E2E Evidence

Backend unit tests:

- Missing mode defaults to desktop.
- Valid modes are exactly `desktop` and `control-ui`.
- Invalid mode returns `400 Bad Request` with no token/cookie.
- Unsupported runtime type for `mode=control-ui` returns no token/cookie.
- Token includes and enforces `access_mode`, `target_port`, and route prefix or equivalent server-side route scope.
- Desktop token is rejected by `/control-ui/`.
- Control-ui token is rejected by `/proxy/`.
- Legacy tokens are desktop-only.

Service tests:

- Existing `3001`-only Service is not reported usable for control-ui.
- Existing Service is patched to add the selected control-ui port while preserving desktop `3001`.
- Misleading mappings such as `port:18789,targetPort:3001` and `port:3001,targetPort:18789` fail validation or are handled explicitly.
- Named targetPorts are resolved or rejected as unverifiable.
- Selector, ClusterIP, Service type, and desktop behavior are preserved.

Proxy route tests:

- `/proxy/` remains desktop semantics.
- `/control-ui/` forwards `/` and `/chat?session=main` to the control-ui upstream.
- Control-ui JS/CSS/font/image assets load under `/control-ui/`.
- HTML base, redirect rewriting, and `X-Forwarded-Prefix` use the active route prefix.
- Route does not fall back to desktop on upstream failure.
- Streaming/WebSocket/SSE behavior is tested if runtime chat uses it.

Frontend tests / build checks:

- Access client sends query-only `mode=desktop` and `mode=control-ui`.
- Existing desktop iframe still uses desktop mode.
- Control-ui entry appears only on intended runtime surfaces.
- Required wrapper routes render GTClaw where runtime-facing and keep GTManager as manager brand.
- `npm run lint` and build run as prerequisite checks, with known lint debt explicitly handled.
- Deployed browser-loaded asset URLs/hashes match the intended bundle.

Runtime reachability probes:

- In-pod `127.0.0.1:18789/` and `/chat?session=main` as supplementary context.
- Same-namespace `podIP:<selected-control-ui-target-port>/`.
- Service `ClusterIP:<selected-service-port>/`.
- Exact command/output for the historical failed `podIP:18789` probe or an explicit bridge-port replacement note.

Final E2E evidence:

- Desktop route regression still works.
- `POST /api/v1/instances/:id/access?mode=control-ui` returns expected access contract without exposing token values.
- `/api/v1/instances/:id/control-ui/` renders `GTClaw 控制台`.
- `/api/v1/instances/:id/control-ui/chat?session=main` renders the chat route and visible Chinese text.
- Required wrapper routes show GTClaw where runtime-facing.
- Protected OpenClaw technical literals remain preserved.
- No direct localhost or port-forward evidence is used as substitute for authorized GTManager proxy evidence.

## bind vs bridge Recommendation

Prefer `0.0.0.0:18789` if the OpenClaw control-ui launcher supports it and exposure remains limited to the pod/ClusterIP path required by GTManager. This is simpler, easier to verify, and avoids bridge supervision failure modes.

Use a bridge only if the control-ui bind address cannot be changed. The bridge must use an explicit selected pod-reachable target port, avoid `18789` bind conflicts, and include supervision/reachability evidence. The backend route and Service must record the selected bridge port explicitly.
