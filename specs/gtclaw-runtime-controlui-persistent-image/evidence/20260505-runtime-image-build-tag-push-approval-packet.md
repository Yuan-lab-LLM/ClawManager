# Runtime Image Build/Tag/Push Approval Packet - 2026-05-05

## Verdict

`RUNTIME_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_DONE`

This packet is a request for user approval only. It records the reviewed parent image, reviewed startup artifact, intended build result, future command boundary, required evidence, and prohibitions for a later Runtime Image Build/Tag/Push Gate.

No build/tag/push/pull was executed in this approval packet.

## Approval Request

Please explicitly approve or reject whether the next gate may execute the Runtime Image Build/Tag/Push Gate from the reviewed startup artifact:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

If approved, the future gate may build a linux/arm64-only runtime image from that artifact root Dockerfile, tag only the approved host/in-cluster tag pair, push only to the local k3d registry target, and inspect the resulting image metadata. If rejected or not explicit, the future gate must remain blocked.

## Dependency Gate Record

| Gate | Status |
| --- | --- |
| WS Auth Bridge Implementation Gate | Commander reviewed as `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE`; `go test -count=1 ./internal/services ./internal/handlers` passed. |
| Runtime Source Artifact Recovery Gate | Commander reviewed as `RUNTIME_SOURCE_ARTIFACT_RECOVERY_DONE`. |
| Runtime Startup Artifact Implementation Gate | Commander reviewed as `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE`; evidence file is `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md`. |

## Parent Image Identity

The future build must use the verified persistent image as parent so the static control-ui localization patch lineage is preserved.

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |
| platform | `linux/arm64` |

Read-only local `docker image inspect` confirmed the host tag is present locally as `linux/arm64` and reports repo digest `localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`.

The in-cluster tag is the Kubernetes-facing registry alias for the same approved image lineage. The future build gate must not pull it by default; if the parent image is missing locally, the future implementation should report `RUNTIME_IMAGE_BUILD_TAG_PUSH_BLOCKED` rather than pulling without explicit approval.

## Reviewed Startup Artifact Identity

Artifact root:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

| Artifact path | Target image path | Mode | Size | sha256 |
| --- | --- | --- | ---: | --- |
| `Dockerfile` | build input only | `0644` | `323` | `f5f650318379eec4fe30f37942a5b8bb7919d394dde6d6680f380e775a6b844b` |
| `MANIFEST.md` | manifest only | `0644` | `2917` | `459c6226db012f80f80c975a1633671b9a36df464d3d6f06954a5bcb798ad833` |
| `defaults/openclaw-agent/config.yaml` | `/defaults/openclaw-agent/config.yaml` | `0644` | `785` | `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e` |
| `etc/services.d/openclaw-agent/run` | `/etc/services.d/openclaw-agent/run` | `0755` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

The startup delta is limited to `defaults/openclaw-agent/config.yaml`: it adds `--bind lan` and `--auth token` to `openclaw_command`. The `etc/services.d/openclaw-agent/run` file remains byte-for-byte unchanged from the recovered source. There is no hardcoded token value; token material remains runtime-provided outside this artifact.

## Intended Image Build Result

If the user approves the future Runtime Image Build/Tag/Push Gate:

- Build from the artifact root Dockerfile.
- Build for `linux/arm64` only.
- Keep the parent as the verified persistent image, preserving the static control-ui localization patch.
- Add only the startup delta for `--bind lan` and `--auth token`.
- Use no hardcoded token and no hardcoded secret value.
- Do not change backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, existing evidence, reviewed startup artifact files, or `/tmp` source/patch directories.

## Candidate Tag Naming

Exact timestamp must be generated only inside the future implementation gate after user approval.

| Tag role | Pattern |
| --- | --- |
| host tag pattern | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-<YYYYMMDDHHMMSS>` |
| in-cluster tag pattern | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-<YYYYMMDDHHMMSS>` |

The future implementation gate must record the exact host tag and in-cluster tag it uses.

## Future Build Gate Minimal Allowed Command Types

If and only if the user approves this packet, the future Runtime Image Build/Tag/Push Gate may use only these command types:

- `docker image inspect` for the parent image.
- `docker build` or `docker buildx build` from the reviewed artifact root for `linux/arm64`.
- `docker tag` only for the approved host/in-cluster tag pair if needed.
- `docker push` only to the local k3d registry target.
- `docker image inspect` for the output image.

The following remain forbidden unless separately approved:

- `docker run`
- `docker create`
- `docker cp`
- `docker export`
- `docker save`
- `docker pull`

If the parent image is missing locally, the future implementation should block rather than pull by default. A future packet may request pull approval only if it explicitly states the source, digest, purpose, and risk.

## Future Build Gate Required Output

The future Runtime Image Build/Tag/Push Gate must write evidence with:

- Exact build command.
- Exact host tag and exact in-cluster tag.
- image index digest.
- linux/arm64 manifest digest.
- Local registry push result.
- Output image inspect result with no secret values.
- Output image file hash verification plan for `/usr/local/lib/node_modules/openclaw/dist/control-ui` static files if the build gate can inspect safely without forbidden container/file-extraction actions.
- Output image file hash verification plan for `/defaults/openclaw-agent/config.yaml`.
- Output image file hash verification plan for `/etc/services.d/openclaw-agent/run`.
- Secret hygiene result: no token values, no cookie values, no credentials, no secret values, and no token-bearing URL.

If safe output image file inspection would require `docker run`, `docker create`, `docker cp`, `docker export`, or `docker save`, the future build gate must either use another already-approved safe inspection method or record that file hash verification is deferred to a separately approved gate.

## Explicit Prohibitions For This Approval Packet

- This approval packet does not execute build/tag/push/pull: no build/tag/push/pull.
- No runtime/K8S/database/registry/browser/fresh instance mutation.
- no browser E2E, no Chrome DevTools MCP, and no Playwright.
- No modification to backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, or existing evidence.
- No modification to the reviewed startup artifact.
- No modification to `/tmp/gtclaw-runtime-patch/**`.
- No modification to `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**`.
- No hardcoded token and no hardcoded secret.
- No Mem0 write.
- No longterm write.
- no passes:true.
- no Close.

## Future Gate Order

1. If the user approves this packet: Runtime Image Build/Tag/Push Gate.
2. Then isolated 2Gi+ fresh instance approval/implementation gate.
3. Then listener/hash verification gate.
4. Then browser E2E gate.
5. Only after fresh E2E evidence plus explicit user approval: allow consideration of passes:true, Close, and longterm write-back.

## Packet Verification Notes

Read-only verification performed for this packet:

- Reviewed `AGENTS.md`, `.specify/memory/constitution.md`, `backend/AGENTS.md`, `spec.md`, `plan.md`, `tasks.md`, and the named evidence files.
- Verified artifact output modes/sizes with `stat`.
- Verified artifact output sha256 values with `shasum -a 256`.
- Ran read-only `docker image inspect` on the parent host tag.
- Read `/tmp/gtclaw-runtime-patch/**` only to confirm the existing static control-ui patch lineage hashes.

This packet remains an approval request. It is not implementation evidence and it does not prove fresh instance behavior, listener reachability, or browser E2E behavior.
