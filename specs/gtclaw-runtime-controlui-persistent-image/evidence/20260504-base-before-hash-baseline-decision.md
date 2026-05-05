# Base Before-Hash Baseline Decision Evidence - 2026-05-04

## Verdict

BASELINE_DECISION_DONE

## Decision

Decision: `OPTION_A_KEEP_APPROVED_BASE_26BC`

Commander/user selected Option A from the base before-hash drift analysis: keep the approved parent OpenClaw Image base and revise the active before-hash gate to the observed approved-base values.

This decision evidence resolves the baseline-choice question only. It does not authorize image build/tag/push/pull/inspect, registry mutation, runtime/K8S/database mutation, fresh instance creation, browser E2E, `passes:true`, or Close.

## Evidence Basis

Primary dependency evidence:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-base-before-hash-drift-analysis.md`

The dependency evidence returned `BASE_BEFORE_HASH_DRIFT_ANALYSIS_DONE` and root-caused the prior T017-T021 rerun stop:

- The expected before-hash table in `20260503-hash-manifest-design.md` was assigned to the wrong base for the approved rerun.
- That table maps to historical T6 image / addendum backup baseline, not to approved base image index `26bc8e3a...`.
- The approved base digest itself resolved successfully and produced the observed before-hashes for `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

## Active Base Digest / Platform Digest

Active approved base image index:

`sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8`

Active approved base linux/arm64 manifest:

`sha256:e1b75327097086d33e2e3134750f65db9ea2a1ea18615eb94078d39f77847315`

The active copy target remains:

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

The following path remains forbidden as a copy target:

`/opt/opensparrow/runtime/openclaw/dist/control-ui`

## Revised Expected Before-Hash Table

This is the revised expected before-hash gate for Option A and approved base `26bc8e3a...` / linux/arm64 `e1b75327...`.

| File | Revised expected before-hash | Revised expected size |
| --- | --- | ---: |
| `index.html` | `f313071437a1b8c432024d3f83af4056fb672a4fe15b93be8b2291dcaac0115c` | `3395` |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` |
| `assets/zh-CN-B26mMdbY.js` | `2afe4858d80c81247f01e21198011a78180de12e72f567b5606fe9355dbfd2c1` | `23247` |
| `assets/index-M4TNVXB3.js` | `e89d5e55d89aaae7bc64598b949335425df7626f57b12a8780426a1911315882` | `707545` |

Future T017-T021 rerun, if separately authorized, should treat this table as the active before-hash gate for approved base `26bc8e3a...`.

## Source Artifact After-Hash Status

The source artifact after-hash table remains unchanged from `/tmp/gtclaw-runtime-patch` and the runtime pod source artifact recovery evidence.

| File | Required after-hash | Required size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

This decision does not read, write, rebuild, or mutate `/tmp/gtclaw-runtime-patch`.

## Historical T6 / Addendum Classification

The historical T6/addendum before-hash table remains historical baseline only. It is not the active before-hash gate for Option A.

Historical T6 image index:

`sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45`

Historical T6 linux/arm64 manifest:

`sha256:112421e96f10a9b45fb679ae1784f269c552680f19066e754ebff0fec11c7984`

Historical T6/addendum before-hash table:

| File | Historical T6/addendum before-hash | Historical size | Option A classification |
| --- | --- | ---: | --- |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` | `3395` | historical baseline only |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` | same value in both baselines; still historical in this table |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` | `23248` | historical baseline only |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` | `707543` | historical baseline only |

The historical T6/addendum table may remain useful for audit narrative, but it must not block an Option A build rerun against approved base `26bc8e3a...`.

## Risk Statement

Resulting image delta over approved base `26bc8e3a...` includes all four repaired files directly. This may include T6-era resource changes plus the body repair, still constrained to the four allowlist files only.

This risk must be carried into the next T017-T021 rerun evidence and any later review. The allowlist scope does not expand, and this decision does not authorize broad OpenClaw to GTClaw replacement or technical identity rewrites.

## Required Next Gate

After Commander verifies this decision evidence, a separate T017-T021 rerun prompt may authorize build/tag/push/pull/inspect using the revised expected before-hash table above.

That later prompt must explicitly authorize any image build, target tag generation, registry push/publish to `localhost:5001`, pull/inspect, and built artifact hash extraction. This decision evidence alone does not grant those actions.

Fresh instance creation, deployed pod hash read, browser E2E, runtime/K8S/database mutation, `passes:true`, and Close remain unauthorized and require later explicit gates.

## Scope Statement

Only this repository file was written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-base-before-hash-baseline-decision.md`

No existing evidence was modified. `spec.md`, `plan.md`, `tasks.md`, `backend/**`, `frontend/**`, `deployments/**`, `docs/**`, `longterm/**`, `AgentTeam/**`, `/tmp/gtclaw-runtime-patch/**`, runtime pod/container files, Kubernetes resources, Secrets, ConfigMaps, namespaces, database, image tags, registry state, `.codex/auth.json`, and `.codex/config.toml` were not modified.

## Gate Statement

Decision evidence only, no image build/tag/push/pull/inspect, no registry mutation, no runtime/K8S/database mutation, no fresh instance, no browser E2E, no passes:true, no Close.
