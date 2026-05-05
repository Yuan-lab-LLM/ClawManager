# Design Amendment Draft: Control-UI Authorized Proxy

Feature: `gtmanager-gtclaw-m1-runtime-localization`  
Date: 2026-05-01 Asia/Shanghai  
Source gate: T8 rerun verdict `E2E EVIDENCE FAIL`  
Scope of this document: design amendment only. No source, deployment, runtime image, cluster, registry, evidence status, or long-term state mutation is authorized here.

## 1. Problem Statement

T8 rerun used disposable instance `3` (`clawmanager-user-1/clawreef-3-gtclaw-t8-dev-20260501001159`) with the expected GTClaw resources image:

- Image: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029`
- Image digest: `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45`
- Package: `openclaw@2026.4.14`
- Control UI path: `/usr/local/lib/node_modules/openclaw/dist/control-ui`

The test environment identity and runtime bundle hashes matched the T6 dev image, but the E2E route evidence failed:

1. `POST /api/v1/instances/3/access` generated access for the existing desktop proxy route.
2. `/api/v1/instances/3/proxy/` rendered `ClawManager Desktop` and loaded the desktop shell asset `assets/index-luFV63l_.js`.
3. `/api/v1/instances/3/proxy/chat?session=main` rendered `404 Not Found`.
4. Direct in-pod control-ui checks on `127.0.0.1:18789` returned `200` and rendered `GTClaw 控制台` with Chinese text for root and `/chat?session=main`.
5. Required GTManager wrapper routes remained reachable and Chinese-default, but the deployed app did not show GTClaw on required runtime-facing surfaces.

The direct `127.0.0.1:18789` evidence proves the patched control-ui process exists inside the pod. It is supplementary only. It is not a substitute for authorized GTManager proxy evidence, and it must not be treated as runtime proxy acceptance evidence.

Root cause: the current GTManager proxy design routes only to the desktop/webtop shell on port `3001`; it has no route-specific authorized path for the OpenClaw/GTClaw control-ui process on `18789`. Separately, wrapper source/deployed bundle evidence is not yet aligned with the GTClaw runtime-facing copy requirement.

This amendment supersedes the original `plan.md` assumption that M1 would add no new runtime path, no new proxy layer, and no new proxy behavior. T8 evidence shows that assumption cannot satisfy the authorized GTManager control-ui E2E requirement. This is an explicit T8-evidence-based design revision requiring user approval before implementation, not silent scope creep.

## 2. Recommended Design

Keep the existing desktop route semantics:

- `/api/v1/instances/:id/proxy/` remains the desktop/webtop route.
- Existing desktop access remains default behavior.
- Existing desktop iframe behavior, cookie path, `SUBFOLDER`, and `3001` target behavior remain in place.

Add a control-ui-specific authorized proxy route:

- New route: `/api/v1/instances/:id/control-ui/`
- Route target: OpenClaw/GTClaw control-ui on the selected control-ui target port. For the bind path this is `18789`; for a bridge fallback it may be a different pod-reachable port.
- Route behavior: strip `/api/v1/instances/:id/control-ui` before forwarding, so:
  - `/api/v1/instances/:id/control-ui/` forwards to `/`
  - `/api/v1/instances/:id/control-ui/chat?session=main` forwards to `/chat?session=main`
- HTML base injection and redirect rewriting use `/api/v1/instances/:id/control-ui/`, not `/proxy/`.
- `X-Forwarded-Prefix` uses `/api/v1/instances/:id/control-ui`.
- Scheme is route-specific. The control-ui route should use `http` unless a fresh probe confirms `18789` is TLS.

Static asset and SPA fallback requirements:

- The control-ui route must prove that the HTML shell, JS/CSS/font/image assets, and history fallback all resolve under `/api/v1/instances/:id/control-ui/`; a title-only HTML response is not enough.
- `<base>` injection, absolute `src`/`href` handling, redirect rewriting, and any runtime router fallback must use the active control-ui route prefix, not `/proxy/`.
- If the runtime uses WebSocket, SSE, or streaming APIs for chat, those upgrades/streams must be routed with the same mode-scoped authorization and prefix stripping.
- The handler must never silently fall back from `/control-ui/` to `/proxy/` when the control-ui upstream is unreachable. It must return a clear upstream/reachability error so E2E cannot be misclassified as a desktop success.

The route should share hardened proxy plumbing where practical, but the route configuration must be explicit. Do not infer control-ui behavior from `instanceType == openclaw` inside the desktop proxy path, because that would blur the existing `/proxy/` contract and increase regression risk.

## 3. Access Design

Add access mode support to the current instance access API:

```text
POST /api/v1/instances/:id/access?mode=desktop
POST /api/v1/instances/:id/access?mode=control-ui
```

Mode rules:

- Valid modes are exactly `desktop` and `control-ui`.
- Missing `mode` defaults to `desktop` for backward compatibility.
- `desktop` maps to `/api/v1/instances/:id/proxy/` and target port `3001` or the existing runtime config port.
- `control-ui` maps to `/api/v1/instances/:id/control-ui/` and the selected control-ui target port.
- `control-ui` is valid only for OpenClaw runtime instances unless a later design explicitly expands support.
- Invalid `mode` values must return an expected client error; recommended response is `400 Bad Request` with no access token and no route cookie.
- Recommended contract: accept `mode` only as a query parameter for this amendment. Do not also accept JSON body `mode`, because supporting both creates precedence ambiguity and expands the API contract. If a future body-based contract is needed, it should be designed separately; body/query conflicts should be rejected with `400 Bad Request`.
- Existing instance ownership/admin authorization must run before token issuance for both modes. `mode=control-ui` must not bypass any current access policy.
- `mode=control-ui` on a non-OpenClaw runtime must return a deterministic no-token/no-cookie client error. Recommended response is `400 Bad Request` unless existing access policy intentionally uses `403` or `404` to avoid information disclosure.

Extend the signed access claim with mode and scope:

| Claim / field | Purpose |
| --- | --- |
| `instance_id` | Keeps token bound to one instance. |
| `user_id` | Keeps token bound to the requesting user/admin authority. |
| `instance_type` | Preserves current validation context. |
| `target_port` | Records `3001` for desktop or the selected control-ui target port. |
| `access_mode` | New field: `desktop` or `control-ui`. |
| `route_prefix` | Optional but recommended: expected route prefix for handler enforcement. |
| `token_type` | Continue `instance_access`; reject unknown values. |

Cookie rules:

| Mode | Cookie name | Cookie path | Route usable by cookie |
| --- | --- | --- | --- |
| `desktop` | `instance_access_<id>` | `/api/v1/instances/<id>/proxy` | Desktop proxy only |
| `control-ui` | `instance_control_ui_access_<id>` | `/api/v1/instances/<id>/control-ui` | Control-ui proxy only |

Handler enforcement:

- `/proxy/` accepts only a token with `access_mode == desktop`.
- `/control-ui/` accepts only a token with `access_mode == control-ui`.
- A query token may be promoted to the route-specific cookie for subresources, using the matching cookie name/path.
- A control-ui token must not authorize `/proxy/`; a desktop token must not authorize `/control-ui/`.
- Legacy tokens that lack `access_mode` must remain desktop-only and must be rejected by `/control-ui/`.
- The route handler must compare `instance_id`, `user_id` or equivalent subject, `token_type`, `access_mode`, `target_port`, and `route_prefix` against the server-selected route configuration. The request must not be able to choose an arbitrary target port.
- Route cookies should keep the existing secure cookie posture and must be `HttpOnly`; under HTTPS they must be `Secure`; `SameSite` should remain at least as restrictive as the existing instance-access cookie; `Max-Age`/expiry must not outlive the signed token.
- If a token-bearing URL is used for first load, the handler should promote it to the route cookie and redirect to a tokenless URL before loading subresources where practical. Evidence and logs must never include the token-bearing URL.
- Response may keep `token`, `access_url`, `proxy_url`, and `expires_at` for compatibility, but should include `access_mode`. A clearer optional field such as `control_ui_url` may be added for frontend use.

## 4. Runtime Reachability Design

T8 showed only in-pod loopback supplementary evidence: control-ui responds at `127.0.0.1:18789` inside the pod. That does not prove pod-network reachability, because a Kubernetes Service targets the pod network address, not the process loopback socket.

Current pod-network reachability has already failed: `podIP:18789` was tested and is currently unreachable. GTManager proxy E2E is therefore directly blocked until a pod-reachable control-ui target exists. The fix must be one of:

1. Bind the control-ui process to `0.0.0.0:18789`; or
2. Add an in-pod bridge that exposes a pod-reachable selected control-ui target port and forwards to `127.0.0.1:18789`.

Direct `127.0.0.1:18789` checks may remain supplementary evidence only. They cannot satisfy authorized GTManager proxy E2E.

| Option | Description | Pros | Risks |
| --- | --- | --- | --- |
| Bind control-ui to `0.0.0.0:18789` | Configure the control-ui process to listen on all pod interfaces. | Smallest network model; Kubernetes Service can target `18789`; easiest to verify with `podIP:18789` and `ClusterIP:18789`; no extra bridge process. | Requires knowing the runtime control-ui bind option; could expose the service inside the namespace if Service/NetworkPolicy scope is too broad. |
| In-pod bridge to `127.0.0.1:18789` | Add a bridge process that listens on a pod-reachable selected control-ui target port and forwards to loopback control-ui. | Works if upstream control-ui cannot change bind address. | More moving parts; process supervision and failure modes; port conflict if trying to bind `0.0.0.0:18789` while loopback already owns `18789`; may need a different bridge listen port or pod-IP-specific bind. |

Recommendation: use `0.0.0.0:18789` as the primary design if the OpenClaw control-ui launcher supports it. It is the minimal verifiable path and aligns with Kubernetes Service behavior.

Approval condition for the bind path: keep exposure limited to the pod/ClusterIP path required by GTManager. Do not add NodePort, LoadBalancer, Ingress, hostNetwork, or broad deployment exposure for this gate. If NetworkPolicy is available, document the expected restriction; if it is not available in the local K3S/K3D environment, record that as an environment limitation rather than treating it as external exposure approval.

Fallback: use an in-pod bridge only if bind configuration is unavailable. The bridge should listen on a distinct pod-reachable selected control-ui target port or a pod-IP-specific address, and the GTManager route configuration must record that target port explicitly. Do not hide the bridge behind the same `18789` assumption unless a probe proves there is no bind conflict. The bridge path also needs supervision/failure evidence so a crashed bridge cannot be mistaken for a runtime UI problem.

Minimum reachability checks for the selected option:

1. Inside pod: `http://127.0.0.1:18789/` returns the control-ui shell.
2. Inside pod: `http://127.0.0.1:18789/chat?session=main` returns the chat route.
3. From a same-namespace debug context or equivalent read-only probe: `http://<podIP>:<selected-control-ui-target-port>/` returns the control-ui shell.
4. Through Kubernetes Service: `http://<service-cluster-ip>:<selected-control-ui-target-port>/` returns the control-ui shell.
5. Through GTManager: `/api/v1/instances/:id/control-ui/` and `/api/v1/instances/:id/control-ui/chat?session=main` render GTClaw/Chinese UI.

