# Browser-only Certificate Confirmation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox syntax.

**Goal:** Chrome reuses one HTTPS hostname (the current manager IP or configured nip.io name) certificate approval across manager, native UIs and previews without installing certificates.

**Architecture:** Optional persistent same-host port registry, one shared nginx8444 listener, owner/generation-scoped preview bootstrap, strict browser-origin credential boundary, credentialless preview rendering pinned to one instance document.

**Tech Stack:** Existing Go/MySQL/upperdb, React/Vite, nginx/njs, Kubernetes NodePort, Chrome/Playwright. No new dependencies.

## Global Constraints

- No CA or client trust changes; fresh Chrome TLS verification enabled.
- No runtime lifecycle actions; instance118 generation9 and Pod UID stay unchanged.
- Existing deployments without new browser-origin configuration retain current behavior.
- Never recycle assigned public origin ports across instance identities.
- Only cookie-free OpenCode/DSH native UIs enter this mode; preview popups blocked.
- No plaintext capability or private TLS key in Git/logs.

### Task 1: Persistent origin port registry

Files: new `backend/internal/browserorigin/{registry.go,registry_test.go}`, `backend/internal/repository/browser_origin_repository{,_test}.go`, `backend/internal/db/migrations/102_add_browser_origin_ports.sql`.

Interfaces: `browserorigin.Config{ManagerOrigin string,FirstPort,LastPort int}`; `Binding{Port,InstanceID int,Kind,Target string}`; constants `Native="native"`, `Preview="preview"`; `Store.Bind(context.Context,int,string,string,int,int)(Binding,error)` and `Store.ByPort(context.Context,int)(Binding,error)`; `New(managerOrigin,portRange string,store Store)(*Registry,error)`; registry `Bind(ctx,instanceID,kind,target)`, `Lookup(ctx,requestHost)`, `Matches(requestHost)`, `URL(binding)`, `ManagerOrigin()`, `Range()`. Errors `ErrCapacity`, `ErrNotFound`, `ErrInvalid`. Store assigns one permanent binding per instance/kind/target; preview distinct targets max32; native target whitelistopencode/deepseek-harness; bounded range max512.

- [x] Write failing behavioral tests for stable binding, concurrent replica allocation, different-instance separation, range validation, exact hostname/port and pool/target exhaustion.
- [x] Implement registry and transaction-backed MySQL store. Preseed/lock bounded pool rows in ascending order; no advisory session locks or metadata ledger hacks. Compare current instance identity before binding; retain rows on deletion.
- [x] Run Go package tests and isolated MySQL concurrent allocation checks on test host; commit focused files.

### Task 2: Frontend browser scope and viewer

Files: existing frame surfaces, new small shared browser-origin helper, WorkbenchBrowser, InstanceDetailPage route scope, viewer route, focused browser tests.

Backend response contract: optional `browser_origin_mode:"chrome-single-host"` and `manager_origin` in native access/workbench capabilities; preview response additionally `viewer_url` (manager-origin external viewer), `credentialless:true`; existing `url` remains the scoped iframe bootstrap URL. Access URLs are permanent route origins with signed existing access query tokens. Mode opt-in only.

- [x] Write failing browser tests: credentialless preview, missing credentialless support, instance-to-instance full-document boundary, external viewer isolation, no preview popups, no extra confirmations, resize preserves native iframe.
- [x] Implement one shared scope helper and use backend metadata to enable it; otherwise preserve legacy behavior. External preview buttons use trusted viewer URL. Bootstrap URL opaque to UI; address bar retains user localhost URL.
- [x] Run focused tests, existing workbench/native-link/resize suites separately, tsc/Vite build; commit focused files.

### Task 3: Gateway integration and deployment

Files: new services browser-origin router/preview bootstrap helper with tests, native/preview forwarding, access URL construction, main.go optional config/router wiring, nginx additional8444 listener, operations docs and deployment patch example.

- [x] Add failing HTTP/WS tests for cookie stripping, Set-Cookie filtering, exact Origin/Fetch Metadata fences, bootstrap capability rejection, top-level preview wrapping, expiration and generation changes.
- [x] Integrate registry. Route browser origin before CM CORS/API router; call existing native authentication and proxy handlers after route-specific validation. Add native port binding as the final access URL projection for every access issuer, including IEI/share.
- [x] Reuse PreviewStore capability validation; bootstrap/cookie carries capability, port binding must match instance and target. Recheck owner/generation on streams. Manager viewer serves only static trusted wrapper; redirect top-level previews to it.
- [x] Add nginx8444 forwarding server, shared certificate and WS support; measure native JWT cookie size. Document pool limits and optional NodePort manifest.
- [x] Linux Go race/isolated MySQL tests, merged frontend suites and independent security review; fix all Important findings.
- [x] Build on54 over current manager image, snapshot fresh deployment/runtime UID baseline, create only owned128-port Service, deploy manager/env with bounded HTTPS readiness retry and rollback.
- [x] Real Chrome one-approval acceptance using actual118 localhost3000 read-only and owned fixtures; verify manager/native/preview isolation, source/file/Pod/generation preservation; save sanitized report.
