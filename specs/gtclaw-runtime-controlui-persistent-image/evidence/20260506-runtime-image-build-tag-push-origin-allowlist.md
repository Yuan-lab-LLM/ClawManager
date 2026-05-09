# Runtime Image Build/Tag/Push Origin Allowlist Gate

Date: 2026-05-06
Worker: RuntimeImageBuildTagPushOriginAllowlistWorker
Verdict: RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE

## Timestamp And Tags

Timestamp: 20260506124656

Host tag:

`localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656`

In-cluster tag:

`k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656`

## Dependency And Parent Digest

Approved prerequisite:

- `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE`
- `APPROVE_RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_GATE`
- `RUNTIME_STARTUP_ARTIFACT_ORIGIN_ALLOWLIST_IMPLEMENTATION_RERUN_DONE`

Artifact root:

`specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth`

Artifact Dockerfile parent:

`localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`

Parent digest comparison:

- Dockerfile digest: `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`
- Local parent image inspect id: `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`
- Local parent RepoDigest: `localhost:5001/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10`
- Result: matched.

## Build Tag Push Results

docker build command:

```bash
docker build --pull=false -t localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656 specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260505-bind-lan-auth
```

docker build result:

- exit code: 0
- exported image index digest: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- exported linux/arm64 manifest digest: `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`
- build warning recorded: Dockerfile uses a constant `FROM --platform=linux/arm64` value.

docker tag command:

```bash
docker tag localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656 k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
```

docker tag result:

- exit code: 0
- in-cluster alias created locally.

docker push command:

```bash
docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
```

docker push result:

- exit code: 0
- pushed only to `localhost:5001`.
- registry digest: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- registry size: 856 bytes.

## Image Digest And Platform Inspection

docker image inspect result:

- host tag image id: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- in-cluster tag image id: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- OS/architecture: `linux/arm64`
- RepoDigest includes host registry digest `localhost:5001/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`.
- RepoDigest includes in-cluster registry alias digest `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`.

Registry manifest inspection:

- image index digest: `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`
- linux/arm64 digest: `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`
- attestation manifest digest: `sha256:fbc69109b4f947a7629fedcc2dce54487e557cc4e101d148f5387abb95f4194d`

## Non-Service Image Validation

node availability validation:

```bash
docker run --rm --entrypoint node localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656 --version
```

Result:

- exit code: 0
- node version: `v22.22.2`

helper scripts and config validation command:

```bash
docker run --rm --entrypoint /bin/sh localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656 -c '<non-service stat, sha256, and marker checks>'
```

Result:

- exit code: 0
- no OpenClaw runtime service was started; entrypoint was overridden to `node` or `/bin/sh` for non-service checks only.

helper scripts:

| Path | Mode | Size | sha256 |
| --- | ---: | ---: | --- |
| `/usr/local/bin/openclaw-ensure-controlui-origin` | 755 | 2009 | `c4151fa9a08ee04c41b212a9b30838f1f19d474fe50b6d2fdc848994d8fba071` |
| `/usr/local/bin/openclaw-gateway-with-origin-allowlist` | 755 | 201 | `79910c9dc6a0dcd0d809af1fc21a45052afeae66732d7a0ff4185089e8c3995c` |

config.yaml:

| Path | Mode | Size | sha256 |
| --- | ---: | ---: | --- |
| `/defaults/openclaw-agent/config.yaml` | 644 | 843 | `bdc8bf155539762c02f37ffbeb27e2dcec48bc5c3badaf4a17ec2edd6cd221c9` |

config.yaml marker proof:

- wrapper command present.
- `--bind lan` present.
- `--auth token` present.
- no unsafe YAML field `allowedOrigins`, `controlUi`, or `gateway.controlUi`.

## Explicit Negatives

This gate performed:

- no docker pull
- no deploy
- no backend deploy/restart
- no fresh instance creation/deletion/modification
- no K8S/runtime/database mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no registry mutation except the approved docker push to `localhost:5001` for this image
- no backend Origin rewrite
- no artifact/source/evidence modification except this new evidence
- no secret values
- no secret, token, cookie, credential, or access URL plaintext output
- no Mem0 write
- no longterm write-back
- no passes:true
- no Close
- no git stage/commit/push

## Recommended Next Gate

Recommended next gate:

1. Fresh Instance / Runtime Deployment Approval Packet.
2. Listener and hash checks after explicit deployment approval.
3. Browser/Manual E2E approval after runtime deployment evidence exists.

Do not deploy this image, create or modify an instance, run browser E2E, mark `passes:true`, Close, or commit without a separate approval gate.
