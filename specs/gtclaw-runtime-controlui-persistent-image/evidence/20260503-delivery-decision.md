# Delivery Decision Packet - 2026-05-03

## Decision Status

DECISION PACKET ONLY - NOT IMPLEMENTATION APPROVAL.

This packet is a Commander/user decision aid for T008-T011. It grants no implementation permission, no image build/tag/push/pull/inspect permission, no registry/resource mutation permission, no runtime/container mutation permission, no Kubernetes/Secret/ConfigMap/namespace/database mutation permission, no fresh instance permission, no browser E2E permission, no passes:true, and no Close.

## Source Basis

| Source | Use |
| --- | --- |
| `AGENTS.md` | Project authority, forbidden actions, memory and evidence discipline |
| `.specify/memory/constitution.md` | Spec-Kit gates, E2E requirement, no `passes:true` without evidence |
| `specs/gtclaw-runtime-controlui-persistent-image/spec.md` | Feature scope, persistence requirements, forbidden actions |
| `specs/gtclaw-runtime-controlui-persistent-image/plan.md` | Default digest-pinned runtime image strategy and equivalent resource delivery constraints |
| `specs/gtclaw-runtime-controlui-persistent-image/tasks.md` | T008-T011 decision-gate boundaries and future T012-T021 approval shape |
| `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-path-verification.md` | Runtime-served path proof and rejected path gate |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md` | Four-file after-hash baseline and manual pod patch limitation |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501001159-test-env-mutation.md` | Local k3d registry test image, digest, arm64 platform digest, and historical pre-body-repair hashes |

## Path Gate Result

| Path | Decision use |
| --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui` | Proven runtime-served copy target. Future implementation planning may use this path as the only carried-forward copy target from the current path verification evidence. |
| `/opt/opensparrow/runtime/openclaw/dist/control-ui` | Rejected/unproven target. The path was missing in the inspected runtime and no symlink, same-directory, served-file, or build-time copy relationship was proven. |

Future delivery must not use `/opt/opensparrow/runtime/openclaw/dist/control-ui` without separate proof.

## Recommended Delivery Type

Recommended default strategy: digest-pinned OpenClaw runtime image/tag.

Rationale:

- The spec and plan require persistent delivery that survives runtime pod replacement without manual pod patching.
- A digest-pinned runtime image can carry the four repaired control-ui files into each fresh runtime instance while preserving OpenClaw Image technical identity.
- The image/tag label may be human-readable, but review, evidence, and rollback must rely on immutable digest identity, including image index digest and linux/arm64 manifest digest when applicable.

Equivalent resource delivery is not the default. If Commander/user chooses equivalent resource delivery, a separate decision must define:

- immutable digest/hash identity
- distribution path
- runtime materialization path
- rollback path
- fresh-instance materialization proof
- evidence showing the resource lands at `/usr/local/lib/node_modules/openclaw/dist/control-ui`

No equivalent resource delivery should be treated as interchangeable with a digest-pinned image unless those properties are explicitly decided and later proven.

## Artifact Location Recommendation

There are two different location decisions:

| Location class | Decision meaning | Current recommendation |
| --- | --- | --- |
| Local k3d registry test delivery | Short-loop implementation/evidence artifact for this local K3D/K3S environment. Historical evidence already used `localhost:5001` and `k3d-clawmanager-registry:5000` aliasing with digest proof. | Use local k3d registry first for the implementation/evidence loop after explicit future approval. |
| Persistent registry/resource delivery | Durable operator-facing delivery location outside the local test loop, or an explicitly chosen non-registry resource path. | Treat as a separate Commander/user approval decision after local evidence proves the artifact shape and path behavior. |

Recommendation: use local k3d registry first for T012-T021 implementation/evidence iteration, then decide persistent registry or persistent resource delivery as its own approval gate. This avoids mixing local proof-of-artifact mechanics with the final distribution policy.

No blocker is identified for starting with local k3d registry, provided future approval explicitly covers image build/tag/push/pull/inspect and keeps the copy target limited to `/usr/local/lib/node_modules/openclaw/dist/control-ui`.

## T008-T011 Decision Notes

| Task | Decision packet result |
| --- | --- |
| T008 | Delivery type recommendation is digest-pinned runtime image/tag, based on path proof and persistence requirements. |
| T009 | Default strategy remains digest-pinned OpenClaw runtime image/tag with immutable digest evidence. |
| T010 | Equivalent resource delivery remains a non-default Commander/user decision requiring immutable identity, distribution, rollback, and fresh-instance proof. |
| T011 | Recommended artifact sequence is local k3d registry first for the evidence loop, with persistent registry/resource location decided separately. |

## Implementation Approval Packet Outline

If Commander/user later chooses to move toward T012-T021, the next approval packet should explicitly name:

- artifact source directory
- base image digest
- target tag naming
- local/persistent registry choice
- image build/tag/push/pull/inspect approval
- allowed copy target only `/usr/local/lib/node_modules/openclaw/dist/control-ui`
- four allowlist files only:
  - `index.html`
  - `assets/i18n-B06L7jQN.js`
  - `assets/zh-CN-B26mMdbY.js`
  - `assets/index-M4TNVXB3.js`

The packet should also state whether any read-only artifact inspection, registry access, or runtime/pod read access is included, and should restate that `/opt/opensparrow/runtime/openclaw/dist/control-ui` remains unusable as a copy target without separate proof.

## Non-Goals / Forbidden Carry-Forward

- no broad OpenClaw -> GTClaw replacement
- no backend/frontend/deployments/longterm changes
- no deployment default change unless later explicitly approved
- no manual pod patch as persistence evidence
- no token/cookie/secret/token-bearing URL in evidence
- no app image mutation
- no Kubernetes resource, Secret, ConfigMap, namespace, or database mutation under this packet
- no registry mutation under this packet
- no runtime pod/container file mutation under this packet
- no fresh instance under this packet
- no browser E2E under this packet
- no passes:true
- no Close

## Open Questions For Commander/User

1. What is the final registry/resource location for persistent delivery after the local evidence loop?
2. Is persistent registry delivery required before the first fresh E2E, or may local k3d registry evidence run first?
3. Does equivalent resource delivery remain out of scope unless separately selected?
4. Should the next step be a T012-T016 approval packet covering manifest design and implementation authorization boundaries only?

## Scope Statement

Only this file is written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260503-delivery-decision.md`

This packet does not modify spec, plan, tasks, path verification evidence, backend, frontend, deployments, docs, longterm, AgentTeam, old specs/evidence, runtime pod/container files, runtime/app images, registry tags, Kubernetes resources, Secrets, ConfigMaps, namespaces, or databases.

## Gate Statement

Decision packet only, not implementation, no image/registry/runtime/K8S mutation, no database mutation, no fresh instance, no browser E2E, no passes:true, no Close.
