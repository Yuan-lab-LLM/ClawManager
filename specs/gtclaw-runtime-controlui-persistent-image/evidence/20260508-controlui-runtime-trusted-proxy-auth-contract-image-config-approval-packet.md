# Control UI runtime trusted-proxy auth contract image/config approval packet

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, approval packet
Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_APPROVAL_PACKET

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_APPROVAL_PACKET_DONE

This is an approval packet only. It does not implement runtime source, does not edit startup/config artifacts, does not assemble or build a runtime image, does not push or pull images, does not deploy, and does not run browser E2E.

## Dependency gates

- CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_BLOCKED
- CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_DONE
- OPENCLAW_RUNTIME_AUTH_CONTRACT_SOURCE_INTAKE_DONE
- CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_RERUN_DONE

## Current facts

Backend delivery is complete:

- backend deployment: `clawmanager-system/deployment/clawmanager-app`
- backend image: `k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-auth-contract-backend-20260508115339`
- backend rollout: successful
- backend health: `/healthz` returned HTTP 200

Runtime readiness is blocked:

- target runtime: instance 16 / `oc2gi-loc-221427`
- runtime `18789`: HTTP 200
- runtime image: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942`
- current runtime config/startup does not enable trusted-proxy auth
- readiness result: NEEDS_RUNTIME_IMAGE_GATE

## Proposed next gate

`CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE`

Requested approval token:

`APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE`

The next gate must not proceed without that exact approval token.

## Proposed runtime patch/config/image scope

The next gate should patch or enable only the runtime trusted-proxy/device-less mediated Control UI auth contract and the minimum image/config packaging needed to run it.

Mandatory review scope:

- official OpenClaw gateway auth source for trusted-proxy mode and connect auth context;
- WebSocket first-connect schema and policy;
- device signature validation path;
- startup config materialization that selects runtime auth mode;
- runtime image assembly path that packages the patched OpenClaw source/config into the deployed image.

Allowed implementation scope for the next gate, if approved:

- add or enable runtime trusted-proxy acceptance for backend-mediated Control UI traffic;
- keep the proof of trust at the backend-to-runtime boundary, not in browser-controlled JSON alone;
- allow device-less mediated Control UI first-connect only after the trusted backend boundary is proven;
- package the runtime source/config change into a new runtime image tag;
- add focused runtime protocol/config tests and image evidence.

Out of scope for the next gate unless separately approved:

- frontend changes;
- ClawManager backend changes beyond read-only compatibility checks;
- desktop `/proxy` behavior changes;
- existing evidence edits;
- longterm write-back, passes:true, Close, or git stage/commit/push.

## Official OpenClaw source patch vs startup config patch

The next gate must decide from source inspection whether a config-only patch is sufficient.

If official OpenClaw already supports the exact required contract:

- patch only startup config/image packaging to enable trusted-proxy auth for the mediated runtime path;
- prove the running config selects trusted-proxy mode and applies trusted source/header validation.

If official OpenClaw does not support the exact required device-less mediated Control UI contract:

- patch official OpenClaw runtime gateway source in the approved runtime source workspace;
- add tests for schema, auth context, connect policy, and device signature bypass only for authenticated mediated traffic;
- package that patched source into a new runtime image.

Current dependency evidence indicates a startup/config change is likely required at minimum, and a source patch may be required if the existing trusted-proxy mode cannot coexist safely with the backend-mediated token boundary.

## Direct client spoofing proof

The next gate must prove a direct client cannot spoof trusted-proxy behavior.

Required negative tests:

- direct client sends a trusted-proxy-like field without the backend-to-runtime trusted boundary and is rejected;
- direct client sends a malformed trusted-proxy marker and is rejected;
- direct client omits device identity while lacking trusted-proxy boundary proof and is rejected;
- direct client mixes browser `params.auth`, `params.device`, or `device.signature` with a trusted-proxy marker and is rejected if the final contract forbids mixed mode;
- remote/source/header checks fail closed when the request does not originate from the approved backend boundary.

The trusted-proxy marker, if any exists in JSON, must be non-secret and must never be accepted as standalone proof.

## Direct flow regression protection

The next gate must keep direct token/password/pairing/device-signature flows from regressing.

Required positive and negative tests:

- direct token auth still succeeds with valid input;
- direct password auth still succeeds with valid input where that mode is configured;
- pairing or device-token retry behavior remains unchanged;
- valid direct device signature still succeeds;
- invalid direct device signature still fails with the expected auth failure;
- nonce mismatch and stale signature behavior remain unchanged;
- config-level trusted-proxy mode, if retained, remains covered by existing or new tests.

No server-owned credential material may be moved into browser-visible JavaScript, request URLs, screenshots, logs, or evidence.

## Fresh runtime instance gate

A fresh runtime instance gate is required after a runtime image/config patch.

Reason:

- the current instance 16 runtime image is already proven to lack trusted-proxy readiness;
- changing runtime source/config/image packaging does not alter an already-running runtime unless a new instance or approved runtime rollout uses the new image/config;
- readiness evidence must show the actual running runtime image/config contains the trusted-proxy/device-less mediated Control UI contract.

Recommended later gate:

`CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_GATE`

That later gate should create or select a fresh/current runtime instance only after the runtime image/config patch gate has produced a new runtime image and digest evidence.

## Browser rerun boundary

Browser rerun must wait until runtime image/config readiness is proven.

Reason:

- backend delivery is healthy, but the current runtime readiness result is NEEDS_RUNTIME_IMAGE_GATE;
- browser E2E before runtime readiness can only reproduce a known auth-contract blocker or produce ambiguous manual-connection evidence;
- browser evidence must test a route whose backend and runtime both contain the completed contract;
- stale route and stale runtime image ambiguity must be eliminated before browser/manual E2E.

Recommended later browser gate:

`CONTROLUI_TRUSTED_PROXY_AUTH_CONTRACT_BROWSER_MANUAL_E2E_GATE`

That gate should run only after backend delivery, runtime image/config readiness, and fresh/current runtime instance evidence all pass.

## Risks

- trusted-proxy spoofing if direct clients can claim mediated status without the backend-to-runtime proof;
- credential exposure if token, password, key, cookie, bearer material, auth header values, or access URLs are written into browser-visible surfaces or evidence;
- direct client regression for token/password/pairing/device-signature flows;
- config drift if runtime source supports trusted-proxy but startup packaging keeps token mode active;
- false readiness if an image is built but the tested runtime instance still runs the old runtime image;
- partial patch risk if device-less acceptance bypasses signature validation outside the mediated Control UI path.

## Rollback boundary

Rollback must be image/config scoped:

- keep the current working backend image available;
- publish runtime changes only under a new runtime image tag;
- do not mutate existing runtime image tags;
- do not edit existing runtime assembly artifacts in place;
- if trusted-proxy tests fail, stop before image build or deployment;
- if image readiness fails, stop before browser rerun;
- rollback means selecting the previous known runtime image/config in a separately approved runtime deployment gate, not deleting pods, Services, PVCs, records, sessions, or assets in this approval packet.

## Verification commands for this approval packet

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-image-config-approval-packet.md
rg -n "APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE|trusted-proxy|device-less|runtime image|direct client|spoof|fresh runtime instance|no browser E2E|no image build|no kubectl|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-image-config-approval-packet.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-image-config-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-image-config-approval-packet.md
```

## Verification results

```text
command=git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-image-config-approval-packet.md
exit=0
result=no whitespace errors
```

```text
command=rg required approval-packet markers
exit=0
result=required markers found, including approval token, trusted-proxy, device-less, runtime image, direct client, spoof, fresh runtime instance, no browser E2E, no image build, no kubectl, no passes:true, no Close, and no git stage/commit/push
```

```text
command=rg sensitive-shape scan against this evidence file
exit=1
result=no matches; sensitive_shape_match_count=0
```

```text
command=git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-image-config-approval-packet.md
exit=0
result=this approval packet is untracked in the scoped status output
```

## Forbidden actions confirmation

- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm write-back
- no AgentTeam modification
- no UnifiedFramework modification
- no runtime source modification
- no runtime artifact modification
- no runtime image assembly modification
- no image build
- no image tag
- no image push
- no image pull
- no container run
- no kubectl mutation
- no k3d
- no Helm
- no instance creation, deletion, or modification
- no pod, Service, or PVC creation, deletion, or modification
- no browser E2E
- no DevTools
- no Playwright
- no database mutation
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
