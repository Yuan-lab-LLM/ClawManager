# Tasks: GTManager / GTClaw M1 Runtime Localization

**Status**: Amended Draft - user review required before implementation

**Input**:

- `specs/gtmanager-gtclaw-m1-runtime-localization/spec.md`
- `specs/gtmanager-gtclaw-m1-runtime-localization/plan.md`
- `specs/gtmanager-gtclaw-m1-runtime-localization/design-amendment-20260501-control-ui-proxy.md`
- `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501-t8-e2e-rerun.md`
- `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501001159-test-env-mutation.md`
- `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501-gpt-pro-design-review-notes.md`

**Current gate**: Plan/tasks amendment only. This file records the post-T8, post-GPT-Pro task plan. It does not authorize implementation, runtime image work, cluster changes, evidence rerun, feature state updates, Close, or `passes:true`.

## Global Rules

- [ ] Current amendment write scope is only:
  - `specs/gtmanager-gtclaw-m1-runtime-localization/plan.md`
  - `specs/gtmanager-gtclaw-m1-runtime-localization/tasks.md`
- [ ] No worker may start implementation until the user explicitly reviews amended `plan.md`/`tasks.md` and explicitly approves implementation.
- [ ] Star topology applies: every worker reports only through the user back to Commander. Workers do not communicate with each other.
- [ ] Without E2E evidence plus explicit user approval, no artifact may write `passes:true`, say Close, say complete, say passed, say accepted, or update long-term feature state.
- [ ] Old `specs/gtmanager-m1-branding-localization/**` evidence may be used only as GTManager management-plane method reference. It is not GTClaw runtime evidence and must not be used as runtime acceptance evidence.
- [ ] Direct `127.0.0.1:18789` or local port-forward evidence is supplementary only. It cannot substitute for authorized GTManager proxy E2E.
- [ ] `podIP:18789` was tested and failed/currently unreachable. The next E2E rerun must include exact failed probe command/output, or an explicit bridge-port replacement note.
- [ ] Do not read, write, copy, paste, or expose token, secret, credential, `.env`, `.codex/auth.json`, or `.codex/config.toml` content.
- [ ] Do not use broad `OpenClaw` to `GTClaw` search/replace.
- [ ] Dirty worktree rule: every future worker must capture `git status --short` before starting and after finishing. Scope checks must compare pre/post status or inspect only the worker's explicitly authorized paths; raw repository-wide `git diff --name-only` alone is not valid evidence in this dirty worktree.
- [ ] Build artifact rule: `npm run build` or equivalent generated-output commands must run in a temporary copy/worktree or another explicitly approved generated-output path. Generated artifacts must not be mixed into source scope or used as proof of source changes.

## Protected OpenClaw Technical Literals

All future workers must preserve these technical literals unless a later user-approved plan explicitly expands scope:

- `openclaw`
- `OpenClaw Image`
- `clawmanager-openclaw-image`
- `.openclaw*`
- `openclaw.json`
- `openclaw dashboard --no-open`
- `dist/control-ui`
- `/usr/local/lib/node_modules/openclaw/dist/control-ui`
- API literals, DB literals, K8S literals, image tag literals, runtime status literals, storage literals, package literals, protocol literals, and path literals
- package name/version, CLI/config/client/module markers, localStorage keys, route paths, service ports, image digest, and compatibility identifiers

Ambiguous `OpenClaw` occurrences default to preservation unless classified before implementation as `change UI display`.

## Locked Evidence Facts

T8 rerun failed E2E while proving the patched runtime resources were present in the test pod.

| Item | Value |
| --- | --- |
| T8 verdict | `E2E EVIDENCE FAIL` |
| Instance | `3` / `gtclaw-t8-dev-20260501001159` |
| Pod | `clawmanager-user-1/clawreef-3-gtclaw-t8-dev-20260501001159` |
| Container | `desktop` |
| Image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` |
| ImageID/index digest | `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |
| Package | `openclaw@2026.4.14` |
| Control-ui path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Direct loopback evidence | `127.0.0.1:18789` rendered `GTClaw 控制台` and Chinese text; supplementary only |
| Proxy failure | `/api/v1/instances/3/proxy/` rendered `ClawManager Desktop`; `/proxy/chat?session=main` returned `404` |
| Wrapper failure | Deployed wrapper routes did not show required GTClaw runtime-facing copy |

## Execution Order and Parallelism

