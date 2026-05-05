# Implementation Approval Packet - 2026-05-03

## Status

IMPLEMENTATION APPROVAL PACKET DRAFT ONLY - NOT APPROVAL GRANTED.

This T016 packet is a Commander/user decision aid for the later T017-T021 image delivery loop. It does not grant implementation permission, source artifact directory access, image build/tag/push/pull/inspect permission, built artifact extraction permission, registry mutation permission, runtime pod/container access or mutation permission, Kubernetes resource/Secret/ConfigMap/namespace mutation permission, database mutation permission, fresh instance permission, browser E2E permission, no passes:true, and no Close.

## Source Basis

| Source | Use |
| --- | --- |
| `AGENTS.md` | Project authority, forbidden actions, memory discipline, secret hygiene |
| `.specify/memory/constitution.md` | Spec-Kit gates, E2E evidence requirement, write-back limits |
| `specs/gtclaw-runtime-controlui-persistent-image/spec.md` | Persistent delivery scope, FR-001 through FR-011, evidence and stop gates |
| `specs/gtclaw-runtime-controlui-persistent-image/plan.md` | digest-pinned runtime image default, rollback and evidence shape |
| `specs/gtclaw-runtime-controlui-persistent-image/tasks.md` | T016 boundary and future T017-T021 approval dependencies |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-path-verification.md` | Proven copy target and rejected path |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-delivery-decision.md` | digest-pinned image recommendation and local k3d registry first loop |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-hash-manifest-design.md` | four allowlist files, expected before/after hashes, T013-T015 gates |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md` | Historical repaired file hash baseline and disposable pod patch limitation |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501001159-test-env-mutation.md` | Historical local k3d image/tag, base digest, image index digest, linux/arm64 manifest digest |

## Recommended Implementation Path

Recommended path for user decision: digest-pinned OpenClaw runtime image/tag, using local k3d registry first for the implementation/evidence loop.

Rationale:

- The feature requires persistence without manual pod patch.
- The proven runtime-served copy target is `/usr/local/lib/node_modules/openclaw/dist/control-ui`.
- The default strategy remains an OpenClaw Image derived runtime artifact with GTClaw control-ui resources.
- The local k3d registry is the lowest-scope first evidence loop because historical evidence already recorded local host registry and in-cluster alias digest behavior.
- Persistent registry or equivalent resource delivery remains a separate later decision after the local loop proves artifact shape, path behavior, hashes, and rollback evidence.

Equivalent resource delivery is not the default in this packet. If Commander/user later chooses that path, it needs a separate approval packet naming immutable digest/hash identity, distribution path, materialization path, rollback method, and fresh-instance proof.

## Approved Target Constraints To Request

| Constraint | Requested value |
| --- | --- |
| Copy target only | `/usr/local/lib/node_modules/openclaw/dist/control-ui` |
| Rejected target | `/opt/opensparrow/runtime/openclaw/dist/control-ui` |
| File scope | four allowlist files only |
| Replacement scope | no broad OpenClaw -> GTClaw replacement |
| Technical identity | preserve OpenClaw technical identity/literals |

Four allowlist files:

- `index.html`
- `assets/i18n-B06L7jQN.js`
- `assets/zh-CN-B26mMdbY.js`
- `assets/index-M4TNVXB3.js`

Protected technical identity/literals to preserve include `openclaw`, `OpenClaw Image`, `.openclaw`, `openclaw.json`, `openclaw dashboard --no-open`, `dist/control-ui`, package identity, CLI/config literals, API/DB/K8S/runtime identifiers, image lineage, image names, image tags, image digests, protocol/config strings, and compatibility identifiers.

## Explicit Approval Items Needed Before T017-T021

- [ ] artifact source directory approval
- [ ] base image digest approval
- [ ] target tag naming approval
- [ ] local k3d registry approval
- [ ] image build approval
- [ ] image tag approval
- [ ] image push/publish approval
- [ ] image pull/inspect approval
- [ ] built artifact extraction approval
- [ ] permission to record image index digest and linux/arm64 manifest digest
- [ ] rollback target/command approval
- [ ] secret hygiene/redaction constraints acknowledged

## Proposed Concrete Values / Decisions Needed

These are candidates from historical evidence and current decision packets. They are not approval grants.

| Item | Candidate value | Decision state |
| --- | --- | --- |
| Historical host local dev image/tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` | Historical reference only; Commander/user decision needed for new tag |
| Historical in-cluster local k3d alias | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-resources-dev-20260430192029` | Historical reference only; Commander/user decision needed for new loop |
| Known base digest candidate | `sha256:26bc8e3a16e45589927183a220b5683c7b3ab4563fca91d39af7fa4b64b11db8` | Commander/user decision needed |
| Historical T6 image index digest | `sha256:b5cef803f8eda8b3e0d3dc99d04147249a5c6354805501dfae2ae555484a4d45` | Historical reference only; not a new T017-T021 output |
| Historical T6 linux/arm64 manifest digest | `sha256:112421e96f10a9b45fb679ae1784f269c552680f19066e754ebff0fec11c7984` | Historical reference only; new loop must record its own linux/arm64 digest |
| Recommended new tag naming pattern | `gtclaw-controlui-persistent-YYYYMMDDHHMMSS` | Commander/user decision needed |
| Artifact source directory | Commander/user decision needed | Do not read or create in T016 |
| Final persistent registry/resource location | Commander/user decision needed | Separate later decision after local evidence loop |
| Rollback target and command | Commander/user decision needed | Must be digest-pinned or hash-pinned, not mutable tag-only |

Expected before-hash table from T012:

| File | Expected before SHA-256 | Expected size |
| --- | --- | ---: |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` | `3395` |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `42702` |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` | `23248` |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` | `707543` |

