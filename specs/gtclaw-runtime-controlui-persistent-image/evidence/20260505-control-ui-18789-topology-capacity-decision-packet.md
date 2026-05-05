# Control-UI 18789 Topology / Capacity Decision Packet - 2026-05-05

## Verdict

`TOPOLOGY_CAPACITY_DECISION_PACKET_DONE`

Current implementation verdict remains `CONTROL_UI_18789_FIX_IMPLEMENTATION_BLOCKED`. This packet is a read-only decision packet for choosing the next approval path. It performs no implementation, no runtime image setting change, no Kubernetes mutation, no database mutation, no registry mutation, no browser E2E, no passes:true, and no Close.

Recommended next approval path: **A1 - runtime bind/auth + control-plane Service generation + GTManager upstream token proxy + isolated capacity verification**.

Do not approve B as a first-line topology replacement, and do not treat C manual K8S harness as acceptance evidence.

## Scope Statement

Allowed output written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-topology-capacity-decision-packet.md`

Read-only inputs reviewed:

- `AGENTS.md`
- `.specify/memory/constitution.md`
- `backend/AGENTS.md`
- `specs/gtclaw-runtime-controlui-persistent-image/spec.md`
- `specs/gtclaw-runtime-controlui-persistent-image/plan.md`
- `specs/gtclaw-runtime-controlui-persistent-image/tasks.md`
- `20260504-control-ui-18789-root-cause.md`
- `20260504-control-ui-18789-fix-approval-packet.md`
- `20260505-control-ui-18789-fix-implementation.md`
- related feature evidence for fresh instance hash and browser E2E blocker
- backend source related to instance runtime settings, Service generation, proxy upstream selection, control-ui access tokens, and gateway token env injection
- deployment defaults related to ports, resource/quota defaults, and service exposure

No backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, existing evidence, runtime pod/container files, runtime image/resource setting, Kubernetes resource, database row, registry state, image tag, Mem0, passes:true, or Close state was modified.

Required negative markers:

- no implementation
- no browser E2E
- no passes:true
- no Close

## Dependency Gate Review

| Dependency | Result used |
| --- | --- |
| `20260504-control-ui-18789-root-cause.md` | `openclaw-gateway` served `127.0.0.1:18789` but did not listen on PodIP or a pod/service-facing address; Service/Endpoint metadata for the earlier target was valid; proxy upstream selected Service ClusterIP `:18789`. |
| `20260504-control-ui-18789-fix-approval-packet.md` | Option A was approved as the minimum runtime listen-address path; Option B backend/proxy topology change was not authorized and was classified not viable without a separate transport/topology proof. |
| `20260505-control-ui-18789-fix-implementation.md` | Runtime bind LAN without auth failed by OpenClaw policy; bind LAN with token auth progressed but the fresh disposable pod OOMKilled at `1Gi`; current fresh Services exposed only `3001/TCP`, not `18789/TCP`. |

## Decision Summary

| Question | Decision |
| --- | --- |
| Why did the fresh Service expose only `3001/TCP`? | Normal instance Service generation uses the runtime primary port `3001` and its `additionalServicePorts` helper does not add `18789` for OpenClaw. The proxy lazy path can request `18789`, but that is not the normal fresh instance Service shape. |
| Should the control plane expose `18789/TCP`? | Yes. For OpenClaw fresh instances, the lifecycle Service should expose desktop `3001/TCP` and control-ui `18789/TCP` without relying on manual Service patching or a later browser/proxy side effect. |
| What is the real GTManager `/control-ui` proxy upstream? | ServiceIP service port that maps to targetPort `18789`; in the normal canonical shape this is `ServiceIP:18789`. It is not backend `127.0.0.1:18789`, not PodIP by default, not desktop `3001`, and not `/proxy/`. |
| Does GTManager already have secure OpenClaw gateway token injection/forwarding? | No. It has route-scoped GTManager instance access tokens and separate instance gateway env for ClawManager LLM/agent use, but no proven server-side OpenClaw gateway auth token lookup and upstream header injection for `/control-ui`. |
| Is there a non-disruptive `2Gi` or higher test plan? | Yes, but it needs a new approval gate. Use isolated capacity: preferred separate disposable k3d/K8S test cluster or dedicated added node/capacity that does not stop, delete, resize, or retag non-task instances. |
| Minimum blast radius? | A1. Keep the existing ServiceIP proxy topology, add missing control-plane Service generation for OpenClaw `18789/TCP`, add server-side upstream token auth, and verify with enough isolated capacity. |

## Service Generation Decision

Fresh Service only exposed `3001/TCP` because the normal create/start path builds the Pod and Service from `runtimeConfig.Port`, which is `3001` for OpenClaw, then calls `additionalServicePorts(runtimeConfig.Port)`. That helper only returns extra ports for `3000` / `8082`; for `3001` it returns nil.

Source evidence:

- `backend/internal/services/instance_runtime.go`: OpenClaw uses the default desktop runtime port `3001`.
- `backend/internal/services/instance_service.go:313-320`: create path passes `ContainerPort: runtimeConfig.Port` and `AdditionalPorts: additionalServicePorts(runtimeConfig.Port)` to Service generation.
- `backend/internal/services/instance_service.go:479-488`: start path does the same when creating a missing Service.
- `backend/internal/services/instance_service.go:1053-1058`: `additionalServicePorts` adds ports only for `3000` or `8082`, not OpenClaw `18789`.
- `backend/internal/services/k8s/service_service.go:220-252`: Service generation can represent additional ports and names port `18789` as `control-ui`.
- `backend/internal/services/k8s/service_service_test.go`: tests already prove a requested Service can include both `3001` and `18789`, but normal lifecycle code does not currently request that pair for OpenClaw.

Decision:

- The control plane should expose `18789/TCP` for OpenClaw fresh instances as part of normal Service generation.
- The Service should keep `3001/TCP` for desktop and add `18789/TCP` for control-ui.
- Numeric targetPort `18789` is sufficient for Kubernetes routing, but declaring a named container port `control-ui` on the Pod would improve verifiability and named-target support.
- The proxy lazy path must not be the only way to add `18789/TCP`; relying on a browser request to create or patch Service shape is not acceptable for fresh-instance preconditions.
- Manual Service patch remains forbidden as acceptance evidence.

## Proxy Upstream Decision

Existing GTManager `/control-ui` access establishes a route-scoped control-ui token with `DefaultControlUITargetPort = 18789`. The proxy removes the `/api/v1/instances/:id/control-ui` prefix, sends upstream paths such as `/`, `/chat`, and `/history-fallback-check`, forces HTTP/WS scheme for control-ui, and builds the dial host from Kubernetes Service info.

Source evidence:

- `backend/internal/services/instance_access_service.go:29-35`: `DefaultControlUITargetPort` is `18789`.
- `backend/internal/services/instance_access_service.go:96-108`: control-ui scope is only for OpenClaw and uses route prefix `/api/v1/instances/:id/control-ui`.
- `backend/internal/services/instance_proxy_service.go:110-129`: proxy target path is derived from the control-ui route and target URL uses resolved service info.
- `backend/internal/services/instance_proxy_service.go:544-552`: control-ui upstream scheme is `http` / `ws`, not the desktop HTTPS upstream.
- `backend/internal/services/instance_proxy_service.go:564-570`: upstream host is `ServiceInfo.ClusterIP:ServiceInfo.ServicePort`, falling back to target port when needed.
- `backend/internal/services/k8s/service_service.go:319-337`: Service info is selected by targetPort, so `18789` means a Service port whose target resolves to `18789`.
- Root-cause evidence confirmed GTManager selected Service ClusterIP `10.43.47.127:18789` and that this was not the wrong proxy target.

Decision:

- Canonical proxy upstream: `http://<instance-ServiceIP>:18789/<stripped-control-ui-path>`.
- More generally, use ServiceIP plus the Service port that maps to targetPort `18789`; if a future Service port differs from targetPort, dial the Service port, not the Pod target port directly.
- Do not change to PodIP as the default topology. PodIP is restart-sensitive and bypasses the Service abstraction already used by GTManager.
- Do not change to backend `127.0.0.1:18789`; from GTManager that refers to the backend pod, not the runtime pod.
- Do not use desktop `3001` or `/proxy/` as a fallback for control-ui. Existing tests already expect no desktop fallback on control-ui upstream failure.

