# Base Before-Hash Drift Analysis - 2026-05-03

## Verdict

BASE_BEFORE_HASH_DRIFT_ANALYSIS_DONE

The ImageDeliveryWorker rerun remains BLOCKED, but this analysis root-caused the stop condition. The selected approved base digest `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` resolved successfully to linux/arm64 manifest `sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315`. The observed before-hashes are confirmed as belonging to that approved base artifact.

Root cause: the expected before-hash table was assigned to the wrong base for the rerun. It belongs to the historical T6 image / addendum backup baseline, not to the approved base image index digest `26bc8e3a...`.

## Scope / Gate

Analysis only. No image build, no target image tag, no target image push, no target image pull, no registry mutation, no runtime/K8S/database mutation, no fresh instance, no browser E2E, no passes:true, no Close.

No `spec.md`, `plan.md`, `tasks.md`, existing evidence, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, runtime pod/container files, Kubernetes resources, Secrets, ConfigMaps, namespaces, database rows, image tags, registry state, `.codex/auth.json`, or `.codex/config.toml` were modified by this analysis.

Only this repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-base-before-hash-drift-analysis.md`

Temporary read-only analysis extraction used:

`/tmp/gtclaw-base-drift-analysis-kAxdgQ`

## Evidence Sources Used

| Source | Use |
| --- | --- |
| `AGENTS.md` | Project authority, forbidden actions, evidence and memory discipline |
| `.specify/memory/constitution.md` | Spec-Kit gate discipline and no `passes:true` without E2E evidence |
| `specs/gtclaw-runtime-controlui-persistent-image/spec.md` | Persistent delivery scope and forbidden actions |
| `specs/gtclaw-runtime-controlui-persistent-image/plan.md` | Digest-pinned image strategy, expected baseline behavior, drift-stop rule |
| `specs/gtclaw-runtime-controlui-persistent-image/tasks.md` | T012-T021 hash/image gates and stop behavior |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-path-verification.md` | `/usr/local/lib/node_modules/openclaw/dist/control-ui` is the proven runtime-served target; `/opt/opensparrow/runtime/openclaw/dist/control-ui` is not proven |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-delivery-decision.md` | Digest-pinned image decision packet and local registry loop boundary |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-hash-manifest-design.md` | Expected before-hash and after-hash tables |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-implementation-approval-packet.md` | Approved-value candidates and T017-T021 stop conditions |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-source-artifact-recovery.md` | Historical T6 image extraction matched the expected before-hash table |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-runtime-pod-source-artifact-recovery.md` | `/tmp/gtclaw-runtime-patch` after-hash source artifact recovery |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop-rerun-after-source-recovery.md` | BLOCKED rerun evidence and observed base before-hashes |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md` | Addendum backup before-hash and repaired after-hash baseline |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501001159-test-env-mutation.md` | Historical T6 digest, arm64 manifest digest, runtime path, and T6 file hashes |
| `/tmp/gtclaw-runtime-patch/**` | Read-only recheck of recovered after-hash source artifact |
| `/tmp/gtclaw-controlui-persistent-build-base-9NqxFp/**` | Read-only offline OCI metadata and layer extraction for approved base `26bc8e3a...` |

## Artifact Identity Findings

Approved base image identity from local offline OCI metadata:

| Class | Digest / value | Finding |
| --- | --- | --- |
| Selected approved base image index | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` | Local blob SHA-256 matches digest; resolved successfully |
| Approved base linux/arm64 manifest | `sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315` | Listed under the `26bc8e3a...` image index for platform `linux/arm64` |
| Layer containing the four control-ui files in approved base | `sha256:047b8ea8e675b815dc1f502646a1916dd44f614c1a83da7093e13f1694792e49` | Offline layer listing showed all four files under `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Historical T6 image index | `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` | Historical/reference image used by earlier T6/T8I flow |
| Historical T6 linux/arm64 manifest | `sha256:112421e96f10a9b45fb679ae1784f269c552680f19066e754ebff0fec11c7984` | Historical/reference arm64 manifest used by earlier T6/T8I flow |

