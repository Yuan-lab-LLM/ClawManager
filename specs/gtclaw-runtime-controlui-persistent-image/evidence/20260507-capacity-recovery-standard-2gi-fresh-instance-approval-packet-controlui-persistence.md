# Capacity Recovery / Standard 2Gi Fresh Instance Approval Packet - Control UI Persistence

Verdict: `CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE`

Packet date: 2026-05-07 Asia/Shanghai

## User Approval Options

To approve the next gate, reply with exactly:

`APPROVE_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_GATE`

To reject or block the next gate, reply with:

`REJECT_OR_BLOCK: <reason>`

## Dependency Evidence Checked

This approval packet was prepared after read-only review of:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-fresh-instance-runtime-deployment-controlui-persistence.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-controlui-persistence.md`

Confirmed dependency gates:

- `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_BLOCKED`
- `RUNTIME_IMAGE_BUILD_TAG_PUSH_CONTROLUI_PERSISTENCE_DONE`

## Background Facts From Blocked Gate

| Field | Value |
| --- | --- |
| blocked instance | `14` / `oc1gi-cp-143256` |
| blocked instance memory | `1Gi` |
| target image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712` |
| pod imageID digest | `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908` |
| expected image index digest | `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908` |
| registry linux/arm64 digest | `sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a` |
| Service ports | `3001` and `18789` were exposed |
| blocker | container was `OOMKilled` after about 13 seconds |
| exit code | `exit_code=137` |
| running-container control-ui path | `/usr/local/lib/node_modules/openclaw/dist/control-ui` was not verified because the pod was already Failed |

The blocked gate proved Kubernetes could pull and start the target image and that the running pod imageID digest matched the expected image index digest. It did not prove runtime HTTP/control-ui availability or running-container path presence because the 1Gi container failed with OOM.

The 1Gi path should not be retried. The next runtime deployment verification should use a standard 2Gi fresh disposable instance.

## Requested Future Gate

If approved, the future gate may do only the following:

- Release or expand enough capacity only for creating one standard 2Gi fresh disposable instance.
- If cleanup is needed, identify the specific old disposable, failed, or pending instance candidates in the future gate evidence before any cleanup action.
- If cleanup is needed, record candidate object names, namespaces, instance IDs when available, status, image reference, and why each candidate is safe to affect.
- Avoid generalized cleanup. No wildcard old-instance, old-session, old-asset, old-tag, or registry cleanup is authorized by this packet.
- Create exactly one standard 2Gi fresh disposable instance through the approved GTManager/ClawManager path.
- Use this runtime image for that 2Gi fresh instance:
  `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- Verify Pod Ready final state, restart count, OOM status, service ports, runtime HTTP/control-ui reachability, running-container control-ui path, and imageID/digest.
- Record whether the running container contains `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- Stop with a BLOCKED verdict if capacity recovery is not approved, if no safe candidate list exists for needed cleanup, if the 2Gi pod cannot schedule, if the 2Gi pod OOMs, if runtime HTTP/control-ui is unreachable, or if the running-container path cannot be verified.

## Future Gate Required Evidence

The future gate must record:

| Evidence | Required content |
| --- | --- |
| capacity action | exact action taken to release or expand enough capacity, or statement that no capacity action was needed |
| cleanup candidates if any | explicitly listed candidate objects before cleanup; no generalized cleanup |
| fresh 2Gi instance | instance ID/name, namespace, pod name, creation route, memory `2Gi`, and target image |
| Pod Ready | final Ready condition and phase |
| restart/OOM | restart count, container state, last state, OOMKilled status, exit code if terminated |
| imageID/digest | running pod image, imageID, expected index digest, expected linux/arm64 digest reference |
| service ports | Service and EndpointSlice ports for `3001` and `18789` |
| runtime HTTP/control-ui | PodIP or ServiceIP reachability for runtime HTTP/control-ui without browser E2E |
| running-container path | proof that `/usr/local/lib/node_modules/openclaw/dist/control-ui` exists in the running container |
| secret hygiene | no token value, password value, key value, cookie value, credential, bearer value, auth header value, or access URL plaintext |

## Current Packet Boundary

This packet only requests approval for the future capacity recovery / standard 2Gi fresh instance gate. It does not authorize that future gate by itself.

This current packet did not execute and does not authorize:

- no kubectl mutation
- no k3d mutation
- no Helm mutation
- no backend API create/delete/modify
- no old instance cleanup
- no old session cleanup
- no old asset cleanup
- no old tag cleanup
- no registry cleanup
- no build/tag/push/pull
- no deploy/restart
- no browser E2E
- no Chrome DevTools
- no Playwright
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
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push

## Current Packet Negative Proofs

This packet-writing gate performed only read-only dependency evidence checks and wrote this new approval packet.

- No Kubernetes resource was created, updated, patched, deleted, restarted, or scaled.
- No k3d cluster, node, server, registry, or load balancer action was executed.
- No Helm action was executed.
- No backend API call was made to create, delete, modify, stop, start, or restart any instance.
- No old instance, session, asset, tag, registry content, or evidence was cleaned up.
- No image build, tag, push, pull, deploy, or restart was executed.
- No browser E2E, DevTools, Playwright, browser storage cleanup, cache cleanup, or cookie cleanup was executed.
- No token, password, key, cookie, credential, bearer value, auth header value, or access URL plaintext was requested, entered, or recorded.
- No source, deployment, docs, longterm, AgentTeam, spec, plan, tasks, assembly artifact, control-ui artifact, or existing evidence file was modified.
- No Mem0 write, `passes:true`, Close, git stage, commit, or push was performed.

## Verification Commands

Required checks for this evidence:

```bash
rg -n "CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE|APPROVE_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_CONTROLUI_PERSISTENCE_GATE|BLOCKED|OOMKilled|exit_code=137|2Gi|gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712|sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908|sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-approval-packet-controlui-persistence.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-approval-packet-controlui-persistence.md
secret-shape scan with matched values suppressed
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-approval-packet-controlui-persistence.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| required marker `rg` scan | `0` | Required markers found, including the DONE verdict, approval option, BLOCKED dependency, `OOMKilled`, `exit_code=137`, `2Gi`, target tag, image index digest, linux/arm64 digest, `no browser E2E`, `no passes:true`, and `no Close`. |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-approval-packet-controlui-persistence.md` | `0` | No whitespace errors. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-approval-packet-controlui-persistence.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |
