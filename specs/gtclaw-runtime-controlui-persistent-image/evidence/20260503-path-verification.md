# Path Verification Evidence - 2026-05-03

## Verdict

- COPY TARGET NOT PROVEN for `/opt/opensparrow/runtime/openclaw/dist/control-ui`.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui` is the runtime-served control-ui path proven by served-file hash.

This evidence packet records the T004-T007 PathVerification result only. It is path evidence write only, not delivery decision, not implementation, no image/registry/runtime/K8S mutation, no passes:true, no Close.

## Evidence Source And Limits

- Source basis: Commander/user handoff for T004-T007 PathVerification plus already-recorded F-006 runtime identity/hash evidence.
- No runtime/pod/container was re-accessed by this evidence write.
- Directory device/inode values are copied from the prior T004 PathVerification report, with no runtime re-access.

## Runtime Target Inspected

| Field | Value |
| --- | --- |
| Namespace | `clawmanager-user-1` |
| Pod | `clawreef-3-gtclaw-t8-dev-20260501001159` |
| Container | `desktop` |
| Phase | `Running` |
| Ready | `true` / `Ready=True` |
| Restarts | `0` |
| Image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` |
| ImageID digest | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` |
| Package | `openclaw@2026.4.14` |

## Path Relationship Table

| Path | Exists | Type | Symlink | Device/inode from report | Relationship conclusion |
| --- | --- | --- | --- | --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui` | yes | directory | not symlink | Device: `142 (0x8e)`; Inode: `1016391` | runtime-served candidate; served `/index.html` hash/size maps to `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` |
| `/opt/opensparrow/runtime/openclaw/dist/control-ui` | no | missing | not symlink | n/a because path missing in reported runtime inspection | missing, not same dir, not symlink; build-time copy relationship not observable/proven |

Conclusion: the two paths are not proven equivalent. `/opt/opensparrow/runtime/openclaw/dist/control-ui` is not proven as the runtime-served copy target.

## Runtime-Served Proof

- `openclaw-gateway` Node process was identified on port `18789`.
- Bridge reachability is not static-dir proof.
- Process args did not expose the static directory.
- Served `/index.html` hash/size matches local `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html`.
- The runtime-served conclusion is based on served-file identity, not on an assumed bridge or copy-path relationship.

## Served-File Hash/Size

| Field | Value |
| --- | --- |
| Route | `http://127.0.0.1:18789/index.html` |
| Status | `200` |
| SHA-256 | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| Size | `3398` |
| Mapped local path | `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` |

## Blocker / Downstream Gate

- `/opt/opensparrow/runtime/openclaw/dist/control-ui` must not be used as copy target without separate proof.
- T008 delivery decision may only proceed using `/usr/local/lib/node_modules/openclaw/dist/control-ui` as the proven runtime-served copy target, unless a later approved proof changes this.

## Scope And Gate

- evidence write only
- no runtime re-access unless separately approved
- no image/registry/runtime/K8S mutation
- no app image mutation
- no runtime image mutation
- no registry tag mutation
- no Kubernetes resource, Secret, ConfigMap, namespace, or database mutation
- no fresh instance
- no browser E2E
- no passes:true
- no Close
