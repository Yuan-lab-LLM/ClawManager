# Official OpenClaw runtime source workspace recovery approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_OPENCLAW_OFFICIAL_RUNTIME_SOURCE_WORKSPACE_RECOVERY_APPROVAL_PACKET

## Verdict

CONTROLUI_OPENCLAW_OFFICIAL_RUNTIME_SOURCE_WORKSPACE_RECOVERY_APPROVAL_PACKET_DONE

This packet only prepares the approval decision for official OpenClaw source workspace recovery. It does not perform recovery and authorizes no implementation, no clone, no download, no extract, no workspace creation, no patch, no build, no image operation, no fresh instance, and no browser E2E.

## Dependency context

Dependency gates:

- CONTROLUI_OPENSPARROW_RUNTIME_SOURCE_DISCOVERY_DONE
- CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_BLOCKED
- CONTROLUI_OPENCLAW_RUNTIME_SOURCE_INTAKE_DONE

Evidence read:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-opensparrow-runtime-source-discovery.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-runtime-trusted-proxy-contract-patch.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-openclaw-runtime-source-intake.md`

## OpenSparrow exclusion summary

OpenSparrow was excluded as the writable runtime source workspace candidate for this patch sequence.

The OpenSparrow discovery evidence found:

- OpenSparrow remote branch/tag tips available for tree inspection did not contain the six required `src/gateway/...` TypeScript runtime source files.
- Local `/Users/eduardogan/Desktop/GHJProject/opensparrow` did not contain the six required files.
- Local OpenSparrow only showed packaged OpenClaw dist artifacts, declarations, and docs under vendor/node_modules and package copies.
- Those packaged artifacts showed connect schema, auth validator, and device signature validator clues, but they are not a suitable editable OpenClaw runtime/gateway TypeScript source workspace.

The blocked patch evidence also stopped because the approved OpenSparrow candidate did not contain the writable runtime source files. Therefore the next recovery attempt should use official OpenClaw source provenance rather than OpenSparrow.

## Official OpenClaw source provenance

Approved source target for a later recovery gate:

- Repository: `https://github.com/openclaw/openclaw.git`
- Repository shorthand: `openclaw/openclaw`
- Tag: `v2026.4.14`
- Peeled commit: `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170`
- Package provenance: `openclaw@2026.4.14`

Proposed destination for a later recovery gate:

- `/Users/eduardogan/Desktop/GHJProject/openclaw-runtime-v2026.4.14-workspace`

The later recovery gate should verify the destination safety before any clone/fetch/extract action. If the destination exists and is non-empty, that gate should stop and write BLOCKED evidence unless the Commander explicitly approves a separate handling plan.

## Required source files to verify

The later recovery gate must verify that the recovered workspace contains writable source files at:

- `src/gateway/protocol/schema/frames.ts`
- `src/gateway/auth.ts`
- `src/gateway/server/ws-connection/auth-context.ts`
- `src/gateway/server/ws-connection/connect-policy.ts`
- `src/gateway/server/ws-connection/message-handler.ts`
- `src/gateway/server/ws-connection/handshake-auth-helpers.ts`

It should also verify:

- `git remote -v`
- resolved `HEAD`
- tag or commit alignment with `v2026.4.14` / `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170`
- file existence and writability for the six required files
- package scripts and test layout discovery
- path-limited `git status` for those six files

## Approval options

APPROVE_OPENCLAW_OFFICIAL_RUNTIME_SOURCE_WORKSPACE_RECOVERY_GATE: approve official source recovery from `https://github.com/openclaw/openclaw.git` at `v2026.4.14` / `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170` into `/Users/eduardogan/Desktop/GHJProject/openclaw-runtime-v2026.4.14-workspace`, with destination safety preflight, six-file existence/writability verification, package script/test layout discovery, path-limited git status, and recovery evidence only.

REJECT_OR_BLOCK: <reason>

## Post-recovery boundary

Recovery does not authorize the runtime trustedProxy patch.

Even after official source recovery succeeds, the runtime trustedProxy contract patch must re-enter a separate patch gate. That later patch gate must explicitly authorize source edits, backend alignment, focused tests, and patch evidence.

This packet and the later recovery gate do not authorize build, image build/tag/push/pull, image extraction, fresh instance creation, browser E2E, Kubernetes operations, database access, instance mutation, passes:true, Close, or git stage/commit/push.

## Forbidden actions statement

Forbidden actions were not executed: no implementation, no patch, no clone, no download, no extract, no workspace creation, no OpenSparrow modification, no backend/frontend/runtime artifact/deployments/docs/longterm/AgentTeam/UnifiedFramework modification, no existing evidence modification, no image/container/registry/build/tag/push/pull action, no kubectl, no k3d, no Helm, no browser E2E, no DevTools, no Playwright, no database access or modification, no instance access or modification, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, auth header plaintext, or access URL plaintext was recorded.
