# Control-UI 18789 Fix Approval Packet - 2026-05-04

## Verdict

`CONTROL_UI_18789_FIX_APPROVAL_PACKET_DONE`

This is an approval packet only. It is a read-only fix design and implementation gate request. It does not authorize repair, source artifact edits, runtime image edits, build, tag, push, pull, fresh instance environment mutation, Kubernetes mutation, pod/container mutation, browser E2E, `passes:true`, or Close.

Status note: prior Browser E2E remained `BLOCKED` by upstream `502` because the runtime control-ui listener was not reachable through the pod/service-facing path. No browser E2E executed in this packet.

## Scope Statement

Allowed output written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-fix-approval-packet.md`

This packet only reviewed existing approved evidence and read-only backend proxy/service code. It did not modify backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, existing evidence, `/tmp/gtclaw-runtime-patch/**`, runtime pod/container files, runtime image/resource settings, Kubernetes resources, database rows, registry state, image tags, Mem0, `passes:true`, or Close state.

Required negative statements:

- no browser E2E executed
- no manual pod patch
- no passes:true
- no Close

## Dependency Gate Review

| Dependency | Status used |
| --- | --- |
| `20260504-control-ui-18789-root-cause.md` | Root cause completed and Commander-reviewed: `wrong listen address`. |
| `20260504-browser-e2e-chrome-devtools-mcp-rerun.md` | Chrome DevTools MCP Browser E2E rerun confirmed all three `/control-ui/` routes returned `502` with `connection refused`; `/proxy/` desktop regression passed. |
| `20260504-fresh-instance-mutation-and-pod-hash.md` | Fresh instance and pod hash gate completed; deployed pod hashes matched the four allowlist files; no manual pod patch. |
| `20260504-image-delivery-loop-rerun-after-baseline-decision.md` | Source artifact hash matched built image hash for the four allowlist files; fresh instance and browser behavior were later separate gates. |

## Root Cause Summary

Confirmed root cause: `wrong listen address`.

The current evidence shows:

- `openclaw-gateway` is running.
- `127.0.0.1:18789` returns HTTP `200` inside the runtime pod.
- `18789` listens only on loopback.
- PodIP `10.42.0.61:18789` is connection refused.
- ServiceIP `10.43.47.127:18789` is connection refused.
- Kubernetes Service selector, EndpointSlice, and `targetPort` are correct.
- GTManager proxy target correctly resolves to `10.43.47.127:18789`.
- `/proxy/` desktop regression passes.

Interpretation: the repaired control-ui static files are present in the fresh pod, and the backend proxy target is consistent with the Service topology. The blocker is that the runtime gateway is bound to loopback, while GTManager reaches the runtime through PodIP/ServiceIP.

## Fix Options

### Option A - Runtime Image / Startup-Level Listen Address Fix

Change the runtime image/startup configuration so `openclaw-gateway` listens on a pod/service-facing address for control-ui traffic, for example `0.0.0.0:18789`, or an equivalent supported configuration that exposes the same listener on PodIP and ServiceIP.

Expected behavior after a correct Option A fix:

- `127.0.0.1:18789` remains reachable.
- PodIP `10.42.0.61:18789` becomes reachable.
- ServiceIP `10.43.47.127:18789` becomes reachable.
- GTManager `/api/v1/instances/:id/control-ui/` can keep its current Service ClusterIP topology.
- Existing `/proxy/` desktop route stays on port `3001`.

Benefits:

- Minimal backend blast radius because current access/proxy code and Kubernetes Service target model remain valid.
- Aligns the runtime process with the Service already exposing `18789 -> 18789`.
- Directly fixes the failed network boundary identified by root cause evidence.

Risks / controls:

- Must identify the exact source artifact, runtime image build context, or startup config that controls the gateway bind/listen address.
- Must avoid broad OpenClaw-to-GTClaw rewrites and preserve OpenClaw Image technical identity.
- Must keep the existing four allowlist control-ui file hashes unchanged unless separately approved.
- Must not use a manual pod patch as acceptance evidence.

### Option B - Backend / Proxy Topology Fix

Change backend/proxy topology to reach control-ui without requiring the runtime listener to bind PodIP/ServiceIP.

Current viability: `not viable` under present evidence and topology.

Reason: the backend proxy runs outside the runtime pod network namespace. `127.0.0.1:18789` from the backend would refer to the backend pod itself, not the runtime pod's loopback. Current backend code resolves proxy targets through Kubernetes Service metadata and builds the upstream host from Service ClusterIP plus service port. Since the runtime only binds loopback, a backend-only change cannot directly access the runtime pod loopback unless a separately proven mechanism exists.

Option B could only become viable if a later design proves one of the following without violating scope:

- an in-pod bridge or sidecar exposes loopback `18789` on a pod/service-facing listener;
- an approved runtime startup process forwards PodIP/ServiceIP to loopback;
- a Kubernetes-native proxy pattern is explicitly approved and proven not to depend on inaccessible pod loopback;
- the backend can use an approved, secure, auditable transport that reaches the runtime pod network namespace without manual pod patching.

Those variants still require runtime/K8S topology changes and would have broader blast radius than Option A. Without that proof, Option B remains `not viable`.

## Recommendation

Recommended minimum blast-radius fix: Option A.

Implement the smallest runtime image/startup-level change that makes `openclaw-gateway` bind to a pod/service-facing listener, preferably `0.0.0.0:18789` if supported by the gateway configuration. This keeps GTManager's current control-ui route, Service ClusterIP target, token scope, and `/proxy/` desktop behavior unchanged.

The next implementation gate should first locate the bind/listen source of truth in the runtime image/startup context, then change only that setting. It should not modify backend proxy code unless Option A is proven impossible and a new approval packet authorizes a different topology.

## Actions Requiring Explicit User Approval

The following actions are not authorized by this packet and require a copy-ready approval from the user before execution:

1. Whether to modify the source artifact, runtime image build context, or startup config that controls `openclaw-gateway` bind/listen address.
2. Whether to rebuild, tag, and push a runtime image.
3. Whether to mutate the fresh instance test environment or runtime image/resource setting.
4. Whether to rerun pod hash verification plus Chrome DevTools MCP Browser E2E after the fix.

No approval is implied for unrelated backend/frontend/deployment/docs/longterm/AgentTeam changes.

## Required Verification Gates After Approved Fix

A later approved implementation must record all gates below before any `passes:true` or Close consideration:

| Gate | Required evidence |
| --- | --- |
| Listener evidence | `127.0.0.1:18789`, PodIP `:18789`, and ServiceIP `:18789` are all reachable. |
| No manual pod patch | The fresh instance is created from the approved artifact/config; no file copy, pod patch, restart repair, or `kubectl cp` is used as acceptance evidence. |
| Four-file hash preservation | Source artifact, built artifact, and fresh pod hash still match the four allowlist files: `index.html`, `assets/i18n-B06L7jQN.js`, `assets/zh-CN-B26mMdbY.js`, and `assets/index-M4TNVXB3.js`. |
| Browser E2E rerun | Chrome DevTools MCP rerun covers `/control-ui/`, `/control-ui/chat?session=main`, and history fallback. |
| Desktop regression | `/api/v1/instances/:id/proxy/` and `access?mode=desktop` still render desktop behavior and are not replaced by control-ui. |
| Secret hygiene | No token value, cookie value, credential, secret, `.env`, `.codex/auth.json`, `.codex/config.toml`, registry credential, or token-bearing URL is recorded. |

Expected listener matrix after fix:

| Probe | Expected result |
| --- | --- |
| `127.0.0.1:18789` | reachable, HTTP `200` or equivalent healthy control-ui response |
| PodIP `:18789` | reachable |
| ServiceIP `:18789` | reachable |
| `/proxy/` desktop | still passes |

## rollback Plan

rollback must be digest/resource identity based and must not rely on mutable tag-only rollback.

Required rollback path:

1. Restore the previous runtime image/resource setting through an explicitly approved control-plane path.
2. Use the prior approved digest/tag identity recorded in evidence, not a mutable tag alone.
3. If an image was rebuilt and pushed, keep the prior approved image index digest and linux/arm64 manifest digest available for rollback.
4. Verify a fresh runtime instance after rollback uses the prior approved digest/resource setting.
5. Verify the expected previous listener and four-file hash state without writing pod files.

Rejected rollback paths:

- mutable tag-only rollback;
- manual pod file repair as the only rollback mechanism;
- `kubectl cp` into a pod;
- restart or pod patch as a substitute for artifact rollback;
- rollback steps that expose tokens, cookies, registry credentials, or secrets.

## Secret Hygiene

This approval packet records no token value, cookie value, credential, secret, `.env`, `.codex/auth.json`, `.codex/config.toml`, registry credential, `Bearer` value, JWT-like value, or token-bearing URL.

If a later worker must inspect runtime environment variables, it may record only variable names and whether each variable exists. It must not record values.

## Copy-Ready Approval Template For Actual Fix Implementation Gate

Use this only if the user approves the actual fix implementation:

```text
Approved: RuntimeControlUI18789FixImplementation

Scope:
- Implement Option A only: runtime image/startup-level listen-address fix for openclaw-gateway.
- Goal: make openclaw-gateway expose control-ui on a pod/service-facing listener such as 0.0.0.0:18789 or an equivalent supported bind/listen configuration.
- Preserve GTManager backend proxy topology, Service ClusterIP target model, /control-ui/ route scope, and /proxy/ desktop behavior unless a new approval packet authorizes otherwise.

Allowed actions:
- Read and modify only the approved source artifact / runtime image build context / startup config needed to control openclaw-gateway bind/listen address.
- Rebuild, tag, and push the runtime image using an approved digest-pinned base and non-latest tag.
- Temporarily mutate the fresh instance runtime image/resource setting only for approved disposable verification.
- Create or use a fresh disposable runtime instance for verification.
- Run pod hash verification and Chrome DevTools MCP Browser E2E after the approved fix is deployed.

Required verification:
- Listener evidence: 127.0.0.1:18789, PodIP:18789, and ServiceIP:18789 are all reachable.
- no manual pod patch.
- Source artifact, built artifact, and fresh pod hash still match the four allowlist control-ui files.
- Chrome DevTools MCP Browser E2E passes for /control-ui/, /control-ui/chat?session=main, and history fallback.
- /proxy/ desktop regression still passes.
- rollback plan records prior approved digest/tag and does not rely on mutable tag-only rollback.
- Secret hygiene: no token/cookie/credential/secret values, .env, .codex/auth.json, .codex/config.toml, or token-bearing URLs recorded.

Forbidden actions:
- Option B backend/proxy topology changes unless Option A is proven impossible and separately approved.
- backend/frontend/deployments/docs/longterm/AgentTeam edits unless separately approved.
- manual pod patch, kubectl cp, in-pod file repair, or restart repair as acceptance evidence.
- broad OpenClaw-to-GTClaw replacement.
- mutable tag-only rollback.
- passes:true or Close before Commander/user review of fresh verification evidence.
```

## Verification Commands

Required verification for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-fix-approval-packet.md
rg -n "CONTROL_UI_18789_FIX_APPROVAL_PACKET_DONE|BLOCKED|wrong listen address|127\\.0\\.0\\.1:18789|PodIP|ServiceIP|0\\.0\\.0\\.0|openclaw-gateway|Option A|Option B|not viable|rollback|Chrome DevTools MCP|no manual pod patch|no browser E2E executed|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-fix-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-fix-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md
```
