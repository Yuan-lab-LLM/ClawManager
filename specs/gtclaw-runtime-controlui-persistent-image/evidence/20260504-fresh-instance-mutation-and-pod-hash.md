# Fresh Instance Mutation And Pod Hash Evidence - 2026-05-04

## Verdict

FRESH_INSTANCE_POD_HASH_DONE

`BLOCKED` is not the current verdict.

## Gate Statement

fresh instance + read-only pod hash only, no browser E2E, no passes:true, no Close.

This evidence covers T023-T026 fresh disposable instance environment mutation and read-only deployed pod hash verification under the approved gate. It does not authorize or perform browser route checks, DOM checks, desktop regression checks, `passes:true`, Close, or longterm write-back.

## Dependency Gate

The T022 approval packet was completed and Commander-reviewed:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-approval-packet.md`

The T017-T021 image delivery loop dependency was completed:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-image-delivery-loop-rerun-after-baseline-decision.md`

## Artifact Identity

| Artifact field | Value |
| --- | --- |
| Host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| In-cluster alias used by pod image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| Image index digest | `sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| linux/arm64 manifest digest | `sha256:d1fbcb65e2a576ffe32396d61931d86931b1e87c829051001bd5be67e067261e` |

## Previous Setting Recorded

Previous runtime image/resource setting was recorded before temporary change:

| Field | Value |
| --- | --- |
| instance_type | `openclaw` |
| display_name | `OpenClaw ARM Local Registry` |
| image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434` |
| is_enabled | `true` |

## Temporary Setting Result

The approved artifact was temporarily set only for the disposable test instance flow.

| Field | Value |
| --- | --- |
| API result | `HTTP 200` |
| temporary image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |

## Fresh Disposable Instance

First disposable attempt:

| Field | Value |
| --- | --- |
| instance id/name | `4` / `gtclaw-fresh-20260504095510` |
| pod | `clawmanager-user-1/clawreef-4-gtclaw-fresh-20260504095510` |
| result | `BLOCKED` for that attempt only: pod was `Pending` with `Unschedulable` because of insufficient node memory |
| rollback action | deleted this task-created disposable instance only |
| delete result | `HTTP 200` |

The first attempt did not become evidence for deployed pod hash. It was not an old runtime instance, was created by this task, and was deleted only because rollback was needed before retrying with smaller disposable resources. No old T8I pod was reused or deleted.

Successful fresh disposable instance:

| Field | Value |
| --- | --- |
| instance id/name | `5` / `gtclaw-fresh-20260504095843` |
| instance status | `running` |
| namespace | `clawmanager-user-1` |
| pod | `clawreef-5-gtclaw-fresh-20260504095843` |
| container | `desktop` |
| pod image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-20260504005506` |
| imageID | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b63a62a230eed161a4a1e47fb882b33288097e360374bb45649219236e080b10` |
| restart count | `0` |
| package version | `openclaw@2026.4.14` |

## Required Pod Hash Target

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

## Deployed Pod Hash Verification

Result: deployed pod hash match for all four allowlist files.

| File | Deployed pod hash | Size | Required hash result |
| --- | --- | ---: | --- |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` | match |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` | match |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` | match |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` | match |

This satisfies the deployed pod hash alignment gate for the approved fresh disposable instance. Browser E2E remains a later separate gate.

## Restore Previous Setting Result

The previous runtime image/resource setting was restored after pod metadata and hash capture.

| Field | Value |
| --- | --- |
| restore result | `HTTP 200` |
| restored image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:dev-arm64-pkt09-20260414170434` |
| restored matches previous setting | `yes` |

Required statement:

`restore previous setting: the previous runtime image/resource setting was recorded before the temporary test change and restored after fresh pod metadata/hash capture.`

## No Manual Pod Patch Statement

`no manual pod patch: the successful fresh disposable instance was created from the approved artifact and no pod file was manually patched, copied into, or repaired in place.`

No `kubectl cp` write into pod was used. No runtime process was restarted. Pod access for the successful instance was read-only metadata, package version, and file hash inspection only.

## Secret Hygiene Statement

No token value, cookie value, credential, secret, registry credential, `.env`, `.codex/auth.json`, `.codex/config.toml`, or token-bearing URL is recorded in this evidence.

Authentication material was used only transiently for approved GTManager API calls and was not written to this file.

## Scope Statement

Only this repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md`

Approved environment changes performed:

- recorded previous `openclaw` runtime image/resource setting
- temporarily set the approved artifact for disposable instance creation
- created a new disposable instance from the approved artifact
- deleted only the first task-created disposable instance after it failed scheduling and required rollback
- captured fresh pod metadata and read-only hashes from the successful fresh disposable instance
- restored the previous runtime image/resource setting

No existing evidence was modified. `spec.md`, `plan.md`, `tasks.md`, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, `/tmp/gtclaw-runtime-patch/**`, runtime pod/container files, Kubernetes Secrets, ConfigMaps, namespaces, deployment defaults, image tags, registry state, `.codex/auth.json`, and `.codex/config.toml` were not modified.

Database mutation was limited to the approved temporary runtime image/resource setting path and normal control-plane instance records created by the approved disposable instance flow.

## Verification Commands

Commands to verify this evidence packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md
rg -n "FRESH_INSTANCE_POD_HASH_DONE|BLOCKED|gtclaw-controlui-persistent-20260504005506|b63a62a|d1fbcb65|/usr/local/lib/node_modules/openclaw/dist/control-ui|fresh disposable instance|pod image|imageID|restart|deployed pod hash|no manual pod patch|restore previous setting|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-approval-packet.md
```