- [ ] T8A runs first as design-approval recording and amended task gate.
- [ ] T8B, T8C, and T8D are backend-focused and must start with failing tests before implementation. T8C and T8D may run in parallel only if file ownership is separated.
- [ ] T8E runtime reachability depends on the selected control-ui target port contract from T8B/T8C/T8D and explicit user approval for bind vs bridge.
- [ ] T8F frontend control-ui access depends on the backend access contract shape.
- [ ] T8G deployed bundle verification depends on T8F and user-approved test environment refresh.
- [ ] T8H review depends on T8B-T8G outputs.
- [ ] T8I E2E rerun is last and depends on T8H.
- [ ] T9 Close gate is conditions-only and may run only after T8I evidence plus explicit user Close review request. It must not execute Close or write long-term state.

## T8A DesignApprovalRecorder / Amended Task Gate

| Field | Requirement |
| --- | --- |
| 发给谁 | Commander / DesignApprovalRecorder |
| 串行/并行 | Serial, before worker dispatch |
| 依赖哪个 gate | Internal DesignAmendmentReviewer `READY FOR USER DESIGN APPROVAL REVIEW`; GPT Pro `APPROVE WITH CONDITIONS`; user approved plan/tasks amendment |
| Allowed files / paths | Write only current feature `plan.md` and `tasks.md`; read current feature spec, plan, tasks, design amendment, T8 evidence, test-env mutation evidence, GPT Pro review notes, AGENTS and constitution files |
| Forbidden files / paths | `frontend/**`, `backend/**`, `deployments/**`, `docs/**`, `longterm/**`, runtime image, pod, Service, registry, K8S resources, old M1 writes, `.env`, `.codex/auth.json`, `.codex/config.toml` |
| Expected output | Amended plan/tasks reflecting GPT Pro conditions; no implementation |
| Verification command or evidence | `git diff --check` on `plan.md` and `tasks.md`; `rg` for critical conditions; scope check with `git status --short -- <allowed/forbidden paths>` |

Tasks:

- [ ] Confirm `tasks.md` remains `Amended Draft - user review required before implementation`.
- [ ] Confirm implementation still requires a later explicit user approval.
- [ ] Confirm GPT Pro `APPROVE WITH CONDITIONS` items are represented in T8B-T8I.
- [ ] Confirm no Close, `passes:true`, or longterm write-back is authorized.

## T8B BackendAccessContractWorker

| Field | Requirement |
| --- | --- |
| 发给谁 | BackendAccessContractWorker |
| 串行/并行 | Serial before backend implementation; may be followed by T8C/T8D |
| 依赖哪个 gate | Amended tasks approved + explicit user implementation approval |
| Allowed files / paths | `backend/internal/handlers/instance_handler.go`; `backend/internal/services/instance_access_service.go`; matching backend tests under existing backend test structure; route registration only if needed for access contract tests |
| Forbidden files / paths | `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, runtime image, pod, Service, registry, K8S resources, `.env`, `.codex/auth.json`, `.codex/config.toml` |
| Expected output | Access mode contract implementation and tests |
| Verification command or evidence | `cd backend && go test ./...`; targeted access tests; pre/post `git status --short`; diff limited to allowed backend files |

Required tests before implementation:

- [ ] Missing `mode` defaults to `desktop`.
- [ ] Valid modes are exactly `desktop` and `control-ui`.
- [ ] Invalid `mode` returns `400 Bad Request` with no access token and no route cookie.
- [ ] `mode=control-ui` on unsupported runtime type returns deterministic no-token/no-cookie client error.
- [ ] Existing user/admin authorization runs before token issuance for both modes.
- [ ] Signed token includes and enforces `access_mode`, server-selected `target_port`, `token_type`, instance ID, user/admin subject, and route prefix or equivalent server-side route scope.
- [ ] Desktop token is rejected by `/control-ui/`.
- [ ] Control-ui token is rejected by `/proxy/`.
- [ ] Legacy tokens lacking `access_mode` remain desktop-only.
- [ ] Token-bearing URLs are not logged or written to evidence; redirect-to-tokenless-URL behavior is tested if implemented.

Implementation constraints:

- [ ] `mode` is query-only for this amendment; JSON body mode is not accepted.
- [ ] Body/query conflict returns `400 Bad Request`.
- [ ] Request must not be able to choose arbitrary target port.
- [ ] Route cookies must keep secure cookie posture: `HttpOnly`, `Secure` under HTTPS, SameSite at least as restrictive as existing instance-access cookie, expiry not longer than token.

## T8C BackendControlUIProxyWorker

| Field | Requirement |
| --- | --- |
| 发给谁 | BackendControlUIProxyWorker |
| 串行/并行 | May run in parallel with T8D only if write scopes do not overlap |
| 依赖哪个 gate | T8B access contract shape stable; explicit user implementation approval |
| Allowed files / paths | Backend route registration files; `backend/internal/services/instance_proxy_service.go`; backend proxy tests |
| Forbidden files / paths | `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, runtime image, pod, registry, K8S mutation, `.env`, `.codex/auth.json`, `.codex/config.toml` |
| Expected output | New `/api/v1/instances/:id/control-ui/` route with route-specific proxy config; `/proxy/` desktop behavior preserved |
| Verification command or evidence | `cd backend && go test ./...`; targeted proxy route tests; diff limited to allowed backend proxy files |

Required tests before implementation:

- [ ] `/api/v1/instances/:id/proxy/` remains desktop/webtop semantics and still targets desktop `3001` or existing runtime config.
- [ ] `/api/v1/instances/:id/control-ui/` strips `/api/v1/instances/:id/control-ui` and forwards `/`.
- [ ] `/api/v1/instances/:id/control-ui/chat?session=main` forwards `/chat?session=main`.
- [ ] JS/CSS/font/image assets resolve under `/control-ui/`; title-only HTML is not enough.
- [ ] HTML `<base>`, absolute `src`/`href`, redirect rewriting, SPA/history fallback, and `X-Forwarded-Prefix` use the active `/control-ui` prefix.
- [ ] WebSocket/SSE/streaming behavior is covered if runtime chat uses it.
- [ ] `/control-ui/` never silently falls back to `/proxy/` desktop on upstream failure.
- [ ] Control-ui scheme and port are route-specific and come from server-selected configuration.

Implementation constraints:

- [ ] Do not infer control-ui behavior from `instanceType == openclaw` inside the desktop proxy path.
- [ ] Preserve `/proxy/` cookie path, `SUBFOLDER`, iframe, and desktop behavior.
- [ ] Return a clear upstream/reachability error when control-ui upstream is not reachable.

## T8D ServicePortEnsureWorker

| Field | Requirement |
| --- | --- |
| 发给谁 | ServicePortEnsureWorker |
| 串行/并行 | May run in parallel with T8C only if write scopes are separated |
| 依赖哪个 gate | T8B access contract shape stable; selected target port known; explicit user implementation approval |
| Allowed files / paths | `backend/internal/services/k8s/service_service.go`; backend Service tests; minimal related backend service files if needed for compile |
| Forbidden files / paths | `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, runtime image, pod, registry, direct K8S mutation, `.env`, `.codex/auth.json`, `.codex/config.toml` |
| Expected output | Service validation and idempotent ensure/patch for selected control-ui target port while preserving desktop `3001` |
| Verification command or evidence | `cd backend && go test ./...`; targeted Service tests; no deployment YAML diff |

Required tests before implementation:

- [ ] Existing `3001`-only Service is not reported usable for control-ui.
- [ ] Service `.port` match alone is not enough if `.targetPort` points to a different pod port.
- [ ] Misleading `port:18789,targetPort:3001` fails validation or is explicitly rejected.
- [ ] Misleading `port:3001,targetPort:18789` fails validation or is explicitly handled.
- [ ] Named `targetPort` resolves to actual container port or is rejected as unverifiable for this gate.
- [ ] Duplicate/conflicting port names are handled deterministically.
- [ ] Existing Service patch adds selected control-ui port while preserving desktop `3001`, selector, ClusterIP, Service type, and existing desktop behavior.
- [ ] Missing Service creates required desktop/control-ui ports when requested by route config.

Implementation constraints:

- [ ] Returned `ServiceInfo` must distinguish Service dial port from pod `target_port`.
- [ ] For M1, prefer unambiguous Service port and targetPort both equal to selected control-ui port unless implementation has a documented reason to differ.
- [ ] Do not add NodePort, LoadBalancer, Ingress, hostNetwork, or broad exposure as part of this task.

## T8E RuntimeReachabilityWorker

| Field | Requirement |
| --- | --- |
| 发给谁 | RuntimeReachabilityWorker |
| 串行/并行 | Serial after selected control-ui target port is defined |
| 依赖哪个 gate | T8B/T8C/T8D selected target port contract; user explicitly chooses or authorizes bind vs bridge; user approves any test environment mutation |
| Allowed files / paths | Approved runtime image/test-env path only; approved evidence path if separately granted; read-only pod/image inspection |
| Forbidden files / paths | Repo source writes unless separately approved; `frontend/**`, `backend/**`, `deployments/**`, `docs/**`, `longterm/**`; `latest` overwrite; NodePort/Ingress/hostNetwork/broad exposure unless separately approved; credentials/secrets |
| Expected output | Runtime reachability record: bind or bridge path, selected target port, podIP/Service reachability, supervision evidence if bridge |
| Verification command or evidence | Exact redacted command/output for reachability probes; no token/secret output |

Preferred path:

- [ ] Configure control-ui launcher to bind `0.0.0.0:18789` if supported.
- [ ] Keep exposure limited to pod/ClusterIP path required by GTManager.
- [ ] Do not add NodePort, LoadBalancer, Ingress, hostNetwork, or broad deployment exposure.
- [ ] If NetworkPolicy is available, document expected restriction. If unavailable in local K3S/K3D, record as environment limitation.

Bridge fallback:

- [ ] Use only if bind cannot be configured.
- [ ] Bridge listens on explicit selected pod-reachable target port and forwards to `127.0.0.1:18789`.
- [ ] Avoid `18789` conflicts unless probes prove no bind conflict.
- [ ] Include bridge supervision/failure evidence so a crashed bridge is not mistaken for runtime UI failure.
- [ ] Backend route and Service config must record the selected bridge port explicitly.

Required probes:

- [ ] In pod: `http://127.0.0.1:18789/` returns control-ui shell as supplementary context.
- [ ] In pod: `http://127.0.0.1:18789/chat?session=main` returns chat route as supplementary context.
- [ ] Same-namespace debug context: `http://<podIP>:<selected-control-ui-target-port>/` returns control-ui shell.
- [ ] Service path: `http://<service-cluster-ip>:<selected-service-port>/` returns control-ui shell and maps to selected pod target port.
- [ ] Exact command/output for historical failed `podIP:18789` probe, or explicit bridge-port replacement note.

## T8F FrontendControlUIAccessWorker

| Field | Requirement |
| --- | --- |
| 发给谁 | FrontendControlUIAccessWorker |
| 串行/并行 | After T8B access contract shape stable; may run before runtime reachability if API contract is fixed |
| 依赖哪个 gate | Backend access contract stable; user explicitly approves frontend implementation |
| Allowed files / paths | `frontend/src/services/instanceService.ts`; `frontend/src/hooks/useInstanceDesktopAccess.ts` or new control-ui hook; approved runtime-entry components/pages from wrapper list; `frontend/src/lib/i18n.ts` only for runtime-facing copy if needed |
| Forbidden files / paths | `backend/**`, `deployments/**`, `docs/**`, `longterm/**`, runtime image paths, frontend generated artifacts in source scope, `.env`, `.codex/auth.json`, `.codex/config.toml` |
| Expected output | Frontend can request desktop and control-ui modes explicitly; GTClaw control-ui entry appears only on intended runtime surfaces |
| Verification command or evidence | Frontend lint/build prerequisite checks; source diff limited to approved frontend files; deployed bundle verification deferred to T8G |

Required tests/checks:

- [ ] Access client appends `mode=desktop` and `mode=control-ui` as query parameters.
- [ ] Existing desktop iframe still uses desktop mode and `/proxy/`.
- [ ] Control-ui entry uses `mode=control-ui` and opens `/control-ui/` or `/control-ui/chat?session=main`.
- [ ] Control-ui entry appears only for intended OpenClaw/GTClaw runtime surfaces.
- [ ] Control-ui entry does not replace desktop iframe unless separately approved.
- [ ] GTManager remains manager/control-plane brand.
- [ ] Runtime-facing wrapper copy shows GTClaw where required.
- [ ] Protected OpenClaw technical literals remain present where required.

Suggested prerequisite checks:

```bash
cd frontend && npm run lint
rsync -a --delete --exclude node_modules --exclude dist frontend/ /tmp/gtclaw-frontend-build/
cd /tmp/gtclaw-frontend-build && npm ci && npm run build
```

Known lint debt must be handled according to explicit user decision: waived for this gate or fixed before final write-back.

## T8G WrapperBundleWorker

| Field | Requirement |
| --- | --- |
| 发给谁 | WrapperBundleWorker |
| 串行/并行 | Serial after frontend source changes and approved environment refresh path |
| 依赖哪个 gate | T8F output ready; user approves deployed bundle refresh/test environment mutation |
| Allowed files / paths | Approved temporary build path or deployment refresh path; optional write only to approved evidence path under `specs/.../evidence/*.md` |
| Forbidden files / paths | Source writes unless separately approved; `backend/**`, `deployments/**`, `docs/**`, `longterm/**`; generated artifacts in source scope; token/cookie/secret output |
| Expected output | Deployed bundle evidence proving browser-loaded asset URLs/hashes match intended build and wrapper routes render GTClaw |
| Verification command or evidence | Browser-loaded asset URLs/hashes; fresh-profile/cache-busted route checks; no credentials/tokens |

Required evidence:

- [ ] Intended frontend build asset names/hashes recorded.
- [ ] Browser-loaded deployed asset URLs/hashes recorded and match intended build.
- [ ] Fresh-profile or cache-busting load method recorded.
- [ ] `/instances/new`, instance detail, `/portal`, `/openclaw-configs`, `/admin/settings`, `/admin/instances`, `/admin/ai-audit`, `/admin/ai-gateway` render GTClaw where runtime-facing.
- [ ] GTManager remains manager/control-plane brand.
- [ ] Evidence fails if browser-loaded asset URL/hash is stale, even if source scans pass.

## T8H ReviewWorker

| Field | Requirement |
| --- | --- |
| 发给谁 | ReviewWorker |
| 串行/并行 | Serial after T8B-T8G |
| 依赖哪个 gate | Backend, Service, runtime reachability, frontend, and bundle artifacts available |
| Allowed files / paths | Read-only diffs, test outputs, route/service/runtime/bundle evidence, protected-literal scans, approved temporary workspaces |
| Forbidden files / paths | Any write; source mutation; deployment mutation; runtime image mutation; K8S mutation; registry mutation; `longterm/**`; credentials/secrets |
| Expected output | Findings ordered by severity, scope boundary check, protected literal check, token/cookie review, ServicePort review, E2E readiness decision |
| Verification command or evidence | `git diff --check`; backend/frontend test outputs; protected literal scans; no token/secret output |

Review checklist:

- [ ] T8B modified only approved backend access files and tests.
- [ ] T8C preserved `/proxy/` desktop semantics and added `/control-ui/` route-specific behavior.
- [ ] T8D did not modify deployment YAML and preserves desktop `3001`.
- [ ] T8E chose approved bind/bridge path and includes podIP/Service probes.
- [ ] T8F changed only approved frontend files and preserved protected literals.
- [ ] T8G proves deployed browser-loaded asset URLs/hashes, not source-only scans.
- [ ] Token/cookie mode scoping is enforced server-side.
- [ ] No token, secret, credential, `.env`, `.codex/auth.json`, or `.codex/config.toml` content appears in reports.
- [ ] Old M1 evidence is not presented as current runtime evidence.

## T8I EvidenceRerunWorker

| Field | Requirement |
| --- | --- |
| 发给谁 | EvidenceRerunWorker |
| 串行/并行 | Serial last |
| 依赖哪个 gate | T8H review has no blocking findings; test environment available; user explicitly approves evidence collection |
| Allowed files / paths | Read-only app/runtime/cluster inspection; optional write only to user-approved evidence path under `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/*.md`; browser screenshots/logs must be redacted |
| Forbidden files / paths | Business source writes; deployment writes; longterm writes; old M1 evidence reuse as runtime evidence; generated build artifacts in source scope; token/secret output; `.env`; `.codex/auth.json`; `.codex/config.toml`; `passes:true` or Close/write-back |
| Expected output | E2E evidence packet covering health, desktop regression, control-ui route, chat route, assets, reachability, wrapper deployed bundle, zh-CN/GTClaw rendering, protected literal preservation, and hashes |
| Verification command or evidence | Playwright E2E or recorded human E2E feedback plus route-specific evidence; `/control-ui/chat?session=main` must be independent evidence |

Minimum E2E coverage:

- [ ] `https://localhost:30443/healthz` reachable.
- [ ] GTManager login tested without exposing credentials/tokens.
- [ ] Desktop regression: `POST /api/v1/instances/:id/access?mode=desktop`; `/api/v1/instances/:id/proxy/` still renders desktop shell.
- [ ] Control-ui access: `POST /api/v1/instances/:id/access?mode=control-ui` returns expected contract without exposing token values.
- [ ] `/api/v1/instances/:id/control-ui/` renders `GTClaw 控制台`.
- [ ] `/api/v1/instances/:id/control-ui/chat?session=main` renders chat route and visible Chinese text.
- [ ] Static control-ui JS/CSS/font/image assets and SPA/history fallback load under `/control-ui/`.
- [ ] Runtime default path in clean/default profile renders Chinese for M1 user-visible surfaces.
- [ ] Runtime title, login gate, sidebar, breadcrumb, logo alt/title, and chat welcome show GTClaw or `GTClaw 控制台`.
- [ ] Same-namespace `podIP:<selected-control-ui-target-port>/` probe succeeds.
- [ ] `serviceClusterIP:<selected-service-port>/` probe succeeds and maps to selected pod target port.
- [ ] Exact command/output for historical failed `podIP:18789` probe is included, or explicit bridge-port replacement note is included.
- [ ] Required GTManager wrapper routes show GTClaw where runtime-facing while GTManager remains manager brand.
- [ ] Browser-loaded deployed asset URLs/hashes match intended frontend build.
- [ ] Protected OpenClaw technical literals are preserved by evidence scan and manifest.
- [ ] Patch manifest includes before/after file size and SHA-256.

Suggested prerequisite checks:

```bash
curl -sk https://localhost:30443/healthz
cd backend && go test ./...
cd frontend && npm run lint
rsync -a --delete --exclude node_modules --exclude dist frontend/ /tmp/gtclaw-e2e-frontend-build/
cd /tmp/gtclaw-e2e-frontend-build && npm ci && npm run build
kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml
kubectl apply --dry-run=client -f deployments/k8s/clawmanager.yaml
```

These checks are prerequisite evidence only. They do not replace runtime E2E.

If `/control-ui/chat?session=main` returns 404, if `/control-ui/` falls back to desktop, if assets fail under `/control-ui/`, or if deployed wrapper bundle is stale, do not proceed to Close gate. Record E2E as failed.

## T9 Close Gate: Conditions Only, No Close Execution

| Field | Requirement |
| --- | --- |
| 发给谁 | Commander / CloseGateChecker |
| 串行/并行 | Serial after T8I |
| 依赖哪个 gate | T8I E2E evidence exists and user explicitly asks for Close gate review |
| Allowed files / paths | Read-only review of T8H output, T8I evidence, patch manifest, delivery record, current spec/plan/tasks/design amendment |
| Forbidden files / paths | `longterm/workspace/feature_list.json`; `longterm/workspace/claude-progress.txt`; `docs/**`; `deployments/**`; `frontend/**`; `backend/**`; old M1 files; `.env`; `.codex/auth.json`; `.codex/config.toml`; any write-back before separate explicit user approval |
| Expected output | Close gate condition checklist only |
| Verification command or evidence | Read-only evidence checklist; no source or status write |

Conditions that must all be true before any later write-back prompt:

- [ ] T8I evidence packet exists and includes `/control-ui/chat?session=main` independent route evidence.
- [ ] T8I evidence packet includes desktop route regression evidence.
- [ ] T8I evidence packet includes control-ui root, assets, SPA/history fallback, and no desktop fallback evidence.
- [ ] T8I evidence packet includes wrapper and runtime GTClaw display evidence.
- [ ] T8I evidence packet includes default zh-CN rendered runtime evidence.
- [ ] T8I evidence packet includes podIP/Service reachability evidence for selected target port.
- [ ] T8I evidence packet includes deployed bundle asset URL/hash match evidence.
- [ ] T8I evidence packet includes protected OpenClaw technical literal preservation evidence.
- [ ] Patch manifest includes baseline, before/after hashes, changed display strings, preserved literals, ambiguous decisions, and rollback target.
- [ ] User explicitly approves a separate write-back prompt after reviewing evidence.
- [ ] Until that separate approval exists, do not update `longterm/**`, do not write `passes:true`, and do not state Close/complete/passed/accepted.

## Not Authorized By This Tasks Draft

- [ ] No frontend implementation.
- [ ] No backend implementation.
- [ ] No deployment edit.
- [ ] No docs edit.
- [ ] No longterm write-back.
- [ ] No runtime image edit.
- [ ] No cluster, pod, registry, or K8S mutation.
- [ ] No evidence rerun.
- [ ] No Close or durable status update.