## Token Auth Decision

OpenClaw refused LAN binding without gateway auth. The second runtime attempt added supported token auth mode and progressed past that refusal, but GTManager has no proven secure way to inject or forward the OpenClaw gateway token to the `/control-ui` upstream.

Current GTManager security surfaces:

- Browser-to-GTManager route access tokens are generated by `InstanceAccessService`; they are scoped by mode, target port, route prefix, and cookie path.
- `GenerateAccessToken` returns the route token and stores it in an HttpOnly route-scoped cookie.
- `/control-ui` proxy validates the route token before proxying.
- `InstanceProxyService` copies incoming browser headers to upstream and removes only hop-by-hop headers and `Accept-Encoding`; it does not set a dedicated OpenClaw upstream auth header.
- `buildGatewayEnv` injects `CLAWMANAGER_INSTANCE_TOKEN`, `CLAWMANAGER_LLM_API_KEY`, and OpenAI-compatible variables for the instance gateway/LLM path. It does not inject `OPENCLAW_GATEWAY_TOKEN` or equivalent dedicated OpenClaw gateway auth material.

Decision:

- Existing GTManager access tokens are not a substitute for OpenClaw gateway token auth.
- Do not expose the OpenClaw gateway token to the browser, access API response, token-bearing URLs, logs, or evidence.
- Do not forward arbitrary browser `Authorization`, `Cookie`, or route access token material as the OpenClaw gateway credential.

