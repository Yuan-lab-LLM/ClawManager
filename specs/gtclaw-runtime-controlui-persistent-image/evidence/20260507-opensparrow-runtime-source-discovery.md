# OpenSparrow runtime source discovery

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Source Discovery

Gate: CONTROLUI_OPENSPARROW_RUNTIME_SOURCE_DISCOVERY

## Verdict

CONTROLUI_OPENSPARROW_RUNTIME_SOURCE_DISCOVERY_DONE

OpenSparrow remote refs that are locally available for tree inspection, and the local `/Users/eduardogan/Desktop/GHJProject/opensparrow` directory, do not prove the presence of the required writable OpenClaw runtime source files. Local deep search found OpenClaw packaged dist artifacts and declarations under vendor/node_modules, but not the six TypeScript runtime source files or a suitable writable source workspace.

## Scope boundary

This gate did not use an opensparrow image and did not inspect, build, pull, or extract any image. It performed source discovery only.

The previous interrupted recovery target `/Users/eduardogan/Desktop/GHJProject/openclaw-runtime-v2026.4.14-workspace` exists, but it was not inspected or modified for this gate. No clone process was found still running before this discovery work continued.

## OpenSparrow repository state

Local candidate path:

- `/Users/eduardogan/Desktop/GHJProject/opensparrow`

Local git root:

- `/Users/eduardogan/Desktop/GHJProject/opensparrow`

Current local commit:

- `828e2b75a2d7e9248f2fe193954f0859b64b0498`

Current branch:

- `017-codespec-framework-adaptation`

OpenSparrow remote URL:

- `origin ssh://git@ssh.github.com:443/gegej1/opensparrow.git`

Remote metadata checked with read-only `git ls-remote`:

- Remote HEAD points to `refs/heads/main`.
- Remote branch refs observed: `017-codespec-framework-adaptation`, `feature/f-027-custom-routing-plugin`, `feature/p0-packaged-mac-diagnostics`, `main`, `release/1.0.0`, `review/chatgpt-pro-handoff-20260421`, `review/chatgpt-pro-handoff-docs-20260421`, `test/packaged-mac-deployment-debug-20260422`.
- Remote tag refs observed: `packaged-mac-latest-usable-20260427`, `v1.0.0`.
- No remote tag or branch named `v2026.4.14` was observed in OpenSparrow.

All advertised remote branch/tag object IDs checked in this gate had local trees available, so branch/tag tip tree discovery did not require fetch. No fetch was performed.

Local opensparrow status was dirty before discovery, with unrelated changes under docs, longrun, ui, and untracked packaged/status recovery paths. No opensparrow file was modified.

## Runtime source files searched

Required source paths:

- `src/gateway/protocol/schema/frames.ts`
- `src/gateway/auth.ts`
- `src/gateway/server/ws-connection/auth-context.ts`
- `src/gateway/server/ws-connection/connect-policy.ts`
- `src/gateway/server/ws-connection/message-handler.ts`
- `src/gateway/server/ws-connection/handshake-auth-helpers.ts`

Searches performed:

- `git ls-tree -r --name-only` on current HEAD and available remote/local refs for exact `src/gateway` paths.
- `find` for the six exact `src/gateway` paths across the local opensparrow filesystem.
- `find` and `rg` across vendor, node_modules, packages, apps, runtime, gateway, and openclaw-related paths for equivalent connect schema, auth validator, and device signature validator clues.

Exact required source file result:

- `frames.ts`: not found as `src/gateway/protocol/schema/frames.ts`
- `auth.ts`: not found as `src/gateway/auth.ts`
- `auth-context.ts`: not found
- `connect-policy.ts`: not found
- `message-handler.ts`: not found
- `handshake-auth-helpers.ts`: not found

Remote tree result:

- Existing OpenSparrow remote branch/tag tips do not contain the six required TypeScript runtime source files.
- Existing remote branch/tag tips contain wrapper/docs/vendor package paths such as `deploy/docker/bin/openclaw-wrapper.sh`, `deploy/docker/bin/prepare-runtime.sh`, `docs/runbooks/F-012-openclaw-runtime-upgrade.md`, and vendor OpenClaw package artifacts.

Local filesystem result:

- Local opensparrow does not contain the six required `src/gateway/...` TypeScript source files.
- Local opensparrow does contain packaged OpenClaw artifacts under vendor/node_modules and dist package copies.

## Equivalent validator clues found locally

