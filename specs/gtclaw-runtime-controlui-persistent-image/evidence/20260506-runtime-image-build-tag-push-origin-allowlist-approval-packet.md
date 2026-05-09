# Runtime Image Build/Tag/Push Origin Allowlist Approval Packet

Date: 2026-05-06
Worker: RuntimeImageBuildTagPushOriginAllowlistApprovalPacketWorker
Verdict: RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE

## Approval Request

Please approve or reject the next Runtime Image Build/Tag/Push Origin Allowlist Gate.

Approval would authorize a future worker to build a new OpenClaw runtime image from the reviewed startup artifact, tag it with a new unique timestamp tag, and push it only to the local k3d registry at `localhost:5001`. Approval would not authorize deploy, fresh instance creation, browser E2E, backend deploy/restart, K8S/runtime/database mutation, manual pod/service patching, `kubectl cp`, backend Origin rewrite, git stage/commit/push, `passes:true`, Close, longterm write-back, or Mem0 write.

Recommended approval options:

- `APPROVE_RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_GATE`: run the future build/tag/push gate with the minimum scope below.
- `REJECT_OR_BLOCK`: do not build/tag/push; provide the blocking concern or revised scope.

No approval is implied by this packet.

## Dependency Summary

Required dependency gate:

- evidence: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-startup-artifact-origin-allowlist-implementation-rerun.md`
- verdict: `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_RERUN_DONE`

Reviewed artifact root:

- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

Current reviewed artifact includes:

- `defaults/openclaw-agent/config.yaml` with wrapper command prepended before the existing OpenClaw gateway command.
- `usr/local/bin/openclaw-ensure-controlui-origin`.
- `usr/local/bin/openclaw-gateway-with-origin-allowlist`.
- `Dockerfile` `COPY --chmod=0755` entries for helper scripts.

Observed artifact facts:

- Dockerfile parent image uses `FROM --platform=linux/arm64 localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`.
- Dockerfile copies `defaults/openclaw-agent/config.yaml` with mode `0644`.
- Dockerfile copies helper scripts with mode `0755`.
- `config.yaml` keeps `--bind lan` and `--auth token`.
- `config.yaml` uses the wrapper command and does not add unsafe YAML field names such as `allowedOrigins`, `controlUi`, or `gateway.controlUi`.

Commander review facts:

- `git diff --check` passed.
- `bash -n` helper script checks passed.
- inline JavaScript `node --check` passed locally.
- no unsafe YAML field was found in `config.yaml`.
- secret-shape scan was clean.

Residual risks:

- future build gate must verify that the target runtime image has usable `node` for the helper.
- future runtime gate must verify lifecycle behavior because later config revisions may overwrite active JSON after startup.

## Future Allowed Scope

If the user approves `APPROVE_RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_GATE`, the future gate may do only the following:

- run `docker build --pull=false` from the reviewed artifact Dockerfile and artifact root
- tag the new image with a new unique timestamp tag
- push only to `localhost:5001` local registry
- inspect the resulting image digest and platform
- produce or inspect the in-cluster tag value needed for later deployment approval
- optionally run or inspect only minimal non-service validation needed to prove:
  - target image has usable `node`
  - helper scripts exist in `/usr/local/bin`
  - helper scripts are executable
  - `defaults/openclaw-agent/config.yaml` is present and has the expected wrapper command
- avoid starting the OpenClaw runtime service during build validation

The future gate must not deploy the image, update an instance, or mutate Kubernetes/runtime/database state.

## Future Required Checks

The future build gate must verify before accepting the image:

- parent digest in the build input matches the reviewed artifact Dockerfile digest.
- helper scripts copied into the image with mode `0755`:
  - `/usr/local/bin/openclaw-ensure-controlui-origin`
  - `/usr/local/bin/openclaw-gateway-with-origin-allowlist`
- `defaults/openclaw-agent/config.yaml` includes the wrapper command.
- `defaults/openclaw-agent/config.yaml` retains `--bind lan`.
- `defaults/openclaw-agent/config.yaml` retains `--auth token`.
- `config.yaml` has no unsafe YAML field `allowedOrigins`, `controlUi`, or `gateway.controlUi`.
- target image has usable `node`; otherwise the gate must stop as BLOCKED before accepting the image.
- no OpenClaw runtime service is started during build validation.
- no backend Origin rewrite is introduced.
- no secret, token, cookie, credential, or access URL plaintext is emitted.

## Future Evidence Requirements

The future build/tag/push evidence must record:

- final local tag
- in-cluster tag
- image index digest
- linux/arm64 digest
- helper file modes, sizes, and sha256 values
- `config.yaml` sha256
- parent digest comparison result
- exact build command
- exact tag command
- exact push command
- exact inspect commands
- node availability validation result
- helper presence/executable validation result
- no OpenClaw runtime service started
- no secret values

## Future Forbidden Actions

This approval packet does not authorize the future gate to do any of the following:

- docker pull
- backend deploy/restart
- fresh instance creation/deletion/modification
- K8S/runtime/database mutation
- browser E2E
- Chrome DevTools MCP
- Playwright
- manual pod patch
- manual Service patch
- `kubectl cp`
- modifying artifact/source/evidence except the future gate's own new evidence file
- backend Origin rewrite
- secret, token, cookie, credential, or access URL plaintext output
- Mem0 write
- longterm write-back
- `passes:true`
- Close
- git stage/commit/push

Registry boundary:

- The only registry write allowed by this proposed future gate is pushing the newly built runtime image to `localhost:5001` local registry.
- No remote registry push is allowed.
- No runtime deployment or system image setting change is allowed.

## Gate Sequence After Approval

If approved:

1. Runtime Image Build/Tag/Push Origin Allowlist Gate.
2. Fresh Instance / Runtime Deployment Approval Packet.
3. Listener and hash checks.
4. Browser/Manual E2E approval.

No deploy, fresh instance, browser E2E, `passes:true`, Close, commit, or longterm write-back may occur from this approval packet.

## Verification Plan

Required checks for this approval packet:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-origin-allowlist-approval-packet.md`
- `rg -n "RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE|RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_APPROVAL_PACKET_BLOCKED|Runtime Image Build/Tag/Push|origin allowlist|node|helper scripts|--bind lan|--auth token|no unsafe YAML field|no docker pull|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-origin-allowlist-approval-packet.md`
- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-runtime-image-build-tag-push-origin-allowlist-approval-packet.md`

## Explicit Negatives

This approval packet performed:

- no build/tag/push/pull
- no docker pull
- no deploy
- no backend deploy/restart
- no fresh instance creation/deletion/modification
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no K8S/runtime/database mutation
- no registry mutation
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no backend Origin rewrite
- no artifact/source modification beyond this approval packet
- no existing evidence modification
- no secret, token, cookie, credential, or access URL plaintext output
- no Mem0 write
- no longterm write-back
- no `passes:true`
- no Close
- no git stage/commit/push
