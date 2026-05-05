# Fresh Instance Approval Packet - 2026-05-04

## Verdict

FRESH_INSTANCE_APPROVAL_PACKET_DONE

`BLOCKED` is not the current verdict. This packet is an approval packet only.

## Proposed Gate

Proposed gate: T023-T026 fresh disposable instance environment mutation + T014 deployed pod hash extraction preparation only.

This proposed gate would, only after later explicit approval, prepare a new disposable runtime instance from the approved artifact and prepare deployed pod hash extraction for the proven runtime control-ui path. It does not authorize browser E2E, `passes:true`, or Close.

## Dependency Gate

T017-T021 local k3d image delivery loop has completed:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-image-delivery-loop-rerun-after-baseline-decision.md`

That dependency proves source artifact hash = built image hash for the four allowlist files. It does not prove fresh deployed pod hash, fresh disposable instance behavior, or browser E2E.

## Artifact Identity Proposed

| Artifact field | Value |
| --- | --- |
| Host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| In-cluster alias | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| Image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |

## Fresh Instance Gate Proposed

Later approval should be limited to the following serial gate:

1. T023: record the previous runtime image/resource setting, then temporarily set the approved runtime image/resource only if needed for a disposable test instance.
2. T024: create a new fresh disposable instance from the approved artifact.
3. T025: capture fresh pod metadata needed for evidence, then restore previous setting if a temporary setting change was used.
4. T026: enforce no manual pod patch as a hard stop condition.
5. T014 preparation: prepare fresh deployed pod hash extraction for the four allowlist files only, at the required target path, if pod read access is later approved.

## Required Fresh Instance Rules

- must create a new disposable runtime instance from the approved artifact
- must not reuse old T8I pod
- must not manually patch pod files
- must not use `kubectl cp` to write into pod
- must record previous image/resource setting before any temporary change
- must restore previous setting after fresh pod metadata is captured if a temporary setting change is used

Required no manual pod patch statement for later evidence:

`no manual pod patch: the fresh disposable instance was created from the approved artifact and no pod file was manually patched, copied into, or repaired in place.`

Required rollback/restore statement for later evidence:

`restore previous setting: the previous runtime image/resource setting was recorded before the temporary test change and restored after fresh pod metadata capture.`

## Required Pod Hash Target

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

## Required Deployed Pod After-Hash Table

| File | Required deployed pod SHA-256 | Required size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

## Required Evidence Fields

Later approved fresh-instance evidence must record:

- instance id/name
- namespace/pod/container
- pod image
- imageID digest
- restart count
- package version if read access is later approved
- four deployed pod hashes if read access is later approved
- no manual pod patch statement
- rollback/restore statement
- secret hygiene statement

## Allowed Actions For Later Approval

Only a later explicit approval may allow:

- setting the runtime image/resource temporarily for a disposable test instance
- creating a fresh disposable instance from the approved artifact
- reading fresh pod metadata needed for instance identity evidence
- restoring the previous image/resource setting after metadata capture
- preparing T014 deployed pod hash extraction for the required target path if pod read access is separately approved

## Forbidden Actions

The following remain forbidden by this packet:

- no runtime image/resource setting change now
- no fresh instance now
- no deployed pod hash read now
- no runtime pod/container access now
- no browser E2E
- no runtime/K8S/database mutation now
- no Kubernetes resource, Secret, ConfigMap, namespace, or deployment default mutation now
- no manual pod patch
- no `kubectl cp` write into pod
- no runtime process restart
- no backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, existing evidence, or `/tmp/gtclaw-runtime-patch/**` modification
- no image tag or registry state mutation now
- no token, cookie, secret, credential, registry credential, token-bearing URL, `.codex/auth.json`, or `.codex/config.toml` output
- no Mem0 or longterm write
- no passes:true
- no Close

## Browser E2E Later Gate

Browser E2E remains a later separate gate after deployed pod hash alignment. This packet proposes only fresh disposable instance environment mutation and deployed pod hash extraction preparation. It does not authorize route checks, DOM checks, desktop regression checks, or token-bearing browser evidence collection.

## Rollback / Restore Requirement

If a temporary runtime image/resource setting change is later approved, the worker must record the previous setting before the change and restore previous setting after fresh pod metadata is captured. If restoration fails, the worker must stop and report `BLOCKED` with the recorded prior setting and the attempted restore action, without running browser E2E, writing `passes:true`, or Close.

## Secret Hygiene Requirement

Later evidence must confirm that no token value, cookie value, credential, secret, registry credential, `.env`, `.codex/auth.json`, `.codex/config.toml`, or token-bearing URL was recorded. Commands and notes must be redacted before inclusion in evidence.

## Scope Statement

Only this repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-approval-packet.md`

No existing evidence was modified. `spec.md`, `plan.md`, `tasks.md`, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, `/tmp/gtclaw-runtime-patch/**`, runtime pod/container files, Kubernetes resources, Secrets, ConfigMaps, namespaces, database, image tags, registry state, `.codex/auth.json`, and `.codex/config.toml` were not modified by this approval packet task.

## Gate Statement

Approval packet only, no runtime/K8S/database mutation, no fresh instance, no deployed pod hash read, no browser E2E, no passes:true, no Close.

## Verification Commands

Commands to verify this packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-approval-packet.md
rg -n "FRESH_INSTANCE_APPROVAL_PACKET_DONE|BLOCKED|gtclaw-controlui-persistent-20260504005506|b63a62a|d1fbcb65|/usr/local/lib/node_modules/openclaw/dist/control-ui|fresh disposable instance|no manual pod patch|restore previous setting|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-image-delivery-loop-rerun-after-baseline-decision.md
```