Expected after-hash table from T012:

| File | Expected after SHA-256 | Expected size |
| --- | --- | ---: |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` | `3398` |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` | `42617` |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` | `23255` |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` | `707959` |

## T013-T015 Gating Map

| Future task | Gate |
| --- | --- |
| T013 built artifact hash extraction | T013 cannot run until image/resource build, pull, save, inspect, or artifact extraction is explicitly approved. |
| T014 fresh deployed pod hash extraction | T014 cannot run until fresh disposable instance creation and pod/container read access are explicitly approved. |
| T015 three-way comparison | T015 comparison may be read-only after T013 and T014 evidence exists, but any follow-up image, artifact, registry, runtime, Kubernetes, database, or browser action still needs explicit approval. |

T013-T015 evidence must prove:

`source artifact hash = built artifact hash = fresh deployed pod hash`

All three surfaces must align with the expected after-hash table for the four allowlist files. Old T8I, addendum, or test-environment mutation evidence remains historical baseline only.

## Stop Conditions

Stop and return to Commander review if any condition appears:

- before hash drift from the expected before-hash table
- after hash mismatch between source artifact, built artifact, and fresh deployed pod
- allowlist expansion beyond the four allowlist files
- unproven copy target or any attempt to use `/opt/opensparrow/runtime/openclaw/dist/control-ui`
- broad OpenClaw replacement or OpenClaw technical identity/literal rewrite
- token/cookie/secret/token-bearing URL leakage
- mutable tag-only rollback
- deployment default change without later explicit approval
- registry credential exposure or secret-bearing command output
- app image build or deployment manifest change entering this runtime image loop
- manual pod patch used as persistence evidence

## User Approval Text

The text below is a copy-ready approval template. It is not approval unless the user sends it as their own instruction.

```text
I explicitly approve T017-T021 local k3d image delivery implementation/evidence loop under the constraints in specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-implementation-approval-packet.md.

Allowed files/resources/actions:
- Use artifact source directory: <Commander/user approved source artifact directory>.
- Use base image digest: <Commander/user approved digest>.
- Use target tag: <Commander/user approved tag, recommended pattern gtclaw-controlui-persistent-YYYYMMDDHHMMSS>.
- Use local k3d registry for this implementation/evidence loop.
- Copy only these four files into /usr/local/lib/node_modules/openclaw/dist/control-ui:
  - index.html
  - assets/i18n-B06L7jQN.js
  - assets/zh-CN-B26mMdbY.js
  - assets/index-M4TNVXB3.js
- Build the digest-pinned OpenClaw runtime image.
- Tag the image with the approved target tag.
- Push/publish the image to the approved local k3d registry.
- Pull/inspect the approved image only for digest and hash evidence.
- Extract built artifact hashes only for the four allowlist files.
- Record image index digest and linux/arm64 manifest digest.
- Record redacted build/tag/push/pull/inspect commands and rollback target/commands.

Forbidden actions:
- Do not use /opt/opensparrow/runtime/openclaw/dist/control-ui as copy target.
- Do not perform broad OpenClaw -> GTClaw replacement.
- Do not change backend/**, frontend/**, deployments/**, docs/**, longterm/**, AgentTeam/**, spec.md, plan.md, tasks.md, or existing evidence files.
- Do not mutate runtime pod/container files.
- Do not mutate Kubernetes resources, Secrets, ConfigMaps, namespaces, or database state.
- Do not create a fresh instance or run browser E2E under this approval text unless I separately approve those later gates.
- Do not change deployment defaults.
- Do not overwrite latest or rely on mutable tag-only rollback.
- Do not output token values, cookie values, credentials, secrets, .env content, .codex/auth.json, .codex/config.toml, or token-bearing URL content.
- Do not write passes:true or Close state.
```

## Scope Statement

Only this file is written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-implementation-approval-packet.md`

This T016 packet does not read or create a source artifact directory. It does not modify spec, plan, tasks, existing evidence, backend, frontend, deployments, docs, longterm, AgentTeam, runtime pod/container files, runtime/app images, registry tags, Kubernetes resources, Secrets, ConfigMaps, namespaces, or databases.

## Gate Statement

Approval packet draft only, not implementation, no image/registry/runtime/K8S mutation, no database mutation, no fresh instance, no browser E2E, no passes:true, no Close.
