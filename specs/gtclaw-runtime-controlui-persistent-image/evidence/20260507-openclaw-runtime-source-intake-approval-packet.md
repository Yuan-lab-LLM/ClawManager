# OpenClaw runtime source intake approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_OPENCLAW_RUNTIME_SOURCE_INTAKE_APPROVAL_PACKET

## Verdict

CONTROLUI_OPENCLAW_RUNTIME_SOURCE_INTAKE_APPROVAL_PACKET_DONE

This approval packet is ready for Commander/user review. It does not approve or execute source intake by itself.

GPT Pro decision = REQUIRE_RUNTIME_CONTRACT_CHANGE

## Why source intake is needed

`CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_SOURCE_DISCOVERY_BLOCKED` found that the repo has GTManager backend proxy code, the minified control-ui client bundle, and runtime startup/image assembly artifacts, but not the OpenClaw gateway server source that validates the first WebSocket `connect`.

The current failure chain is already established:

- the browser reaches the correct GTManager route;
- stale route, origin allowlist, and disconnected 1006 are excluded;
- the control-ui still shows manual connection UI and `device signature invalid`;
- the accepted architecture requires GTManager as trusted proxy;
- backend-to-runtime uses the server-owned OpenClaw gateway token;
- browser auth remains route-scoped through GTManager token/cookie;
- the runtime contract must change unless current runtime source proves the trusted-proxy contract already exists.

Source intake is required before any patch because the actual runtime-side locations are unknown. The next gate must find the real runtime source for:

- connect schema
- connect auth validator
- device signature validation
- pairing/device-token retry
- trustedProxy validator
- origin/controlUi config usage if it affects the trusted proxy contract

## Source intake target

The intake target is the exact OpenClaw runtime/gateway source corresponding to the parent images documented by current artifacts:

- startup artifact parent image index digest: `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`
- startup artifact linux/arm64 manifest digest: `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e`
- control-ui persistence assembly parent image index digest: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- control-ui persistence assembly linux/arm64 digest: `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`

The intake should map those image artifacts back to source, then identify file paths and line numbers for the runtime gateway first-connect validator.

## Required user approval or provided input

One of these is required before the intake gate can start:

1. exact OpenClaw runtime/gateway source repo/path/tarball

   The user provides the exact source repository, local path, commit/tag, or tarball that built the relevant parent image. The next gate reads that source only and records provenance.

2. approval for read-only external source intake

   The user approves a read-only external source intake gate to fetch or inspect the specific OpenClaw runtime/gateway source. That future gate must still avoid implementation, secrets, deployment, browser, database, runtime, and image mutation.

3. approval to map source from corresponding parent image provenance

   The user approves a provenance-focused gate to inspect metadata or source mapping for the parent image. This option is for source provenance only; it does not authorize image unpacking, runtime mutation, rebuild, tag, push, pull, or registry action unless separately approved in that future gate.

## Approval options

APPROVE_OPENCLAW_RUNTIME_SOURCE_INTAKE_GATE: provide exact local source path, repo URL plus commit/tag, or tarball path for the OpenClaw runtime/gateway source matching the parent image provenance. If external read-only intake is approved, state the allowed source and network boundary explicitly.

APPROVE_OPENCLAW_RUNTIME_SOURCE_INTAKE_GATE: approve parent-image provenance source mapping only, with the exact parent image digest scope and any allowed metadata/provenance commands stated in the next gate before execution.

REJECT_OR_BLOCK: no exact OpenClaw runtime/gateway source or provenance path is available, or external/provenance intake is not approved.

## Guardrails for the future intake gate

The future intake gate must:

- perform source discovery only;
- identify runtime source files and line numbers for connect schema, connect auth validator, device signature validation, pairing/device-token retry, trustedProxy validator, and relevant origin/controlUi config usage;
- record provenance from user-provided source or approved external/provenance intake;
- avoid token/password/key/cookie/bearer/auth header/access URL plaintext;
- stop after evidence is written.

The future intake gate must not:

- implement patch;
- modify backend/frontend/runtime artifacts/deployments/docs/longterm/AgentTeam/UnifiedFramework;
- modify existing evidence;
- run browser E2E, DevTools, or Playwright;
- run kubectl/k3d/Helm/docker/build/tag/push/pull/registry commands unless a later user approval explicitly grants a narrower source-provenance read action;
- clone, download, or extract external source/image content unless the user approval option explicitly authorizes that exact source intake path;
- create/delete/modify instance;
- access or modify database;
- write Mem0;
- write passes:true;
- Close;
- git stage/commit/push.

## Boundary after source intake

Even after source intake succeeds, implementation remains blocked. The next successful intake evidence may only unlock a later patch approval gate, expected as:

`CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_APPROVAL_PACKET`

That later approval packet must name the exact runtime source write set, backend write set, startup/control-ui conditional write set, test plan, image/fresh-instance gates if needed, and secret-safety rules. No patch is authorized by this approval packet.

## Referenced evidence and artifacts

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-runtime-trusted-proxy-contract-source-discovery.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-ws-auth-signature-alignment-design.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-device-signature-invalid-root-cause.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260506-controlui-persistence/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md`

## Forbidden actions statement

Forbidden actions were not executed. Specifically: no implementation, no patch, no backend/frontend/runtime artifact/deployment/docs/longterm/AgentTeam/UnifiedFramework modification, no existing evidence modification, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no docker, no build/tag/push/pull, no registry operation, no clone, no download, no external source intake, no image extraction, no instance create/delete/modify, no database access or modification, no Mem0 write, no passes:true, no Close, no git stage/commit/push, and no token/password/key/cookie/bearer/auth header/access URL plaintext recording.
