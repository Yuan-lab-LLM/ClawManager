# Fresh Instance / Runtime Deployment Approval Packet - Control UI Persistence

Verdict: `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE`

Packet date: 2026-05-06

## User Approval Options

To approve the next gate, reply with exactly:

`APPROVE_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_GATE`

To reject or block the next gate, reply with:

`REJECT_OR_BLOCK: <reason>`

## Dependency Evidence Checked

This approval packet was prepared after read-only review of:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-controlui-persistence.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-assembly-artifact.md`

Confirmed dependency gates:

- `RUNTIME_IMAGE_BUILD_TAG_PUSH_CONTROLUI_PERSISTENCE_DONE`
- `RUNTIME_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE`
- `RUNTIME_IMAGE_ASSEMBLY_ARTIFACT_DONE`
- `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_DONE` as historical process reference only.

This packet uses the new control-ui persistence runtime image below, not the historical origin-allowlist runtime image.

## New Runtime Image For Future Gate

- Runtime image for future fresh instance gate: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- Host tag: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- Image index digest: `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908`
- linux/arm64 digest: `sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a`
- Runtime control-ui target path proven in build evidence: `/usr/local/lib/node_modules/openclaw/dist/control-ui`

## Why Fresh Instance Gate Is Needed Before Browser / Manual E2E

The image build/tag/push gate proved that the new runtime image exists in the local registry and contains the patched control-ui files at the expected runtime path. It did not prove that a Kubernetes-created runtime instance can pull and run this new image, become Pod Ready, expose expected service ports, avoid OOM or restart loops, or serve runtime HTTP/control-ui endpoints.

Browser or manual E2E would be premature until a fresh disposable instance proves the runtime deployment path is healthy with this exact new image reference and digest. The next gate therefore needs explicit approval to create/use one fresh instance and collect runtime readiness evidence before any browser E2E approval packet.

## Future Gate Boundaries

If approved, the future fresh instance / runtime deployment gate may:

- Create/use one fresh disposable instance with `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`.
- Verify Pod Ready status.
- Verify restart count.
- Verify OOM status.
- Verify service ports.
- Verify runtime HTTP health and control-ui availability.
- Verify imageID/digest for the running pod.
- Verify runtime files/config presence, including `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- Record evidence for the disposable instance and the new persistence image only.

The future fresh instance gate does not authorize:

- no browser E2E.
- no Chrome DevTools or Playwright.
- no browser storage/cache/cookie cleanup.
- no gateway token/password/key entry.
- no passes:true.
- no Close.
- no longterm write-back.
- no Mem0 write.
- no git stage, commit, or push.
- no old asset cleanup, old session cleanup, old tag deletion, or registry cleanup.

## Current Packet Negative Proofs

This packet-writing gate performed only read-only dependency evidence checks and wrote this approval packet.

- No instance was created, deleted, or modified.
- No deploy/restart was executed.
- No kubectl/k3d/Helm mutation was executed.
- No K8S/runtime/database/registry mutation was executed.
- No browser E2E, Chrome DevTools, Playwright, browser storage cleanup, cache cleanup, or cookie cleanup was executed.
- No gateway token/password/key was requested or entered.
- No backend, frontend, runtime-startup-artifact, control-ui artifact, assembly artifact, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence was modified.
- No Mem0 write or longterm write-back was performed.
- No passes:true update was made.
- No Close action was taken.
- No git stage, commit, or push was performed.
- No old session or old asset cleanup was performed.

## Approval Boundary

Approval of `APPROVE_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_GATE` authorizes only the future fresh disposable instance/runtime deployment verification gate described above. It does not authorize browser E2E, browser/manual token entry, feature closure, longterm write-back, commit/push, or cleanup work.
