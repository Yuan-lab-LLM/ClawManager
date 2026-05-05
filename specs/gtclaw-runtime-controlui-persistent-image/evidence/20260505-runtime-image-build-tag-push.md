# Runtime Image Build/Tag/Push Gate - 2026-05-05

## Verdict

`RUNTIME_IMAGE_BUILD_TAG_PUSH_BLOCKED`

The approved Runtime Image Build/Tag/Push Gate reached build, tag, and local image inspect, but did not complete the required local k3d registry push. The gate is blocked because `docker push` to `localhost:5001` exited nonzero with a local registry blob HEAD i/o timeout.

No docker pull, no docker run, no docker create, no docker cp, no docker export, and no docker save were executed.

## Scope Boundary

This gate used only the approved build/tag/push scope from:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-build-tag-push-approval-packet.md`

It wrote only this evidence file plus Docker local image/tag metadata produced by the approved gate. It did not mutate runtime, Kubernetes, database, browser state, or any fresh instance.

## Dependency Gate Record

| Gate | Status |
| --- | --- |
| Runtime Image Build/Tag/Push Approval Packet | `RUNTIME_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_DONE` |
| Runtime Startup Artifact Implementation Gate | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE` |
| WS Auth Bridge Implementation Gate | `WS_AUTH_BRIDGE_IMPLEMENTATION_DONE` |
| Runtime Source Artifact Recovery Gate | `RUNTIME_SOURCE_ARTIFACT_RECOVERY_DONE` |

## Generated Timestamp

Asia/Shanghai gate timestamp:

`20260505162033`

## Parent Image Inspect Result

Exact command:

```bash
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Exit: `0`

Output:

```text
RepoTags=["localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506"]
RepoDigests=["localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10"]
Id=sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10
Os=linux
Architecture=arm64
```

Parent identity matched the approval packet:

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |
| local inspect platform | `linux/arm64` |

## Artifact Input Identity

Artifact root:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

Exact commands:

```bash
stat -f 'path=%N mode=%OLp bytes=%z' specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/Dockerfile specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/etc/services.d/openclaw-agent/run
shasum -a 256 specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/Dockerfile specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/MANIFEST.md specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/defaults/openclaw-agent/config.yaml specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth/etc/services.d/openclaw-agent/run
```

Exit: `0` for both commands.

| Artifact path | Mode | Size | sha256 |
| --- | --- | ---: | --- |
| `Dockerfile` | `0644` | `323` | `f5f650318379eec4fe30f37942a5b8bb7919d394dde6d6680f380e775a6b844b` |
| `MANIFEST.md` | `0644` | `2917` | `459c6226db012f80f80c975a1633671b9a36df464d3d6f06954a5bcb798ad833` |
| `defaults/openclaw-agent/config.yaml` | `0644` | `785` | `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e` |
| `etc/services.d/openclaw-agent/run` | `0755` | `289` | `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda` |

The artifact contains startup command flags `--bind lan` and `--auth token`. No hardcoded token or secret value is present in the artifact.

## Output Tags

| Tag role | Exact tag |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |

## Build Result

Exact command:

```bash
docker build --platform linux/arm64 --pull=false -t localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth
```

Exit: `0`

Key output:

```text
#0 building with "desktop-linux" instance using docker driver
#1 WARN: FromPlatformFlagConstDisallowed: FROM --platform flag should not use constant value "linux/arm64" (line 1)
#2 [internal] load metadata for localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10
#5 [1/3] FROM localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10
#5 CACHED
#6 [2/3] COPY --chmod=0644 defaults/openclaw-agent/config.yaml /defaults/openclaw-agent/config.yaml
#7 [3/3] COPY --chmod=0755 etc/services.d/openclaw-agent/run /etc/services.d/openclaw-agent/run
#8 exporting manifest sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7 done
#8 exporting config sha256:97d58e39336da094472658199f592533595cbbac76c1344d51e32a9f68101c9a done
#8 exporting attestation manifest sha256:f7eb99c3c25679d1daae8e9976a9682ceca362c9afa7781da51435da6ba3448c done
#8 exporting manifest list sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9 done
#8 naming to localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 done
```

Build digest classification from build output:

| Field | Value |
| --- | --- |
| image index digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| linux/arm64 manifest digest | `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7` |
| config digest | `sha256:97d58e39336da094472658199f592533595cbbac76c1344d51e32a9f68101c9a` |
| attestation manifest digest | `sha256:f7eb99c3c25679d1daae8e9976a9682ceca362c9afa7781da51435da6ba3448c` |

## Tag Result

Exact command:

```bash
docker tag localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Exit: `0`

## Push Result

Exact command:

```bash
docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Exit: `1`

Failure summary:

```text
The push refers to repository [localhost:5001/clawmanager-openclaw/openclaw]
failed to do request: Head "https://localhost:5001/v2/clawmanager-openclaw/openclaw/blobs/sha256:78afb5093431c9e97265b8161a7d538f947425fb7ecde3ba29f19739b6b3b58f": dial tcp [::1]:5001: i/o timeout
```

This satisfies the blocker condition: local registry push target unavailable for the approved output tag. The gate did not retry through an alternate registry path, did not run registry diagnostics outside the approved command list, and did not perform any runtime/K8S/database/browser/fresh instance mutation.

## Output Image Inspect Result

Exact command:

```bash
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Exit: `0`

Output:

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Exact command:

```bash
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Exit: `0`

Output:

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Output image metadata:

| Field | Value |
| --- | --- |
| exact host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| exact in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| output image ID | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| platform | `linux/arm64` |
| image index digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| linux/arm64 manifest digest | `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7` |

The output image digest was determinable with allowed build and docker image inspect outputs. The push result remains blocked, so this digest is not accepted as completed local registry publication evidence.

## File-Level Output Hash Verification

File-level output hash verification is deferred. Verifying files inside the output image would require a separately approved safe method; this gate did not use docker run/create/cp/export/save.

Deferred file paths:

- `/usr/local/lib/node_modules/openclaw/dist/control-ui` static files, if a future gate can inspect safely.
- `/defaults/openclaw-agent/config.yaml`.
- `/etc/services.d/openclaw-agent/run`.

## Secret Hygiene

No secret value, token value, cookie value, credential, registry credential, access URL, `.env`, `.codex/auth.json`, or `.codex/config.toml` content was printed or written.

The words `token`, `--auth token`, `bootstrap_token`, and `OPENCLAW_GATEWAY_TOKEN` appear only as variable names, empty fields, or CLI mode names. No hardcoded token/secret values were used.

## Explicit Negatives

- no docker pull
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no runtime/K8S/database/browser mutation
- no fresh instance mutation
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec.md / plan.md / tasks.md modification
- no existing evidence modification
- no reviewed startup artifact modification
- no `/tmp/gtclaw-runtime-patch/**` modification
- no `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**` modification
- no manual pod patch
- no manual Service patch
- no `kubectl cp` write
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no Mem0 write
- no longterm write
- no passes:true
- no Close

## Recommended Next Gate

Because this gate is `RUNTIME_IMAGE_BUILD_TAG_PUSH_BLOCKED`, the immediate next action should be Commander/user review of the local registry push timeout before any fresh instance gate.

The next successful chain gate, after a future build/tag/push gate reaches `RUNTIME_IMAGE_BUILD_TAG_PUSH_DONE`, remains:

`Isolated 2Gi+ Fresh Instance Approval Packet`
