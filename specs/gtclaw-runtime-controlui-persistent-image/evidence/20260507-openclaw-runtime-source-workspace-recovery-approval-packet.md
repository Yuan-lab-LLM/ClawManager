# OpenClaw runtime source workspace recovery approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_OPENCLAW_RUNTIME_SOURCE_WORKSPACE_RECOVERY_APPROVAL_PACKET

## Verdict

CONTROLUI_OPENCLAW_RUNTIME_SOURCE_WORKSPACE_RECOVERY_APPROVAL_PACKET_DONE

This is an approval packet only. It authorizes no implementation, no clone, no download, no extract, no workspace creation, and no runtime trustedProxy patch.

## Dependency context

Dependency gates referenced:

- CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_BLOCKED
- CONTROLUI_OPENCLAW_RUNTIME_SOURCE_INTAKE_DONE
- APPROVE_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_GATE

Blocking evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-runtime-trusted-proxy-contract-patch.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-openclaw-runtime-source-intake.md`

## Blocker summary

The approved OpenClaw candidate workspace `/Users/eduardogan/Desktop/GHJProject/opensparrow` is a git root, but it does not contain the six writable runtime source files required by the patch gate:

- `src/gateway/protocol/schema/frames.ts`
- `src/gateway/auth.ts`
- `src/gateway/server/ws-connection/auth-context.ts`
- `src/gateway/server/ws-connection/connect-policy.ts`
- `src/gateway/server/ws-connection/message-handler.ts`
- `src/gateway/server/ws-connection/handshake-auth-helpers.ts`

The blocked patch evidence records that `ls`, `find`, and `rg --files` checks did not find the `src/gateway` runtime source tree at the approved path. The patch gate therefore stopped before backend or runtime implementation.

Read-only opensparrow status observed for this packet shows unrelated dirty files and untracked directories in the existing opensparrow workspace, but no recovery was attempted.

## Source provenance

Target runtime source provenance for recovery:

- Package: `openclaw@2026.4.14`
- Repository: `https://github.com/openclaw/openclaw.git`
- Tag: `v2026.4.14`
- Peeled commit: `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170`

The source intake evidence mapped the required files to the OpenClaw runtime gateway source for this version and confirmed that the runtime contract requires source-level changes rather than a backend-only patch.

## Recovery options

Option 1: User-provided existing writable local source path.

- User provides an absolute local path to an already existing OpenClaw runtime source checkout.
- The next recovery gate verifies that the path is a writable source workspace and contains all six required files.
- The recovery gate must record path-limited git status for the six files and package/test script discovery.
- No clone, no download, and no extract are needed for this option.

Option 2: Approved clone/fetch of `openclaw/openclaw` at `v2026.4.14` into a new directory.

- Commander explicitly approves the destination directory and network git operation.
- The recovery gate obtains source for `https://github.com/openclaw/openclaw.git` at tag `v2026.4.14` / peeled commit `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170`.
- The recovery gate must verify that the checkout contains the six required source files and records path-limited git status.
- This packet does not itself approve the clone/fetch action.

Option 3: Approved extract from a user-provided tarball into a new directory.

- User provides the tarball path and approves the destination directory.
- The recovery gate extracts into a new directory only after approval.
- The recovery gate verifies source provenance as far as the tarball metadata and contents allow, then confirms the six required files and records path-limited status or equivalent file-state evidence.
- This packet does not itself approve extraction.

## Post-recovery boundary

Recovery only prepares a writable runtime source workspace. After recovery, implementation is still not authorized by this packet.

The runtime trustedProxy contract patch must re-enter a separate patch gate before any source edits. That later patch gate must re-check the recovered workspace, update focused runtime and backend tests first, implement the hybrid trustedProxy contract and backend first-connect rewrite alignment, run the approved verification commands, and write separate patch evidence.

## Approval options

APPROVE_OPENCLAW_RUNTIME_SOURCE_WORKSPACE_RECOVERY_GATE: option 1, user provides an existing writable OpenClaw runtime source path containing the six required `src/gateway` files; recovery gate may verify that path read-only and record status, but still may not implement patch.

APPROVE_OPENCLAW_RUNTIME_SOURCE_WORKSPACE_RECOVERY_GATE: option 2, approve clone/fetch of `https://github.com/openclaw/openclaw.git` at `v2026.4.14` / `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170` into a Commander-approved new directory; recovery gate may create that workspace and verify it, but still may not implement patch.

APPROVE_OPENCLAW_RUNTIME_SOURCE_WORKSPACE_RECOVERY_GATE: option 3, approve extraction of a user-provided OpenClaw runtime source tarball into a Commander-approved new directory; recovery gate may create that workspace and verify it, but still may not implement patch.

REJECT_OR_BLOCK: <reason>

## Forbidden actions statement

Forbidden actions were not executed: no implementation, no patch, no clone, no download, no extract, no workspace creation, no backend/frontend/runtime artifact/deployments/docs/longterm/AgentTeam/UnifiedFramework modification, no existing evidence modification, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no database access or modification, no instance create/delete/modify, no image/container/registry action, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, auth header plaintext, or access URL plaintext was recorded.