## 5. Service Behavior

The current service lookup path can misclassify an existing Service as usable for a requested target port:

- `InstanceProxyService.getOrCreateService()` calls `ServiceService.GetServiceInfo(ctx, userID, instanceID, targetPort)`.
- `ServiceService.GetServiceInfo()` returns `extractServiceInfo(service, targetPort)`.
- `extractServiceInfo()` reports the requested `TargetPort` even if `service.Spec.Ports` does not contain that port.

For control-ui this is dangerous: an existing Service created for `3001` can be treated as usable for the selected control-ui target port, even though the Service does not expose that port.

Required service design change:

1. Add a service-port validation helper that returns a match only when a concrete `service.Spec.Ports[]` entry can actually route to the selected pod target port. A Service `.port` match alone is not enough if `.targetPort` points to a different pod port.
2. Make `GetServiceInfo(..., targetPort)` return an error when no ServicePort maps to the requested pod target port. The returned `ServiceInfo` should distinguish the Service port GTManager will dial from the pod `target_port` used for validation.
3. Add an idempotent ensure path that creates or patches the Service so it contains all required ports for the requested route. For M1, prefer an unambiguous mapping where the control-ui Service port and targetPort are both the selected control-ui port unless implementation has a documented reason to differ.
4. For an existing desktop Service, adding the selected control-ui target port must preserve the existing `3001` port, selector, ClusterIP, Service type, and desktop behavior.
5. The control-ui proxy must not proceed until the Service has a verified ServicePort mapping for the selected pod target port.
6. Named `targetPort` values must be resolved to the actual container port or rejected as unverifiable for this gate.

