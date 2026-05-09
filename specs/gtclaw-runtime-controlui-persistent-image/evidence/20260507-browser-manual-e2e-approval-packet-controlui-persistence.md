# Browser / Manual E2E Approval Packet - Control UI Persistence

Verdict: `BROWSER_MANUAL_E2E_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE`

Packet date: 2026-05-07 Asia/Shanghai

## User Approval Options

To approve the next gate, reply with exactly:

`APPROVE_BROWSER_MANUAL_E2E_CONTROLUI_PERSISTENCE_GATE`

To reject or block the next gate, reply with:

`REJECT_OR_BLOCK: <reason>`

## Dependency Evidence Checked

This approval packet was prepared after read-only review of:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-controlui-persistence.md`

Confirmed dependency gates:

- `CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_DONE`
- `RUNTIME_IMAGE_BUILD_TAG_PUSH_CONTROLUI_PERSISTENCE_DONE`

## Current Running Instance For Future Browser / Manual E2E

| Field | Value |
| --- | --- |
| instance 15 | `15` / `oc2gi-cp-150002` |
| pod | `clawmanager-user-1/clawreef-15-oc2gi-cp-150002` |
| service | `clawreef-15-oc2gi-cp-150002-svc` |
| runtime image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712` |
| image index digest | `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908` |
| linux/arm64 digest | `sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a` |
| runtime state | Pod Ready, restart `0`, no OOM, status running |
| service ports | `3001` and `18789` |
| control-ui listener | `18789` HTTP `200` via loopback, PodIP, and ServiceIP |
| running-container path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` exists |

## Requested Future Browser / Manual E2E Gate

If approved, the future browser/manual E2E gate may use the current authorized GTManager browser session to validate the user-facing route for instance 15.

The future gate may verify:

- GTManager instance 15 page can open.
- The current instance is shown as `oc2gi-cp-150002`.
- Clicking `打开 GTClaw 控制台` enters `/api/v1/instances/15/control-ui` or a child route such as `/api/v1/instances/15/control-ui/chat?session=main`.
- The route remains tied to instance 15 and does not fall back to stale instance 10 or stale instance 11 WebSocket URL state.
- The control-ui does not display `来源不被允许`.
- The control-ui does not remain stuck at `disconnected 1006`.
- The user does not need to manually fill a WebSocket URL.
- The user does not need to enter a gateway token.
- The user does not need to enter a password.
- The visible page is usable GTClaw control-ui rather than desktop `/proxy/`, a 404 page, or a stale cached route.

If GTManager login is needed, the future gate may use only an existing authorized login state. It must not request, input, print, screenshot, or record any token, password, key, cookie value, credential, bearer value, auth header value, or access URL plaintext.

If browser storage inspection is needed, the future gate may read only diagnostic, non-sensitive key names and value shapes relevant to the current page. It must not clear storage, cache, or cookies. It must not record credential values, token values, password values, key values, cookie values, bearer values, auth header values, or access URL plaintext.

## Future Gate Stop Conditions

The future browser/manual E2E gate must stop with a BLOCKED verdict if any of the following occurs:

- The GTManager instance 15 page cannot be opened from the current authorized session.
- The `打开 GTClaw 控制台` action does not route to `/api/v1/instances/15/control-ui` or a valid instance 15 control-ui child route.
- The browser route uses a stale instance 10 or stale instance 11 WebSocket URL.
- The control-ui displays `来源不被允许`.
- The control-ui remains at `disconnected 1006`.
- The user is asked to manually fill a WebSocket URL.
- The user is asked to enter a gateway token or password.
- Evidence collection would require recording token, password, key, cookie, bearer, auth header, credential, or access URL plaintext.
- Browser storage/cache/cookie cleanup would be required to pass.

## Current Packet Boundary

This current approval-packet gate only writes this packet. It does not execute browser/manual E2E and does not authorize final acceptance by itself.

Current gate explicit negatives:

- no browser E2E
- no Chrome DevTools
- no Playwright
- no browser storage cleanup
- no browser cache cleanup
- no browser cookie cleanup
- no token/password/key input
- no backend source modification
- no frontend source modification
- no runtime-startup source modification
- no control-ui source modification
- no assembly artifact modification
- no deployment modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no kubectl mutation
- no k3d mutation
- no Helm mutation
- no backend API create/delete/modify instance
- no build/tag/push/pull
- no registry cleanup
- no old session cleanup
- no old asset cleanup
- no old tag cleanup
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push

## Verification Commands

Required checks for this evidence:

```bash
rg -n "BROWSER_MANUAL_E2E_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE|APPROVE_BROWSER_MANUAL_E2E_CONTROLUI_PERSISTENCE_GATE|CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_DONE|instance 15|oc2gi-cp-150002|/api/v1/instances/15/control-ui|stale instance|来源不被允许|disconnected 1006|WebSocket URL|gateway token|password|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-approval-packet-controlui-persistence.md
secret-shape scan with matched values suppressed
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-approval-packet-controlui-persistence.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| required marker `rg` scan | `0` | Required markers found, including DONE verdict, approval option, 2Gi dependency gate, instance 15, `oc2gi-cp-150002`, `/api/v1/instances/15/control-ui`, stale instance, `来源不被允许`, `disconnected 1006`, `WebSocket URL`, `gateway token`, `password`, `no browser E2E`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-browser-manual-e2e-approval-packet-controlui-persistence.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |
