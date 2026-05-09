# Control-ui localization fresh instance runtime deployment approval packet

Date/timezone: 2026-05-07, Asia/Shanghai

Role/task: Worker, serial topology, Approval Packet

Gate: CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET

Dependency gates:

- CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH_DONE
- COMMANDER_READONLY_REVIEW_ACCEPTED_FOR_CONTROLUI_LOCALIZATION_IMAGE_BUILD_TAG_PUSH
- CONTROLUI_LOCALIZATION_IMAGE_ASSEMBLY_PATCH_DONE

## Verdict

CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_DONE

This document is an approval packet only. It does not deploy the image, create or modify an instance, run Kubernetes commands, run browser/manual E2E, mutate database/runtime state, close the feature, write longterm memory, stage, commit, or push git changes.

## Proposed next gate

Recommended next gate name:

`CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_GATE`

Required user approval token:

`APPROVE_CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_GATE`

The next runtime deployment gate must not proceed without that exact explicit approval token.

## Reviewed image for proposed gate

The proposed next gate must use the reviewed localization image below.

| Field | Value |
| --- | --- |
| TAG_SUFFIX | `gtclaw-controlui-localization-20260507211942` |
| HOST_TAG | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| CLUSTER_TAG | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942` |
| image index digest | `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54` |
| linux/arm64 digest | `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e` |
| runtime control-ui root | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |

Source build evidence:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md`

## Expected runtime file hashes

The proposed next gate must verify that the fresh running runtime serves or contains these reviewed control-ui files under `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

| Runtime target path | Expected SHA-256 |
| --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` |

## Proposed fresh instance scope

If explicitly approved, the next gate may create or deploy exactly one new standard 2Gi fresh runtime instance through the normal ClawManager runtime path, using only:

`k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942`

The proposed gate should record:

- fresh instance identity and proof that exactly one new standard 2Gi runtime instance was created for this gate.
- Pod phase `Running` and Pod Ready condition `True`.
- container readiness, restart_count, and no OOMKilled state or event for the new pod.
- pod image reference and imageID digest, including comparison to image index digest `sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54`.
- registry or image metadata check for linux/arm64 digest `sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e`.
- Service ports including runtime HTTP `3001` and control-ui `18789`.
- Endpoint or EndpointSlice readiness for the new runtime pod.
- runtime-level control-ui HTTP `18789` evidence with HTTP `200`.
- running runtime control-ui path and file/hash evidence under `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- secret hygiene confirmation with no token/password/key/cookie/bearer/auth header/access URL plaintext recorded.

## Prior deployment lesson carried forward

Readonly evidence from `20260507-fresh-instance-runtime-deployment-controlui-persistence.md` showed the prior 1Gi fresh instance could start transiently but failed with `OOMKilled`, leaving control-ui HTTP and running-container path checks blocked.

Readonly evidence from `20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md` showed a standard 2Gi fresh runtime instance for the prior control-ui image remained Running/Ready, had restart count `0`, no OOMKilled state, Service ports `3001` and `18789`, control-ui `18789` HTTP `200`, and running-container control-ui path/hash proof.

Therefore the proposed next gate is intentionally scoped to exactly one standard 2Gi fresh runtime instance.

## Explicit future-gate boundary

The proposed next gate still must not run browser/manual E2E. Browser/manual E2E remains a later independent approval gate after runtime-level readiness and control-ui HTTP evidence exists.

The proposed next gate is not approval for:

- more than one new fresh instance.
- generalized cleanup or deletion of old runtime instances unless separately approved.
- backend, frontend, deployment, docs, longterm, AgentTeam, or UnifiedFramework edits.
- trustedProxy or runtime auth contract patches.
- image build/tag/push/pull.
- container run or docker compose.
- registry cleanup, registry mutation, or old tag deletion.
- database mutation outside the normal approved fresh-instance create/deploy path, and no direct database access unless separately approved.
- browser E2E, DevTools, Playwright, browser storage/cache/cookie cleanup, or manual browser verification.
- `passes:true`, Close, longterm write-back, git stage, commit, or push.

## Readonly evidence checked

This approval packet was prepared after readonly review of:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-fresh-instance-runtime-deployment-controlui-persistence.md`

## Verification commands

Readonly input checks:

```bash
sed -n '1,320p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-image-build-tag-push.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-capacity-recovery-standard-2gi-fresh-instance-controlui-persistence.md
sed -n '1,260p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-fresh-instance-runtime-deployment-controlui-persistence.md
```

Packet hygiene checks:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment-approval-packet.md
rg -n "CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_DONE|CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_BLOCKED|APPROVE_CONTROLUI_LOCALIZATION_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_GATE|gtclaw-controlui-localization-20260507211942|k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw|sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54|sha256:44c6614441ce51d5baca2b487090a80c40cfd4b7e434aa34d653576b72cf7a1e|2Gi|fresh instance|18789|no browser E2E|no kubectl|no database|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-fresh-instance-runtime-deployment-approval-packet.md
```

## Forbidden actions confirmation

Forbidden actions were not executed for this approval packet. Specifically: no instance creation, deletion, or modification; no kubectl; no k3d; no Helm; no database access or mutation; no runtime mutation; no browser E2E; no manual E2E; no DevTools; no Playwright; no image build/tag/push/pull; no container run; no docker run; no docker compose; no trustedProxy patch; no runtime auth patch; no plugin; no skill distribution; no backend modification; no frontend modification; no deployments modification; no docs modification; no longterm modification; no AgentTeam modification; no UnifiedFramework modification; no existing artifact modification; no existing evidence modification; no Mem0 write; no passes:true; no Close; no longterm write-back; and no git stage/commit/push.

No token, password, key, cookie, bearer material, authorization header plaintext, private key, secret value, or access URL plaintext was recorded.