Recommended implementation shape:

- Add `EnsureServicePort(ctx, userID, instanceID, targetPort, additionalPorts)` or update the existing creation path into an idempotent `EnsureService`.
- Keep stable port names, for example `http` for primary desktop if already present and `control-ui` or `tcp-<selected-port>` for control-ui; do not change existing desktop port names unless separately approved.
- Keep Service exposure internal to the current instance Service shape; do not add NodePort, LoadBalancer, Ingress, or host networking as part of this amendment.
- Add unit tests for an existing Service that lacks the selected control-ui target port; the expected behavior is a patch or a clear error, not a synthetic usable `ServiceInfo`.
- Add unit tests for misleading ServicePort combinations, such as `port: 18789` with `targetPort: 3001`, and `port: 3001` with `targetPort: 18789`, so implementation cannot confuse Service dial port with pod target port.

## 6. Frontend Design

### Wrapper Runtime-Facing Copy

GTManager remains the manager/control-plane brand. Runtime-facing user copy should show GTClaw where it describes the user's boxed runtime experience.

Required wrapper surfaces:

- `/instances/new`
- Instance detail route
- `/portal`
- `/openclaw-configs`
- `/admin/settings`
- `/admin/instances`
- `/admin/ai-audit`
- `/admin/ai-gateway`

