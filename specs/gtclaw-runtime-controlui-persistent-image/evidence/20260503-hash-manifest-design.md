# Hash Manifest Design Evidence - 2026-05-03

## Status

HASH MANIFEST DESIGN ONLY - NOT IMPLEMENTATION APPROVAL.

This evidence file records T012 source artifact hash manifest design only. It grants no authorization for implementation, image build/tag/push/pull/inspect, registry mutation, runtime/container mutation, Kubernetes resource/Secret/ConfigMap/namespace mutation, database mutation, fresh instance creation, browser E2E, `passes:true`, or Close.

## Source Basis

| Source | Design use |
| --- | --- |
| `AGENTS.md` | Project authority, forbidden actions, memory and evidence discipline |
| `.specify/memory/constitution.md` | Spec-Kit gate discipline and E2E evidence requirements |
| `specs/gtclaw-runtime-controlui-persistent-image/spec.md` | Persistent delivery scope, hash/evidence requirements, secret hygiene |
| `specs/gtclaw-runtime-controlui-persistent-image/plan.md` | Digest-pinned runtime image default, hash alignment, path/source-of-truth gates |
| `specs/gtclaw-runtime-controlui-persistent-image/tasks.md` | T012-T015 HashManifestWorker boundaries and approval requirements |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-path-verification.md` | Proven runtime-served copy target and rejected unproven path |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-delivery-decision.md` | T008-T011 decision packet and future approval packet outline |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md` | Historical four-file before/after hash baseline and manual pod patch limitation |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501001159-test-env-mutation.md` | Historical T6 dev image digest, arm64 digest, runtime path, and pre-body-repair hashes |

## Proven Copy Target

Future copy target may only be:

`/usr/local/lib/node_modules/openclaw/dist/control-ui`

This is the only runtime-served control-ui copy target carried forward from the current path verification evidence.

The following path is not proven and must not be used as a copy target:

`/opt/opensparrow/runtime/openclaw/dist/control-ui`

Unless a later approved path proof changes this gate, `/opt/opensparrow/runtime/openclaw/dist/control-ui` must not be treated as source-of-truth or build target. Future source artifact, build, extraction, and deployed-pod hash checks must be interpreted against the proven `/usr/local/lib/node_modules/openclaw/dist/control-ui` target.

## Four Allowlist Files

Only these four files may enter a future artifact manifest:

| File |
| --- |
| `index.html` |
| `assets/i18n-B06L7jQN.js` |
| `assets/zh-CN-B26mMdbY.js` |
| `assets/index-M4TNVXB3.js` |

No other file may be added to the future manifest unless a later reviewed plan expands scope.

## Expected After-Hash Table

Every future source artifact, built image/resource artifact, and fresh deployed pod hash surface must match this expected after-hash table.

| File | Expected after SHA-256 | Expected size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

## Expected Before-Hash Table

The selected future base artifact must show this expected before-hash table before any future implementation continues.

| File | Expected before SHA-256 | Expected size |
| --- | --- | ---: |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` | `3395` |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` | `23248` |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` | `707543` |

## Three-Way Alignment Contract

Future T013-T015 must prove this exact contract for each allowlist file:

`source artifact hash = built image/resource hash = fresh deployed pod hash`

All three surfaces must match the expected after-hash table above. A match to historical T8I or addendum evidence is not enough for this persistent feature because those records describe an older disposable runtime state and manual pod repair context.

Fresh deployed pod hash evidence must come from a fresh disposable instance created from the future persistent artifact. It must not reuse the old T8I pod `clawreef-3-gtclaw-t8-dev-20260501001159`, and it must not rely on manual pod patch, `kubectl cp`, in-pod file replacement, process restart, or shell repair as persistence evidence.

## Drift Stop Rules

Stop for Commander review before continuing if any of these drift or scope conditions appear:

| Stop condition | Required response |
| --- | --- |
| Selected base artifact before hash differs from the expected before-hash table | Stop; do not reinterpret the baseline without Commander review |
| Built artifact after hash differs from source artifact after hash | Stop; do not publish, tag, deploy, or compare onward |
| Fresh deployed pod hash differs from built artifact hash | Stop; do not treat the artifact as proven persistent delivery |
| Any file outside the four-file allowlist changes | Stop; scope expansion requires later review |
| Copy target points to `/opt/opensparrow/runtime/openclaw/dist/control-ui` or any other unproven path | Stop; only `/usr/local/lib/node_modules/openclaw/dist/control-ui` is currently proven |
| Broad OpenClaw -> GTClaw replacement is found | Stop; preserve OpenClaw technical identity |
| Evidence contains token, cookie, secret, credential, `.env`, `.codex/auth.json`, `.codex/config.toml`, or token-bearing URL content | Stop; redact and request Commander review |

These rules are drift stop conditions, not automatic repair instructions.

## Copy Target Constraints

Future manifest work must keep copy and extraction paths scoped to the proven runtime-served directory:

- Source artifact paths must map one-to-one onto `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- Built image/resource extraction must read the same four relative paths under `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- Fresh deployed pod extraction must read the same four relative paths under `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- `/opt/opensparrow/runtime/openclaw/dist/control-ui` remains forbidden as source-of-truth or build target unless separate path proof is later approved.
- The manifest must not include broad OpenClaw -> GTClaw replacement, package identity changes, CLI/config literal changes, API/DB/K8S/runtime identifier changes, or unrelated asset churn.

## Future Approval Dependencies

| Future task | Approval dependency |
| --- | --- |
| T013 built artifact hash extraction | Requires explicit approval for image/resource build, pull, save, inspect, or artifact extraction before execution. |
| T014 fresh deployed pod hash extraction | Requires explicit approval for fresh disposable instance creation and pod/container read access before execution. |
| T015 comparison | Comparison itself may be read-only, but any follow-up artifact, image, registry, runtime, Kubernetes, database, or browser action still requires explicit approval. |

## Scope Statement

Only this file is written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-hash-manifest-design.md`

This T012 evidence design does not read, create, or modify future source artifact directories. It does not modify spec, plan, tasks, existing evidence, backend, frontend, deployments, docs, longterm, AgentTeam, runtime pod/container files, runtime/app images, registry tags, Kubernetes resources, Secrets, ConfigMaps, namespaces, or databases.

## Gate Statement

Hash manifest design only, not implementation, no image/registry/runtime/K8S mutation, no database mutation, no fresh instance, no browser E2E, no passes:true, no Close.