`/tmp/gtclaw-runtime-patch` was also rechecked and still matches the required after-hash table:

| File | Recovered source SHA-256 | Size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

## Digest / Hash Mapping Table

| File | Approved base `26bc8e3a...` / arm64 `e1b75327...` observed before | Expected before table | Historical T6 / addendum backup before | Classification |
| --- | --- | --- | --- | --- |
| `index.html` | `f313071437a1b8c432024d3f83af4056fb672a4fe15b93be8b2291dcaac0115c` / `3395` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` / `3395` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` / `3395` | Expected before-hash belongs to T6/addendum, not approved base |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` / `42702` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` / `42702` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` / `42702` | Same in both baselines |
| `assets/zh-CN-B26mMdbY.js` | `2afe4858d80c81247f01e21198011a78180de12e72f567b5606fe9355dbfd2c1` / `23247` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` / `23248` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` / `23248` | Expected before-hash belongs to T6/addendum, not approved base |
| `assets/index-M4TNVXB3.js` | `e89d5e55d89aaae7bc64598b949335425df7626f57b12a8780426a1911315882` / `707545` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` / `707543` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` / `707543` | Expected before-hash belongs to T6/addendum, not approved base |

## Root-Cause Finding

The observed values `f3130714...`, `09c2150d...`, `2afe4858...`, and `e89d5e55...` are confirmed as the before-hash set for the approved base image index `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` at linux/arm64 manifest `sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315`.

The expected before-hash table `ed3560d9...`, `09c2150d...`, `9a4ecc89...`, and `eb8379dc...` maps to:

- historical T6 image `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029`, image index `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45`, linux/arm64 manifest `sha256:112421e96f10a9b45fb679ae1784f269c552680f19066e754ebff0fec11c7984`
- addendum backup before-hash set from `/tmp/gtclaw-controlui-repair-backup-20260502233047`

This is best classified as expected before-hash table assigned to wrong base. In planning terms it is a base drift gate hit because the selected base artifact differs from the hash table, but the lower-level cause is not mutable digest drift. The approved base digest itself appears stable and correctly resolved in the local offline OCI save.

Rejected classifications:

| Candidate cause | Finding |
| --- | --- |
| Base image drift from mutable digest | Not supported. `26bc8e3a...` resolved to a concrete local image index blob, and its linux/arm64 manifest `e1b75327...` produced the observed hashes offline. |
| Local registry/digest confusion | Not supported by current evidence. The selected local OCI artifact maps `26bc8e3a...` to `e1b75327...`, and the extracted hashes match the ImageDeliveryWorker rerun evidence. |
| Wrong extraction path | Not supported. Path verification proved `/usr/local/lib/node_modules/openclaw/dist/control-ui` as the runtime-served path, and the approved base OCI layer contains the same relative path. `/opt/opensparrow/runtime/openclaw/dist/control-ui` remains unproven and was not used. |
| After-hash source artifact problem | Not supported. `/tmp/gtclaw-runtime-patch` matches the required after-hash table for all four files. |

## Commander Decision Options

### Option A - Keep approved base `26bc8e3a...`; revise expected before-hash table after explicit approval

Decision: keep the approved base image index digest `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` and linux/arm64 manifest `sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315` as the baseline. Require explicit Commander/user approval to revise the expected before-hash table to the observed values in this analysis.

Risk:

- This changes the baseline from historical T6/addendum backup to the earlier approved base.
- The resulting image delta may include the T6-era resource changes plus the body repair if the same after-hash source artifact is copied directly over the approved base.
- Commander must decide whether that broader baseline jump is intended and whether any T6-era intermediate changes need separate audit wording.

Required next approval gate:

- Explicit approval to revise the expected before-hash table to `f3130714...`, `09c2150d...`, `2afe4858...`, `e89d5e55...` for approved base `26bc8e3a...`.
- Only after that approval, a separate T017-T021 approval may authorize build/tag/push/pull/inspect and built artifact extraction.

Allowed next actions under Option A before implementation approval:

- Update or write a new approval packet / manifest design note naming the revised before-hash table.
- Keep analysis and decision documentation read-only aside from the specifically approved evidence/packet file.

Forbidden until later explicit approval:

- image build/tag/push/pull, registry mutation, runtime/K8S/database mutation, fresh instance, browser E2E, passes:true, Close.

### Option B - Change base to historical T6 image `b5cef803...` / arm64 `112421e9...`

Decision: treat the historical T6 image as the intended baseline because it matches the expected before-hash table and addendum backup.

Risk:

- This selects a derived historical image as the base instead of the approved parent base digest.
- Commander must approve using T6 image index `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` and linux/arm64 manifest `sha256:112421e96f10a9b45fb679ae1784f269c552680f19066e754ebff0fec11c7984`.
- The local T6 tag may be environment-local; persistent registry policy and rollback identity must be explicit.

Required next approval gate:

- Explicit approval to change the selected base artifact from `26bc8e3a...` to historical T6 image `b5cef803...` / arm64 `112421e9...`.
- Explicit approval for any read-only local image extraction if Commander wants a fresh local recheck of the T6 artifact before implementation.
- Separate T017-T021 approval before any build/tag/push/pull/inspect.

Allowed next actions under Option B before implementation approval:

- Prepare a revised implementation approval packet naming T6 as the base and preserving the current expected before-hash table.
- If separately approved, perform read-only local image metadata/extraction of already-local T6 image artifacts without registry/network pull.

Forbidden until later explicit approval:

- image build/tag/push/pull target image, registry mutation, runtime/K8S/database mutation, fresh instance, browser E2E, passes:true, Close.

### Option C - Stop and send external expert escalation

Decision: pause the delivery loop and send the baseline-choice question for senior/external review.

Risk:

- Slower path, but lowest risk if Commander is uncertain whether `26bc8e3a...` or T6 `b5cef803...` is the correct semantic baseline.
- Expert needs only the evidence and digest/hash mapping, not secrets or runtime access.

Required next approval gate:

- Commander approval to package and send an external expert prompt. No external tool should be contacted by this worker.

Allowed next actions under Option C:

- Draft a ready-to-forward escalation packet with exact file/evidence scope, confirmed facts, hypotheses, constraints, and decision requested.

Forbidden:

- Same as current gate: no image build/tag/push/pull, no registry mutation, no runtime/K8S/database mutation, no fresh instance, no browser E2E, no passes:true, no Close.

## Recommended Commander Path

If the intended baseline is the user-approved parent OpenClaw base digest, choose Option A and explicitly approve revising the expected before-hash table to the observed approved-base values.

If the intended baseline is the already-localized historical T6 runtime image that the repaired pod was based on, choose Option B and explicitly approve changing the base to `b5cef803...` / `112421e9...`.

If there is any architecture or audit uncertainty about whether the persistent image should replay all T6 resource changes or only the final body repair over a prior image, choose Option C before authorizing another delivery loop.

## Verification Commands

Commands to verify this evidence packet:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-base-before-hash-drift-analysis.md
rg -n "BASE_BEFORE_HASH_DRIFT_ANALYSIS_DONE|BLOCKED|26bc8e3a|b5cef803|112421e9|e1b75327|f3130714|2afe4858|e89d5e55|ed3560d9|9a4ecc89|eb8379dc|wrong base|base drift|expected before-hash|no image build|no registry mutation|no fresh instance|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-base-before-hash-drift-analysis.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-base-before-hash-drift-analysis.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-image-delivery-loop-rerun-after-source-recovery.md
```