Minimum compliant token auth design for A1:

1. Generate a dedicated per-instance OpenClaw gateway upstream token, or explicitly designate an existing per-instance token only after a security review confirms it is intended for OpenClaw gateway auth.
2. Store the upstream token server-side through a Kubernetes Secret or an approved secret-ref mechanism; do not hardcode it and do not store it in evidence.
3. Inject the same token into the runtime through the supported OpenClaw mechanism, such as `OPENCLAW_GATEWAY_TOKEN` or a secret-backed config field used by `openclaw gateway run --auth token`.
4. Teach GTManager proxy to resolve the per-instance upstream token server-side for control-ui only.
5. Before forwarding to OpenClaw, strip user-supplied upstream auth material that should not reach the runtime, then set the OpenClaw-required auth header, for example `Authorization: Bearer <redacted-token>` if that is the confirmed OpenClaw contract.
6. Apply the same upstream auth injection to WebSocket control-ui proxying.
7. Keep route-scoped GTManager access token validation as the browser-facing boundary; the upstream OpenClaw token is a backend-to-runtime secret only.
8. Add tests proving desktop tokens cannot access control-ui, control-ui route tokens are not forwarded as upstream OpenClaw credentials, and upstream auth is injected only for control-ui.

## Capacity Decision

The blocked implementation evidence shows:

- `1Gi` fresh disposable instance could schedule, but the token-auth LAN bind attempt OOMKilled at `1Gi`.
- A `2Gi` fresh disposable instance could not schedule in the current cluster because of insufficient node memory.
- Existing non-task OpenClaw instances were using significant memory; stopping or modifying them was not authorized.

Code evidence:

- `backend/internal/services/k8s/pod_service.go:59-68` sets both memory request and limit to `MemoryGB` Gi.
- `backend/internal/handlers/instance_handler.go` and `backend/internal/services/instance_service.go` allow a create request to specify `memory_gb` from `1` to `128`.
- Default user quota paths are high enough in normal admin/dev configuration; the observed blocker was node capacity, not API validation.

Decision:

- Do not force acceptance through `1Gi` if OpenClaw token-auth LAN mode OOMKilled.
- Do not stop, resize, delete, or repurpose non-task instances to free memory unless explicitly approved in a separate operational gate.
- The next verification should use `2Gi` or higher if that is the smallest resource class that can keep the runtime alive long enough to capture listener, ServiceIP, hash, and browser evidence.

Non-disruptive test environment options, in preferred order:

1. **Preferred:** create or use a separate disposable k3d/K8S test cluster with enough memory, deploy the current ClawManager stack and local registry there, and run the normal control-plane fresh instance flow at `2Gi` or higher. This leaves the current non-task instances untouched.
2. Add dedicated temporary cluster capacity, such as an added worker node or equivalent local capacity, then run the normal control-plane fresh instance flow without stopping existing instances. This still requires explicit K8S/capacity approval.
3. If neither is available, produce a capacity-blocked packet and do not run browser E2E or passes:true.

A manual K8S harness may be useful only to diagnose OpenClaw runtime bind/auth behavior under more memory. It cannot satisfy the feature acceptance gate because it bypasses normal control-plane Service generation, runtime image setting, and fresh instance lifecycle.

## Option Decision

### A1 - Runtime + Service Port + Auth Proxy + Capacity

Decision: **approve next, with explicit scope**.

This is the minimum blast-radius path because it preserves the already intended topology:

- runtime exposes control-ui on a pod/service-facing `18789/TCP` listener;
- control plane creates OpenClaw Services with both `3001/TCP` and `18789/TCP`;
- GTManager continues to proxy `/control-ui` through ServiceIP target `18789`;
- GTManager injects upstream OpenClaw token auth server-side;
- desktop `/proxy/` remains on `3001`.

Why A1 is now different from old Option A:

- Old Option A only covered runtime bind/startup.
- The implementation evidence proved two additional blockers: Service generation and token auth.
- A1 keeps the topology but authorizes the smallest missing control-plane changes needed to make that topology real.

### B - Backend / Topology Change

Decision: **do not approve as the next path**.

Reasons:

- Backend-only access to runtime `127.0.0.1:18789` is not possible from the GTManager pod.
- PodIP routing is more brittle than ServiceIP and bypasses the Service abstraction already used by proxy code.
- A backend topology rewrite would still need runtime auth handling and capacity verification.
- It has broader blast radius than A1 and does not reduce the known runtime requirement that OpenClaw requires auth for non-loopback binding.

