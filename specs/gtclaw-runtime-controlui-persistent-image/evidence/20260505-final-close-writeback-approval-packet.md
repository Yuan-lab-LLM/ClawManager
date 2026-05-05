# Final Close / Longterm Write-back Approval Packet

Date: 2026-05-05

Worker: FinalCloseWritebackApprovalPacketWorker

Topology: serial

## Verdict

FINAL_CLOSE_WRITEBACK_APPROVAL_PACKET_DONE

This packet requests user approval or rejection for the next gate:

Final Close / Longterm Write-back Gate

This approval packet itself did not execute Close, did not write longterm, did not modify `feature_list.json`, did not write `passes:true`, did not write Mem0, and did not run browser E2E, runtime, K8S, database, registry, build, deploy, or browser mutation actions.

## Dependency Gate Summary

The final feature evidence chain is complete enough to request approval for close/write-back only.

| Evidence | Result used |
| --- | --- |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-browser-e2e.md` | `BROWSER_E2E_DONE`; GTManager `/control-ui` route HTTP `200`, non-502; WebSocket `101` plus frame traffic; `/proxy` desktop regression HTTP `200`; no token exposure; no token, cookie, credential, or access URL plaintext. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy.md` | `ISOLATED_2GI_FRESH_INSTANCE_RERUN_AFTER_BACKEND_DEPLOY_DONE`; fresh instance `10` / `oc2gi-185707`; pod `clawmanager-user-1/clawreef-10-oc2gi-185707`; Service exposed `3001` and `18789`; `18789` reachable; startup hashes matched; no OOM and no hostname/label assertion. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-plane-backend-build-deploy.md` | `CONTROL_PLANE_BACKEND_BUILD_DEPLOY_DONE`; `Deployment/clawmanager-app` ran backend image `clawmanager:control-plane-backend-gtclaw-20260505183733`; `/healthz` returned HTTP `200` with `ok`. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-ws-auth-bridge-implementation.md` | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE`; control-ui WebSocket bridge first-connect auth injection implemented and tested; browser auth material stripped for control-ui upstream use. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md` | `BACKEND_RUNTIME_LISTENER_FOLLOWUP_IMPLEMENTATION_DONE`; OpenClaw runtime hostname/label safety and Service `18789` exposure behavior tested. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md` | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE`; startup artifact set gateway bind/auth mode without hardcoded secret values. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-build-tag-push.md` and `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-registry-recovery-implementation.md` | Runtime image build produced the approved digest, initial registry push was blocked, then registry recovery completed and preserved final local image metadata for the approved digest. |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-ui-18789-a1-source-implementation.md` | Earlier A1 backend source evidence for Service `18789`, control-ui route behavior, token auth path, and desktop non-injection behavior. |

## Final Passing Evidence

| Field | Value |
| --- | --- |
| Browser E2E verdict | `BROWSER_E2E_DONE` |
| fresh instance | `10` / `oc2gi-185707` |
| fresh pod | `clawmanager-user-1/clawreef-10-oc2gi-185707` |
| backend image | `clawmanager:control-plane-backend-gtclaw-20260505183733` |
| runtime digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| GTManager control-ui | `/control-ui` HTTP `200`, non-502, not connection refused |
| WebSocket/session bootstrap | WebSocket HTTP upgrade status `101` plus frame traffic through backend bridge |
| desktop regression | `/proxy` desktop route HTTP `200`; desktop `3001` regression remains usable and is not replaced by control-ui |
| token hygiene | no token exposure; no token, cookie, credential, or access URL plaintext in browser evidence |

## User Approval Request

Please approve or reject whether to execute:

Final Close / Longterm Write-back Gate

Approval of this packet authorizes only the future gate described below. It does not authorize any immediate Close, longterm write-back, `passes:true`, or feature close action inside this packet.

## Future Gate Allowed Write Scope

If and only if the user approves the Final Close / Longterm Write-back Gate, that future gate may write only:

- `longterm/workspace/feature_list.json`
- `longterm/workspace/claude-progress.txt`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-final-close-writeback.md`

The future gate must mark feature `gtclaw-runtime-controlui-persistent-image` as passed/closed only after preserving exact fresh E2E evidence references, especially:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-browser-e2e.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-backend-deploy.md`
- instance `10` / `oc2gi-185707`
- pod `clawmanager-user-1/clawreef-10-oc2gi-185707`
- backend image `clawmanager:control-plane-backend-gtclaw-20260505183733`
- runtime digest `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`
- GTManager `/control-ui` HTTP `200`, non-502
- WebSocket `101` plus frame traffic
- `/proxy` desktop regression HTTP `200`
- no token exposure

## Future Gate Forbidden Scope

The future Final Close / Longterm Write-back Gate remains forbidden from:

- backend/frontend/runtime/deployments/docs/AgentTeam/spec/plan/tasks modification
- existing evidence modification
- build/tag/push/pull
- no build/tag/push/pull
- fresh instance creation/deletion/modification
- no fresh instance creation/deletion/modification
- K8S/runtime/database/registry/browser mutation
- no browser mutation
- manual pod/service patch
- kubectl cp write
- secrets, token, cookie, credential, or access URL plaintext output
- Mem0 write
- commit/stage/revert

## This Packet Explicit Non-actions

This approval packet itself performed only read-only evidence review and wrote this single packet file. It did not:

- modify `longterm/workspace/feature_list.json`
- modify `longterm/workspace/claude-progress.txt`
- write longterm
- write `passes:true`
- no passes:true
- execute Close
- no Close
- run browser E2E
- run Chrome DevTools MCP
- run Playwright
- run runtime, K8S, database, registry, build, deploy, or browser mutation actions

## Follow-up Order

If the user approves:

1. Final Close / Longterm Write-back Gate.
2. Record exact fresh E2E references in the allowed longterm files and final close/write-back evidence.
3. Only then mark feature `gtclaw-runtime-controlui-persistent-image` passed/closed.

If the user rejects:

1. Do not write `feature_list.json`.
2. Do not write `claude-progress.txt`.
3. Do not write `passes:true`.
4. Do not Close.

## Verification Results

Required static verification for this packet was run after writing this file:

```sh
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-final-close-writeback-approval-packet.md
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-final-close-writeback-approval-packet.md
rg -n "FINAL_CLOSE_WRITEBACK_APPROVAL_PACKET_DONE|FINAL_CLOSE_WRITEBACK_APPROVAL_PACKET_BLOCKED|BROWSER_E2E_DONE|/control-ui|WebSocket|101|/proxy|oc2gi-185707|no token exposure|feature_list.json|claude-progress.txt|no build/tag/push/pull|no browser mutation|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-final-close-writeback-approval-packet.md
secret-shape scan on this new evidence file
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-final-close-writeback-approval-packet.md longterm/workspace/feature_list.json longterm/workspace/claude-progress.txt
```

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-final-close-writeback-approval-packet.md` | `0` | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-final-close-writeback-approval-packet.md` | `1` | No output and no whitespace diagnostics; exit `1` is the expected no-index difference status for `/dev/null` vs this new file. |
| required marker `rg` scan | `0` | Required markers found, including `FINAL_CLOSE_WRITEBACK_APPROVAL_PACKET_DONE`, `BROWSER_E2E_DONE`, `/control-ui`, `WebSocket`, `101`, `/proxy`, `oc2gi-185707`, `no token exposure`, `feature_list.json`, `claude-progress.txt`, `no build/tag/push/pull`, `no browser mutation`, `no passes:true`, and `no Close`. |
| secret-shape scan on this new evidence file | `1` | No matches. |
| path-limited `git status --short` | `0` | Shows this new evidence file as untracked. Also shows pre-existing modifications to `longterm/workspace/feature_list.json` and `longterm/workspace/claude-progress.txt`; this approval packet did not write or modify those files. |
