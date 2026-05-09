# Control-ui localization capacity recovery standard 2Gi fresh instance approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_LOCALIZATION_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_APPROVAL_PACKET

Dependency gates:

- CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_BLOCKED
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_BLOCKED
- CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE

## Verdict

CONTROLUI_LOCALIZATION_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_APPROVAL_PACKET_DONE

This document is an approval packet only. It does not delete or modify any pod, Service, PVC, instance, database row, registry tag, session, or asset. It does not create an instance, run Kubernetes mutation, run browser/manual E2E, build/pull/push an image, close the feature, write longterm memory, stage, commit, or push git changes.

## Proposed next gate

Recommended next gate name:

`CONTROLUI_LOCALIZATION_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_GATE`

Required user approval token:

`APPROVE_CONTROLUI_LOCALIZATION_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_GATE`

The next gate must not proceed without that exact explicit approval token.

## Blocker being addressed

The previous localization fresh-instance runtime deployment gate correctly blocked before creation:

```text
CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_BLOCKED
blocker=insufficient schedulable memory for one new 2Gi instance without unauthorized cleanup
allocatable_memory_approx_mib=7836
allocated_memory_requests_mib=6284
remaining_memory_approx_mib=1552
required_new_standard_instance_memory_mib=2048
create_attempted=false
```

Reason for this approval packet: a standard 2Gi fresh instance cannot be retried unless the next approved gate is allowed to recover capacity through one narrow, explicit old disposable runtime pod cleanup candidate.

## Target image for retry

| Field | Value |
| --- | --- |
| CLUSTER_TAG | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| HOST_TAG | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| image index digest | `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54` |
| linux/arm64 digest | `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e` |
| runtime control-ui root | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |

## Proposed cleanup candidate

The proposed next gate must first run a fresh read-only capacity and pod-state check. If capacity is already sufficient for exactly one standard 2Gi fresh instance, it must skip cleanup and proceed directly to the normal ClawManager create path.

If capacity is still insufficient, this approval packet proposes exactly one cleanup candidate:

| Candidate | Namespace | Instance ID | Current evidence status | Memory request | Image | Cleanup boundary |
| --- | --- | ---: | --- | --- | --- | --- |
| `pod/clawreef-10-oc2gi-185707` | `clawmanager-user-1` | `10` | old disposable runtime pod, Running/Ready in read-only preflight | `2Gi` | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033` | May delete this pod only if still present, still non-target image, and still the selected stale disposable runtime pod. |

Candidate selection rationale:

- It is not the current localization target image `gtclaw-controlui-localization-20260507211942`.
- It is not the future new target instance, because the future target instance does not exist before the next gate creates it.
- It was recorded in prior capacity evidence as an old disposable 2Gi runtime pod and was explicitly left untouched only because a different single 2Gi candidate was enough at that time.
- Deleting one 2Gi pod is the minimum capacity action needed to recover enough request headroom for one standard 2Gi fresh instance.

Stale-state guardrails for the proposed next gate:

- If `pod/clawreef-10-oc2gi-185707` is absent, not in namespace `clawmanager-user-1`, no longer a 2Gi runtime pod, or now uses the localization target image, do not delete it and block for a new approval packet.
- If deleting this one pod would not recover enough capacity for one standard 2Gi fresh instance, do not delete it and block.
- If the next gate would require deleting a Service, PVC, database row, session, asset, registry tag, or any second pod, block.

## Proposed cleanup boundary

If explicitly approved and still needed, the next gate may delete only:

```text
kubectl delete pod clawreef-10-oc2gi-185707 -n clawmanager-user-1 --wait=true
```

The next gate must not delete or modify:

- `service/clawreef-10-oc2gi-185707-svc`
- any PVC
- any database row
- any ClawManager instance record
- any session
- any asset
- any registry tag or registry content
- any pod other than `clawreef-10-oc2gi-185707`
- any backend, frontend, deployment, docs, longterm, AgentTeam, or UnifiedFramework file

## Proposed retry scope after capacity recovery

After either confirming sufficient capacity or deleting only the approved candidate above, the proposed next gate may create exactly one new standard 2Gi fresh runtime instance through the normal ClawManager API/runtime path using:

`k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942`

The proposed next gate must record:

- before/after instance count and instance ids proving exactly one new instance.
- new instance id/name/type/memory_gb/image_registry/pod_namespace/pod_name.
- Pod phase `Running` and `Ready=True`.
- `container_ready`, `restart_count=0`, `oom_killed=false`, `last_state`, and pod events.
- pod image reference and imageID digest compared to image index digest `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54`.
- registry/image metadata confirming linux/arm64 digest `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e`.
- Service ports `3001` and `18789`.
- Endpoint/EndpointSlice pointing to the new pod and ready/serving.
- control-ui `18789` HTTP 200 through loopback, PodIP, and ServiceIP.
- runtime `3001` listener smoke, with HTTP code recorded but not required to be 200.
- running-container path `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- running-container control-ui file hashes.

Expected running-container control-ui hashes:

| Runtime target path | Expected SHA-256 |
| --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` |

## E2E boundary

The proposed next gate still must not run browser/manual E2E, DevTools, Playwright, browser storage cleanup, browser cache cleanup, browser cookie cleanup, or manual browser verification.

Browser/manual E2E remains a later independent approval gate after runtime-level readiness, `18789` HTTP 200, and running-container file/hash evidence exist.

## Readonly evidence checked

This approval packet was prepared after readonly review of:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md`

## Verification commands

Readonly input checks:

```bash
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md
```

Packet hygiene checks:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-capacity-recovery-standard-2gi-fresh-instance-approval-packet.md
rg -n "CONTROLUI_LOCALIZATION_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_APPROVAL_PACKET_DONE|CONTROLUI_LOCALIZATION_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_APPROVAL_PACKET_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_CAPACITY_RECOVERY_STANDARD_2GI_FRESH_INSTANCE_GATE|insufficient schedulable memory|gtclaw-controlui-localization-20260507211942|sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54|2Gi|fresh instance|cleanup candidate|18789|no browser E2E|no database|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-capacity-recovery-standard-2gi-fresh-instance-approval-packet.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-capacity-recovery-standard-2gi-fresh-instance-approval-packet.md | wc -l
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-capacity-recovery-standard-2gi-fresh-instance-approval-packet.md
```

## Forbidden actions confirmation

Forbidden actions were not executed for this approval packet. Specifically: no pod deletion or modification, no Service deletion or modification, no PVC deletion or modification, no instance creation, deletion, or modification, no database row deletion or modification, no registry tag deletion or modification, no kubectl mutation, no k3d, no Helm, no direct database access or mutation, no browser E2E, no DevTools, no Playwright, no image build/tag/push/pull, no container run, no docker run, no docker compose, no trustedProxy patch, no runtime auth patch, no plugin, no skill distribution, no backend modification, no frontend modification, no deployments modification, no docs modification, no longterm modification, no AgentTeam modification, no UnifiedFramework modification, no existing artifact modification, no existing evidence modification, no Mem0 write, no passes:true, no Close, no longterm write-back, and no git stage/commit/push.

No token value, password value, key value, cookie value, bearer value, authorization header plaintext, access URL plaintext, private key, registry credential, `.env`, `.codex/auth.json`, or `.codex/config.toml` content was recorded.