B should only be reconsidered if A1 is proven impossible after a focused, evidence-backed implementation attempt.

### C - Manual K8S Harness

Decision: **diagnostic only, not acceptance**.

Allowed future use if explicitly approved:

- prove OpenClaw runtime can bind/auth/listen under `2Gi` or higher;
- test the exact OpenClaw gateway token header contract;
- collect runtime-only listener/process evidence.

Rejected use:

- no manual Service patch as acceptance;
- no direct pod patch/file repair as acceptance;
- no browser E2E acceptance through a hand-built Service that normal control-plane Service generation cannot reproduce;
- no passes:true or Close based on harness-only evidence.

## Next Approval Template

Use this only if the user approves A1 implementation and verification:

```text
Approved: ControlUI18789A1TopologyCapacityImplementation

Scope:
- Implement A1 only: runtime OpenClaw gateway pod/service-facing bind with token auth, normal OpenClaw fresh-instance Service generation for both 3001/TCP and 18789/TCP, and GTManager server-side upstream token auth injection for /control-ui.
- Preserve the existing GTManager /control-ui route model: browser -> GTManager route-scoped token/cookie -> GTManager proxy -> ServiceIP service port mapped to targetPort 18789.
- Preserve /proxy/ desktop behavior on 3001/TCP.
- Use an isolated capacity plan that does not stop, delete, resize, or mutate non-task instances.

Allowed actions:
- Modify only the approved runtime image/startup artifact needed for openclaw gateway bind/auth.
- Modify backend code/tests only where needed for OpenClaw Service generation, control-ui upstream token secret injection/lookup, and proxy auth header handling.
- Add or update focused backend tests for Service generation, proxy upstream, token auth, and no desktop fallback.
- Build, tag, push, and inspect a non-latest runtime image from the approved digest-pinned base.
- Temporarily set the approved runtime image/resource setting only for disposable verification, recording and restoring the previous setting.
- Use approved isolated capacity for a 2Gi or higher fresh disposable instance.
- Run listener probes, hash verification, and browser E2E only after pod/service preconditions are satisfied.

Required verification:
- Service generation: fresh OpenClaw Service exposes 3001/TCP and 18789/TCP without manual Service patch.
- proxy upstream: GTManager /control-ui dials ServiceIP service port mapped to targetPort 18789, with stripped control-ui paths.
- token auth: OpenClaw gateway token is injected server-side, not exposed to browser, logs, token-bearing URLs, or evidence; route tokens are not reused as upstream credentials unless explicitly approved by security review.
- listener evidence: 127.0.0.1:18789, PodIP:18789, and ServiceIP:18789 are reachable.
- capacity evidence: fresh disposable instance runs at 2Gi or approved higher value without OOMKilled.
- no manual pod patch, kubectl cp, manual Service patch, or restart repair as acceptance evidence.
- four control-ui file hashes remain the approved allowlist hashes.
- Chrome DevTools MCP browser E2E covers /control-ui/, /control-ui/chat?session=main, and history fallback.
- /proxy/ desktop regression remains desktop behavior.
- rollback records prior runtime image/resource setting and digest/resource identity.
- secret hygiene: no token/cookie/credential/secret values, .env, .codex/auth.json, .codex/config.toml, registry credentials, or token-bearing URLs recorded.

Forbidden actions:
- B backend/topology rewrite to PodIP, backend localhost, /proxy/, or desktop 3001 unless A1 is proven impossible and separately approved.
- manual K8S harness evidence as acceptance.
- stopping, resizing, deleting, or mutating non-task instances.
- deployment default changes unless separately approved with docs.
- broad OpenClaw-to-GTClaw replacement.
- mutable latest tag overwrite or mutable tag-only rollback.
- longterm write-back, Mem0 write, passes:true, or Close before Commander/user review of fresh E2E evidence.
```

## Stop / Escalation Criteria For Next Gate

The next implementation must stop and produce a new packet instead of continuing if any of these occur:

- OpenClaw token auth requires a header or token format that cannot be confirmed without exposing secrets.
- A1 requires changes outside runtime startup, backend Service generation, backend proxy auth, or focused tests.
- `2Gi` or approved higher isolated capacity is unavailable.
- fresh Service still exposes only `3001/TCP` after the Service generation change.
- runtime still OOMKilled before listener evidence.
- browser E2E would require a manual Service patch, pod file repair, or non-control-plane path.

## Verification Commands

Required verification for this decision packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-topology-capacity-decision-packet.md
rg -n "TOPOLOGY_CAPACITY_DECISION_PACKET_DONE|BLOCKED|3001/TCP|18789/TCP|Service generation|proxy upstream|token auth|2Gi|OOMKilled|no implementation|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-topology-capacity-decision-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-topology-capacity-decision-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-fix-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-fix-implementation.md
```
