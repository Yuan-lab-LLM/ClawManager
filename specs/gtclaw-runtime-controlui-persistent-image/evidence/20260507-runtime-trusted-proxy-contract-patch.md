# Runtime trustedProxy contract patch evidence

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Implementation Patch

Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_CONTRACT_PATCH_BLOCKED: OpenClaw source workspace is missing the required writable runtime source files.

No implementation patch was applied. The gate instructions required STOP and BLOCKED evidence if `/Users/eduardogan/Desktop/GHJProject/opensparrow` did not contain the listed runtime source files.

## Required rule intake

Read-only project rule files checked:

- ClawManager `AGENTS.md`
- ClawManager `backend/AGENTS.md`
- OpenClaw source workspace `AGENTS.md`

OpenClaw source workspace path checked:

- `/Users/eduardogan/Desktop/GHJProject/opensparrow`
- `git rev-parse --show-toplevel` returned `/Users/eduardogan/Desktop/GHJProject/opensparrow`

## OpenClaw source workspace preflight

The required runtime source files were not present at the approved path:

- `src/gateway/protocol/schema/frames.ts`
- `src/gateway/auth.ts`
- `src/gateway/server/ws-connection/auth-context.ts`
- `src/gateway/server/ws-connection/connect-policy.ts`
- `src/gateway/server/ws-connection/message-handler.ts`
- `src/gateway/server/ws-connection/handshake-auth-helpers.ts`

Read-only checks performed:

- `ls -l` on the six paths returned no such file for each path.
- `find . -maxdepth 4 -path '*src/gateway*' -print` returned no matching gateway source tree.
- `rg --files | rg '(^|/)frames\.ts$|(^|/)auth\.ts$|(^|/)auth-context\.ts$|(^|/)connect-policy\.ts$|(^|/)message-handler\.ts$|(^|/)handshake-auth-helpers\.ts$'` returned no matches.
- `rg --files -g 'package.json'` returned no package scripts in the approved opensparrow workspace root.
- Path-limited OpenClaw status for the six required runtime files returned no entries because those paths are absent.

Because the approved workspace does not contain the writable runtime files, the runtime contract patch could not be implemented or verified in place. I did not patch `/tmp` raw cache files.

## Backend preflight

ClawManager path-limited status before evidence showed existing dirty files:

- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`

Those files were not edited by this gate. They may contain changes from another worker or an earlier session and were left intact.

The requested backend rewrite was not applied because the approved runtime source workspace failed the mandatory preflight. Applying only the backend rewrite would violate the dependency decision `CONTROLUI_WS_AUTH_SIGNATURE_ALIGNMENT_GPT_PRO_DECISION = REQUIRE_RUNTIME_CONTRACT_CHANGE`.

Backend behaviors therefore remain unclaimed by this gate:

- `params.auth` removal
- `params.device` removal
- `device.signature` removal
- `trustedProxy` marker injection
- JSON payload exclusion of OpenClaw gateway token material
- backend-to-runtime WS boundary auth alignment
- route query token and password stripping
- challenge forwarding before rewritten first connect
- fail closed coverage

## Verification performed

Backend Go verification was run from `backend/` because the Go module is at `backend/go.mod`.

Command:

```bash
go test -count=1 ./internal/services -run 'ControlUI|ProxyWebSocket'
```

Result:

```text
ok  	clawreef/internal/services	0.604s
```

Command:

```bash
go test -count=1 ./internal/services ./internal/handlers
```

Result:

```text
ok  	clawreef/internal/services	0.532s
ok  	clawreef/internal/handlers	0.617s
```

These passing backend tests are baseline checks only. They do not prove this blocked patch gate, because no backend or runtime implementation was applied here.

## Runtime verification

Runtime test verification was blocked by the missing approved source workspace:

- No required `src/gateway/...` runtime source files were available under `/Users/eduardogan/Desktop/GHJProject/opensparrow`.
- No package scripts were discoverable in that approved workspace by `rg --files -g 'package.json'`.
- No dependency install or network install was performed.

Runtime tests for `frames.ts`, `auth.ts`, `auth-context.ts`, `connect-policy.ts`, `message-handler.ts`, and `handshake-auth-helpers.ts` were therefore not run.

## Modified files

ClawManager/backend:

- No backend files were modified by this gate.

ClawManager/evidence:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-runtime-trusted-proxy-contract-patch.md`

OpenClaw/runtime:

- No OpenClaw runtime files were modified.

## Follow-up recommendation

Recommended next gate:

`CONTROLUI_OPENCLAW_RUNTIME_SOURCE_WORKSPACE_RECOVERY`

Purpose:

- Provide or materialize a writable OpenClaw runtime source workspace that contains the six approved `src/gateway/...` files.
- Confirm package scripts and the repo test layout for the required runtime tests.
- Re-run this implementation gate only after the approved workspace contains `frames.ts`, `auth.ts`, `auth-context.ts`, `connect-policy.ts`, `message-handler.ts`, and `handshake-auth-helpers.ts`.

## Forbidden actions statement

Forbidden actions were not executed: no build/tag/push, no browser E2E, no DevTools, no Playwright, no kubectl, no k3d, no Helm, no instance create/delete/modify, no database access or modification, no image build/tag/push/pull, no container run, no registry cleanup, no old session cleanup, no old asset cleanup, no old tag cleanup, no runtime startup artifact modification, no control-ui bundle artifact modification, no image assembly artifact modification, no frontend/deployments/docs/longterm/AgentTeam/UnifiedFramework modification by this gate, no existing evidence modification, no Mem0 write, no passes:true, no Close, and no git stage/commit/push.

No token, password, key, cookie, bearer material, auth header plaintext, or access URL plaintext was recorded.