Use existing i18n only. Do not introduce another localization framework. Preserve technical identifiers:

- `openclaw` type IDs
- API paths
- storage keys
- `.openclaw` archive naming
- `openclaw.json`
- OpenClaw Image identity
- image names/tags/digests
- DB/K8S/runtime status fields

Concrete copy direction:

- Instance type display: GTClaw where user-facing.
- Gateway/control entry: GTClaw control or GTClaw control UI where user-facing.
- Resource management title: GTClaw resources where user-facing, while preserving OpenClaw technical resource/import/export terms where the text names `.openclaw`, OpenClaw bootstrap, or OpenClaw Image mechanics.
- Admin image/settings cards: keep image/runtime technical IDs, but user-facing runtime card descriptions may mention GTClaw when the card is displayed as a product choice.

### Control-UI Entry

Frontend should be able to request both access modes:

```ts
generateAccessToken(id, { mode: "desktop" })
generateAccessToken(id, { mode: "control-ui" })
```

Recommended UI behavior:

- Existing desktop iframe continues to request desktop mode and use `/proxy/`.
- OpenClaw/GTClaw instance detail and portal surfaces may expose a GTClaw control-ui entry that requests control-ui mode and opens `/control-ui/` or `/control-ui/chat?session=main`.
- The control-ui entry should not replace the desktop iframe unless the user explicitly approves that product behavior.

### Deployed Bundle Evidence

T8 showed source/deployment mismatch risk: source may contain some GTClaw strings, but deployed wrapper routes did not render GTClaw.

Add a deployment evidence gate before T8 rerun:

1. Build the frontend from the intended source revision.
2. Record the generated asset names/hashes without exposing credentials.
3. Deploy or refresh the frontend bundle through the approved environment path.
4. Browser-load the deployed app and record the actual loaded asset URLs.
5. Scan the deployed asset text for required GTClaw display strings and protected technical literals.
6. Confirm required wrapper routes render GTClaw where runtime-facing.

This gate must distinguish "source contains the string" from "deployed browser route renders the string."

Stale-bundle fail criteria:

