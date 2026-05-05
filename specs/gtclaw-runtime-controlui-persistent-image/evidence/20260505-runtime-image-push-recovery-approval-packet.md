# Runtime Image Push Recovery Approval Packet - 2026-05-05

## Verdict

`RUNTIME_IMAGE_PUSH_RECOVERY_APPROVAL_PACKET_DONE`

This packet requests user approval for a later Runtime Image Push Recovery Gate. It does not run diagnostics, does not retry push, and does not mutate registry, runtime, Kubernetes, database, browser state, or any fresh instance.

## Approval Request

Please explicitly approve or reject whether the next gate may execute:

`Runtime Image Push Recovery Gate`

Default requested recovery scope:

- Run minimal read-only registry and local image diagnostics listed below.
- Prefer retry publishing the existing local image/tag after read-only registry health checks.
- Do not rebuild.
- Do not retag unless explicitly needed and approved.
- Do not proceed to fresh instance work until successful publication evidence exists.

Additional explicit approval request:

If read-only diagnostics show that `localhost:5001` IPv6/HTTPS behavior is the likely blocker, please also approve or reject whether the future recovery gate may create and push an additional `127.0.0.1:5001` host alias for the same existing local image digest.

Without explicit approval, the future recovery gate must not use the `127.0.0.1:5001` alias path.

## Dependency Gate Record

| Gate | Reviewed status |
| --- | --- |
| Runtime Image Build/Tag/Push Gate | `RUNTIME_IMAGE_BUILD_TAG_PUSH_BLOCKED` |
| Runtime Image Build/Tag/Push Approval Packet | `RUNTIME_IMAGE_BUILD_TAG_PUSH_APPROVAL_PACKET_DONE` |
| Runtime Startup Artifact Implementation Gate | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE` |

The blocked gate evidence is:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-build-tag-push.md`

## Blocked Push Facts

| Field | Value |
| --- | --- |
| timestamp | `20260505162033` |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| local image index digest | `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` |
| linux/arm64 manifest digest | `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7` |
| push command | `docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` |
| push result | exited `1` with blob HEAD i/o timeout |

Failure summary recorded from the blocked evidence, with the local registry endpoint shape summarized rather than expanded as a URL:

```text
failed to do request: Head against localhost:5001 registry blob sha256:78afb5093431c9e97265b8161a7d538f947425fb7ecde3ba29f19739b6b3b58f: dial tcp [::1]:5001: i/o timeout
```

Local build, local tag, and local image inspect succeeded before the blocked push. Registry publication did not complete, so this image must not be used as published runtime evidence yet.

## Future Recovery Gate Default Target

The future recovery gate should target publication recovery only:

- Do not rebuild.
- Do not retag unless explicitly needed.
- First prefer retry publishing the existing local image/tag after read-only registry health checks.
- Keep the existing approved host tag as the default publication target:
  `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033`
- Keep the existing in-cluster tag as the Kubernetes-facing label only until publication succeeds:
  `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033`
- If `localhost:5001` IPv6/HTTPS behavior appears to be the blocker, the future gate may use an additional `127.0.0.1:5001` host alias only if the user explicitly approves that alias path.

## Future Recovery Gate Minimal Read-Only Diagnostics To Request

The future gate may request these read-only diagnostics:

- `docker image inspect` for the existing output tags.
- `docker ps` with a registry-container filter, read-only.
- `docker logs --tail` with a limited line count for the registry container, read-only, only if the container exists.
- `curl` `GET` or `HEAD` `/v2/` against `localhost:5001` and `127.0.0.1:5001`, only if no token, cookie, secret, credential, or access URL is printed.

The future diagnostics must not perform K8S write, registry config mutation, registry restart, runtime mutation, database mutation, browser E2E, or fresh instance mutation.

## Future Recovery Gate Minimal Mutation To Request

If the user approves the future recovery gate, the minimum mutation request is:

- `docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` for the existing approved host tag.

Optional alias mutation, only if explicitly approved:

- `docker tag` the existing local image digest to:
  `127.0.0.1:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033`
- `docker push` that `127.0.0.1:5001` alias.

The alias path must remain tied to the same local image index digest:

`sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`

## Explicit Prohibitions For This Approval Packet

- This approval packet does not execute diagnostics.
- This approval packet does not execute push retry.
- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no registry config mutation/restart
- no K8S/runtime/database/browser mutation
- no fresh instance mutation
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no backend modification
- no frontend modification
- no deployments modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec.md / plan.md / tasks.md modification
- no existing evidence modification
- no reviewed startup artifact modification
- no secrets/token/cookie/access URL plaintext output
- no Mem0 write
- no longterm write
- no passes:true
- no Close

## Future Gate Order

1. If the user approves: Runtime Image Push Recovery Gate.
2. Then, only after `RUNTIME_IMAGE_BUILD_TAG_PUSH_DONE` or equivalent successful publication evidence: Isolated 2Gi+ Fresh Instance Approval Packet.
3. Then listener/hash verification.
4. Then browser E2E.
5. Then only after fresh E2E evidence plus explicit user approval: passes:true / Close / longterm write-back.

## Packet Notes

This packet is approval-only. It records the blocked push fact and proposes the minimum next recovery boundary. It does not diagnose the local registry, does not retry publication, and does not claim the image is published.
