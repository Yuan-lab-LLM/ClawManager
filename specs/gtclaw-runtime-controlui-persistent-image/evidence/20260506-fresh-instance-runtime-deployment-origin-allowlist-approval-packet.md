# Fresh Instance / Runtime Deployment Origin Allowlist Approval Packet - 2026-05-06

Worker: FreshInstanceRuntimeDeploymentOriginAllowlistApprovalPacketWorker
Verdict: `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE`

Not `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_APPROVAL_PACKET_BLOCKED`: the required runtime image build/tag/push gate is complete, and the packet can ask for a narrow future gate to deploy the new image only through one isolated fresh OpenClaw instance and validate runtime listener/config/helper behavior.

This approval packet did not deploy, create or modify a fresh instance, run listener checks, run hash checks inside runtime, run browser E2E, mutate Kubernetes/runtime/database/registry state, build/tag/push/pull, restart backend, write Mem0, write longterm, set passes:true, run Close, stage, commit, or push.

## Approval Request

Please approve or reject the next Fresh Instance / Runtime Deployment Origin Allowlist Gate.

Recommended approval options:

- `APPROVE_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_GATE`: authorize the future gate with only the allowed scope below.
- `REJECT_OR_BLOCK`: do not create a fresh runtime instance; provide the blocking concern or revised scope.

No approval is implied by this packet.

## Published Image To Use

Dependency gate:

`RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE`

Published image:

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656` |
| image index digest | `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45` |
| linux/arm64 digest | `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97` |

Build validation already passed:

- node availability: `v22.22.2`
- helper scripts present with mode `755`
- `defaults/openclaw-agent/config.yaml` wrapper present
- `--bind lan` present
- `--auth token` present
- no unsafe YAML field for `allowedOrigins`, `controlUi`, or `gateway.controlUi`

## Future Gate Allowed Scope

If approved, the future Fresh Instance / Runtime Deployment Origin Allowlist Gate may do only the following:

- run read-only preflight checks for cluster, local registry, and backend health
- create exactly one new isolated fresh OpenClaw instance using the approved new in-cluster runtime image
- require at least `2Gi` memory for the fresh instance/runtime workload
- use only the normal product/backend path needed to create the fresh instance
- allow normal fresh-instance database writes only if explicitly required by the backend API flow
- verify the new runtime pod becomes Ready
- verify restart count and no OOMKilled state
- verify pod image and imageID/digest match the approved image
- verify the Service exposes ports `3001` and `18789`
- verify PodIP `18789` reachability
- verify ServiceIP `18789` reachability
- verify helper scripts exist in the pod image with mode `755`
- verify helper script hashes match the built image evidence
- verify `/defaults/openclaw-agent/config.yaml` markers and hash match the built image evidence
- verify `/config/.openclaw/openclaw.json` exists after startup
- verify `/config/.openclaw/openclaw.json` contains `gateway.controlUi.allowedOrigins` with `https://localhost:30443`
- verify no manual pod patch, no manual Service patch, and no `kubectl cp`
- write only the future gate evidence file

The future gate must not run browser E2E. Browser/Manual E2E requires a separate later approval.

## Future Gate Required Records

The future evidence must record:

- fresh instance id/name
- namespace
- pod name
- Service name
- pod image
- pod imageID
- image digest comparison against `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45` and `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`
- restart count
- OOMKilled / no OOM state
- Service ports, including `3001` and `18789`
- PodIP `18789` reachability result
- ServiceIP `18789` reachability result
- helper script presence, mode, and hashes or redacted summaries
- `config.yaml` markers and hash or redacted summary
- `/config/.openclaw/openclaw.json` redacted summary proving `gateway.controlUi.allowedOrigins` includes `https://localhost:30443`
- no secret values, no token values, no cookie values, no credentials, and no access URL plaintext

## Future Gate Explicitly Forbidden

Approval of this packet must not authorize:

- modifying existing instance `10`
- modifying existing instance `9`
- deleting existing instance `10`
- deleting existing instance `9`
- browser E2E
- Chrome DevTools MCP
- Playwright
- build/tag/push/pull
- docker build/tag/push/pull
- backend deploy/restart
- registry mutation
- database migration
- manual database writes except normal fresh-instance creation if explicitly required by backend API
- manual pod patch
- manual Service patch
- `kubectl cp`
- backend Origin rewrite
- modifying backend source
- modifying frontend source
- modifying deployments
- modifying docs
- modifying existing evidence
- modifying runtime startup artifact
- secrets/token/cookie/access URL plaintext output
- Mem0 write
- longterm write-back
- passes:true
- Close
- git stage/commit/push

## Required Runtime Validation Focus

The future gate should prove the startup artifact behavior in a real pod without using browser E2E:

1. The pod runs the approved image:
   - `gtclaw-controlui-persistent-origin-allowlist-20260506124656`
   - index digest `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
   - linux/arm64 digest `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`
2. The runtime remains healthy:
   - Ready pod
   - no OOM
   - stable restart count
   - Service exposes `3001` and `18789`
   - PodIP/ServiceIP `18789` reachable
3. The image content matches build validation:
   - node remains available
   - helper scripts exist with mode `755`
   - helper/config hashes match built image evidence
   - `--bind lan` and `--auth token` are present
   - no unsafe YAML field is present
4. The active runtime config has the allowlist:
   - `/config/.openclaw/openclaw.json`
   - `gateway.controlUi.allowedOrigins`
   - `https://localhost:30443`

## Gate Sequence After Approval

If approved:

1. Fresh Instance / Runtime Deployment Origin Allowlist Gate
2. Listener/hash review
3. Browser/Manual E2E approval

If the future gate cannot create exactly one isolated fresh instance, cannot use the approved image digest, cannot require `2Gi` memory, cannot verify helper/config/openclaw.json behavior without secrets, or would need to modify existing instance `10` or `9`, it must stop and write BLOCKED evidence.

## Explicit Negatives For This Packet

This approval packet performed:

- no deploy
- no fresh instance creation/deletion/modification
- no listener check
- no runtime hash check
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no build/tag/push/pull
- no backend deploy/restart
- no K8S/runtime/database mutation
- no registry mutation
- no database migration
- no manual database write
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no backend Origin rewrite
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no existing evidence modification
- no runtime startup artifact modification
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this approval packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist-approval-packet.md
rg -n "FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE|FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_APPROVAL_PACKET_BLOCKED|gtclaw-controlui-persistent-origin-allowlist-20260506124656|sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45|sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97|2Gi|18789|gateway.controlUi.allowedOrigins|https://localhost:30443|no browser E2E|no build/tag/push/pull|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist-approval-packet.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist-approval-packet.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist-approval-packet.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including verdict, image tag, image digests, `2Gi`, `18789`, `gateway.controlUi.allowedOrigins`, `https://localhost:30443`, `no browser E2E`, `no build/tag/push/pull`, `no passes:true`, and `no Close`. |
| secret-shape scan on this evidence | `1` | No secret-shaped matches. Matched values were suppressed by count-only scan. |
| final `git status --short -- ...` | `0` | Shows only this new approval packet as untracked in the requested evidence path scope. |