- If the browser-loaded asset URL/hash does not match the intended build output, the wrapper evidence gate fails even if source scans pass.
- Evidence should include a cache-busting or fresh-profile load method and the actual asset URLs seen by the browser, with credentials and tokens omitted.
- Required deployed route checks must be repeated after the refreshed bundle is served; screenshots or DOM extracts from an old bundle cannot be reused as current evidence.

## 7. File Ownership Map

| Area | Proposed ownership | Read/write note for future gates |
| --- | --- | --- |
| Backend access/API | `backend/internal/handlers/instance_handler.go`, `backend/internal/services/instance_access_service.go`, `backend/internal/services/instance_access_service_test.go`, `backend/cmd/server/main.go` | Add access mode, mode claim, route-specific cookies, and new route only after explicit implementation approval. |
| Backend proxy | `backend/internal/services/instance_proxy_service.go` plus new or existing tests | Generalize route config for desktop vs control-ui, target path extraction, scheme, base injection, redirect rewriting, and WebSocket handling if needed. |
| Backend Service behavior | `backend/internal/services/k8s/service_service.go` plus new tests | Validate requested target port and patch/create Services idempotently. |
| Frontend access client/hooks | `frontend/src/services/instanceService.ts`, `frontend/src/hooks/useInstanceDesktopAccess.ts` or a new control-ui-specific hook | Keep desktop default; add explicit `mode=control-ui` support for GTClaw control-ui entry. |
| Frontend wrapper/i18n | `frontend/src/lib/i18n.ts`, `frontend/src/contexts/I18nContext.tsx`, listed wrapper pages/components | Use existing i18n and preserve protected technical literals. |
| Runtime image/test environment | Runtime control-ui launcher/config in the image build path or approved test env mutation prompt | Choose `0.0.0.0:18789` primary path or approved bridge fallback with an explicit selected control-ui target port. No repo deployment default changes without separate approval. |
| Evidence | Future evidence under `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/` | Record route evidence, wrapper deployed-bundle evidence, service reachability, hashes, and protected literal scans only under a separately approved evidence gate. |

## 8. Updated Task Breakdown Proposal

Do not edit `tasks.md` from this amendment. The next task breakdown should add or revise gates as follows:

1. `T8A DesignAmendmentReview`: review this amendment and external-architecture risk packet.
2. `T8B BackendAccessContractWorker`: add access mode contract tests for desktop and control-ui tokens, route-specific cookies, mode enforcement, unsupported instance types, and no-token/no-cookie error paths.
3. `T8C BackendControlUIProxyWorker`: add `/control-ui/` route with route-specific proxy configuration; keep `/proxy/` desktop behavior unchanged; cover static assets, SPA fallback, redirects, and WebSocket/SSE/streaming if the runtime uses them.
4. `T8D ServicePortEnsureWorker`: fix Service target-port validation and idempotent add/patch behavior for the selected control-ui target port; distinguish Service dial port from pod targetPort.
5. `T8E RuntimeReachabilityWorker`: implement or configure `0.0.0.0:18789`, or produce an approved bridge design with an explicit selected control-ui target port if binding cannot be changed.
6. `T8F FrontendControlUIAccessWorker`: add `mode=control-ui` API support and optional GTClaw control-ui entry without replacing desktop.
7. `T8G WrapperBundleWorker`: finish wrapper runtime-facing GTClaw copy and prove the deployed bundle contains those changes, including actual browser-loaded asset URLs/hashes.
8. `T8H ReviewWorker`: read-only review of backend route scope, frontend runtime copy, Service port behavior, protected literals, deployed bundle evidence, and token/cookie mode scoping.
9. `T8I EvidenceRerunWorker`: rerun E2E against the authorized proxy routes and deployed wrapper bundle, including exact pod-network and Service reachability command/output for the selected control-ui target port.

Dependency order:

- `T8A` first.
- `T8B` before backend implementation.
- `T8C` and `T8D` may run in parallel only if their write scopes are separated and tests are coordinated.
- `T8E` must finish before route E2E.
- `T8F` and `T8G` may run after backend API shape is stable.
- `T8H` before E2E rerun.
- `T8I` last.

## 9. Verification Plan

### Backend Unit Tests

Access mode:

- Signed token includes `access_mode`.
- Valid modes are exactly `desktop` and `control-ui`.
- Missing mode defaults to desktop.
- `mode=control-ui` produces route prefix `/api/v1/instances/:id/control-ui/`.
- Invalid mode returns an expected client error; recommended response is `400 Bad Request`.
- Mode is supported only as a query parameter in this amendment; JSON body mode is not supported, and body/query conflicts are rejected with `400 Bad Request`.
- `mode=control-ui` on an unsupported runtime type returns a no-token/no-cookie client error.
- Existing user/admin authorization remains required before either mode can mint access.
- Desktop token is rejected by control-ui route.
- Control-ui token is rejected by desktop route.
- Cookie names, paths, expiry, and secure attributes are route-specific and do not outlive token expiry.
- Legacy tokens remain desktop-only unless explicitly migrated.
- Token-bearing URLs are not logged or written to evidence, and redirect-to-tokenless-URL behavior is tested if implemented.

Proxy route:

- `/proxy/` strips or preserves path exactly as current desktop semantics require.
- `/control-ui/` strips `/api/v1/instances/:id/control-ui` and forwards `/` and `/chat?session=main`.
- `/control-ui/` resolves JS/CSS/font/image assets and history fallback under the control-ui route prefix.
- `X-Forwarded-Prefix`, HTML `<base>` injection, absolute asset rewriting, and redirect rewriting use the active route prefix.
- Control-ui scheme and port are route-specific.
- WebSocket/SSE/streaming proxy behavior is covered if the runtime chat path uses it.
- `/control-ui/` never falls back to desktop `/proxy/` on upstream failure.

Service behavior:

- Existing Service with only `3001` is not reported usable for the selected control-ui target port.
- Misleading ServicePort combinations do not pass validation: `port: 18789` with `targetPort: 3001`, `port: 3001` with `targetPort: 18789`, named targetPort without resolvable container port, and duplicate/conflicting port names.
- Ensure path adds or patches the selected control-ui target port while keeping `3001`.
- Missing Service creates both needed ports when requested by route config.
- Service patch preserves selector, ClusterIP, Service type, and existing desktop port behavior.

### Frontend Tests / Build Checks

- Access client appends `mode=desktop` and `mode=control-ui` as query parameters and preserves desktop default behavior.
- Control-ui entry appears only for the intended OpenClaw/GTClaw runtime surfaces and does not replace the desktop iframe unless separately approved.
- Required wrapper routes render GTClaw where runtime-facing while GTManager remains the manager/control-plane brand.
- Browser-loaded deployed asset URLs/hashes match the intended build output.
- Protected OpenClaw technical literals remain present where required in source and deployed bundle scans.

### Dry-Run / Prerequisite Checks

- `cd backend && go test ./...`
- `cd frontend && npm run lint`
- Frontend build in an approved temporary copy or approved generated-output path.
- `kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml`
- `kubectl apply --dry-run=client -f deployments/k8s/clawmanager.yaml`

These are prerequisite checks only. They do not replace E2E route evidence.

### E2E Rerun Coverage

Desktop route regression:

- `POST /api/v1/instances/:id/access?mode=desktop`
- `/api/v1/instances/:id/proxy/` still renders the desktop shell.
- Existing desktop iframe flow still works.

Runtime reachability:

- Exact command/output for the historical failed `podIP:18789` probe, or a clear statement that the selected bridge port replaced `18789` with the old failure retained as prior context. This gap is not a design-approval blocker, but it is a mandatory next E2E rerun item.
- `127.0.0.1:18789` inside the pod returns the control-ui shell and `/chat?session=main` only as supplementary context.
- `podIP:<selected-control-ui-target-port>` from a same-namespace debug context returns the control-ui shell.
- `serviceClusterIP:<selected-service-port>` returns the control-ui shell and maps to the selected pod target port.

Control-ui route:

- `POST /api/v1/instances/:id/access?mode=control-ui`
- `/api/v1/instances/:id/control-ui/` renders `GTClaw 控制台`.
- `/api/v1/instances/:id/control-ui/chat?session=main` renders the chat route and visible Chinese text.
- Static control-ui assets load under `/api/v1/instances/:id/control-ui/`; a partial HTML shell without assets is not sufficient.
- Direct `127.0.0.1:18789` or local port-forward evidence may be recorded only as supplementary reachability context.