The following packaged artifacts contain gateway connect schema / auth validator / device signature validator clues. They are not valid TypeScript source-workspace targets for the patch gate.

- `vendor/windows-openclaw/node_modules/openclaw/dist/method-scopes-B2vIWWxl.js:1418` has a compiled `ConnectParamsSchema` clue. File permission check: writable, but under vendor packaged dist.
- `vendor/windows-openclaw/node_modules/openclaw/dist/method-scopes-B2vIWWxl.js:1899` has a compiled connect schema validator clue. File permission check: writable, but under vendor packaged dist.
- `vendor/windows-openclaw/node_modules/openclaw/dist/gateway-cli-DzTv3_FS.js:40171` has a compiled `authorizeWsControlUiGatewayConnect` use. File permission check: writable, but under vendor packaged dist.
- `vendor/windows-openclaw/node_modules/openclaw/dist/gateway-cli-DzTv3_FS.js:40770` has a compiled `device signature invalid` validator clue. File permission check: writable, but under vendor packaged dist.
- `vendor/windows-openclaw/node_modules/openclaw/dist/gateway-cli-DzTv3_FS.js:41263` has a compiled `connect.challenge` clue. File permission check: writable, but under vendor packaged dist.
- `vendor/windows-openclaw/node_modules/openclaw/dist/auth-Buq0Niri.js:408` has a compiled auth validator implementation clue. File permission check: writable, but under vendor packaged dist.
- `vendor/windows-openclaw/node_modules/openclaw/dist/plugin-sdk/src/gateway/protocol/schema/frames.d.ts:8` declares `ConnectParamsSchema`. File permission check: writable, but it is a declaration artifact.
- `vendor/windows-openclaw/node_modules/openclaw/dist/plugin-sdk/src/gateway/auth.d.ts:61` declares `authorizeWsControlUiGatewayConnect`. File permission check: writable, but it is a declaration artifact.
- `vendor/linux-openclaw/bin/node_modules/openclaw/dist/discord-BGqJ05Bl.js:53461` has a compiled `ConnectParamsSchema` clue. File permission check: writable, but under vendor packaged dist.
- `vendor/linux-openclaw/bin/node_modules/openclaw/dist/model-selection-7OCRoBDT.js:61193` has a compiled `ConnectParamsSchema` clue. File permission check: writable, but under vendor packaged dist.

These findings show OpenSparrow carries packaged OpenClaw runtime outputs, docs, and declarations. They do not show that OpenSparrow remote or local contains the editable OpenClaw runtime/gateway TypeScript source tree needed for the trustedProxy contract patch.

## Discovery decision

OpenSparrow remote source judgment:

- Current advertised OpenSparrow remote branch/tag tips can be judged from existing local trees without fetch.
- Those refs do not contain the six required `src/gateway/...` runtime source files.
- Fetch is not needed to support this branch/tag-tip discovery conclusion.

Local opensparrow source judgment:

- The local opensparrow directory does not contain the required six TypeScript runtime source files.
- It contains packaged OpenClaw dist artifacts and declarations, which are not suitable patch sources.

Therefore OpenSparrow remote/local does not prove a writable OpenClaw runtime source workspace for this gate.

## Next gate recommendation

Recommended next gate:

`CONTROLUI_OPENCLAW_OFFICIAL_RUNTIME_SOURCE_WORKSPACE_RECOVERY_APPROVAL_PACKET`

Purpose:

- Approve a clean official OpenClaw source workspace recovery path for `openclaw@2026.4.14`, repo `https://github.com/openclaw/openclaw.git`, tag `v2026.4.14`, peeled commit `323493fa1b6adc1e10b9954a68d5eaa5a6ef1170`.
- Decide whether to reuse, inspect, or discard the interrupted previous recovery target in a separate gate. This discovery gate did not inspect or clean it.
- After a valid writable source workspace is recovered, re-enter the runtime trustedProxy contract patch gate. Recovery or discovery alone still authorizes no implementation.

## Forbidden actions statement

Forbidden actions were not executed: no implementation, no patch, no clone, no download, no extract, no fetch, no workspace creation, no OpenSparrow modification, no backend/frontend/runtime artifact/deployments/docs/longterm/AgentTeam/UnifiedFramework modification, no existing evidence modification, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no image/container/registry/build/tag/push/pull action, no database access or modification, no instance access or modification, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, auth header plaintext, or access URL plaintext was recorded.