Wrapper:

- `/instances/new`, instance detail, `/portal`, `/openclaw-configs`, `/admin/settings`, `/admin/instances`, `/admin/ai-audit`, and `/admin/ai-gateway` render GTClaw where runtime-facing.
- GTManager remains the manager/control-plane brand.
- Protected technical literals remain in source/deployed bundle scans where required.

Bundle:

- Browser-loaded asset names match the newly built/deployed bundle.
- Deployed bundle scan finds required GTClaw display strings.
- Deployed route rendering, not only source scan, is recorded.

## 10. Open Risks and User Approval Points

Open risks:

- `18789` loopback handling is architectural risk. External review is recommended before choosing bind vs bridge.
- Changing runtime bind address may expose control-ui inside the namespace; Service scope and NetworkPolicy assumptions need review.
- Bridge fallback adds process supervision and observability risk.
- Mode-scoped cookies and route-specific token enforcement are security-sensitive, especially if token-bearing URLs are used for first load.
- Existing Service patch behavior must avoid breaking currently running desktop access, and must not confuse Service `.port` with pod `.targetPort`.
- Control-ui proxy asset rewriting/history fallback can fail even when the root HTML title renders correctly.
- The known frontend lint debt may interfere with final quality gates unless separately triaged.
- Deployed bundle refresh may require environment mutation outside source changes, and stale browser-loaded assets can invalidate wrapper evidence.
- The scoped evidence files do not include the exact command/output for the failed `podIP:18789` probe. This should not block design approval, but the next E2E rerun must include exact pod-network and Service reachability probes.

Explicit user approval points:

1. Explicitly approve the new `/api/v1/instances/:id/control-ui/` route and the proxy/access contract deviation from the original no-new-runtime-path/no-new-proxy-behavior assumption.
2. Approve access token claim extension and route-specific cookie names/paths.
3. Choose or authorize runtime reachability path: preferred `0.0.0.0:18789` bind or bridge fallback with an explicit selected control-ui target port.
4. Approve Service patch/create behavior for adding the selected control-ui target port.
5. Approve frontend control-ui entry behavior.
6. Approve any test environment mutation needed to deploy the refreshed frontend bundle or runtime image.
7. Approve Service exposure and NetworkPolicy assumptions for the selected route and target port.
8. Decide whether the known frontend lint debt is waived for this gate or must be fixed before the later final write-back gate.
9. Approve the evidence rerun path after review.

External expert packet:

```text
Please review ClawManager's OpenClaw/GTClaw control-ui proxy design. Current `/api/v1/instances/:id/proxy/` should keep desktop/webtop semantics and target `3001`. Patched control-ui is visible inside the pod on `127.0.0.1:18789`, and direct probes show GTClaw/Chinese UI, but podIP:18789 was tested and failed / is currently unreachable. E2E requires authorized GTManager proxy access to control-ui root and `/chat?session=main`. Constraints: do not break existing desktop access, do not rename OpenClaw technical identifiers, and do not treat direct port-forward evidence as proxy acceptance evidence. Please review the proposed design: add `/api/v1/instances/:id/control-ui/` authorized proxy route plus `POST /api/v1/instances/:id/access?mode=control-ui`, mode-scoped token/cookie, and choose runtime bind `0.0.0.0:18789` or an in-pod bridge from an explicit selected control-ui target port to `127.0.0.1:18789`. Please identify the minimal change surface, major regression risks, and required tests.
```

## 11. Mutation Statement

This amendment draft writes only:

```text
specs/gtmanager-gtclaw-m1-runtime-localization/design-amendment-20260501-control-ui-proxy.md
```

No `frontend/**`, `backend/**`, `deployments/**`, `docs/**`, `longterm/**`, current `spec.md`, current `plan.md`, current `tasks.md`, runtime image, pod, Service, system image setting, registry, or Kubernetes resource mutation is authorized or performed by this design draft.
