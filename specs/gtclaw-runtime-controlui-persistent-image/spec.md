# Feature Specification: GTClaw Runtime Control-UI Persistent Image Delivery

**Feature Branch**: `not-created`
**Feature Short Name**: `gtclaw-runtime-controlui-persistent-image`
**Created**: 2026-05-03
**Status**: Draft - spec-only for Commander review; not approved for plan, tasks, implementation, image build, registry push, runtime mutation, Kubernetes mutation, `passes:true`, or Close/write-back
**Input**: Follow-up feature from `gtmanager-gtclaw-m1-runtime-localization` F-006 Close state, T8I evidence, and runtime control-ui body repair manifest addendum.

## Dependency Gate

This feature may only proceed as a spec-only draft because the prerequisite GTManager / GTClaw M1 localization work has already reached its Close/write-back gate:

- `gtmanager-gtclaw-m1-runtime-localization` has completed Close/write-back.
- `longterm/workspace/feature_list.json` records F-006 with `passes: true`.
- T8I evidence exists at `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-runtime-body-fix.md`.
- Patch manifest addendum exists at `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md`.
- Residual risk remains: the runtime control-ui body repair is disposable pod patch evidence, not a persistent runtime image rebuild or equivalent persistent resource delivery.

## Problem Statement

F-006 satisfied its corrected E2E gate for authorized GTManager `/control-ui/` root, chat, and history fallback routes. That result proves the current local environment can render the repaired GTClaw control-ui body through the authorized route while preserving `/proxy/` desktop behavior.

The remaining delivery problem is persistence. The current runtime control-ui body repair lives as a disposable pod patch record. If the runtime pod restarts, the instance is recreated, or the runtime image is redeployed from the recorded image/tag, the body repair can be lost unless the same four repaired files are delivered through a persistent image layer or an equivalent auditable runtime resource mechanism.

This feature exists to define the persistent delivery contract for those repaired control-ui resources. It does not itself build an image, push a tag, patch a runtime pod, mutate Kubernetes resources, or change deployment defaults.

## Scope

In scope:

- Persist the repaired runtime control-ui allowlist files into a new runtime image/tag or an equivalent auditable resource delivery:
  - `index.html`
  - `assets/i18n-B06L7jQN.js`
  - `assets/zh-CN-B26mMdbY.js`
  - `assets/index-M4TNVXB3.js`
- Preserve OpenClaw Image technical identity, image lineage, package identity, paths, CLI/config literals, protocols, runtime status fields, API/DB/K8S identifiers, and compatibility identifiers.
- Keep the user-visible runtime product surface as GTClaw where F-006 T8I criteria require it.
- Define evidence required to prove the repaired body survives a fresh disposable instance created without manual pod patching.
- Define rollback requirements for returning to the prior image digest/tag or prior file hash set.

Out of scope for this feature:

- Wrapper wording or manager UI copy changes.
- Backend proxy redesign.
- Frontend lint cleanup.
- Query-token security hardening.
- Bridge/process supervision hardening.
- Broad OpenClaw rename or OpenClaw-to-GTClaw search/replace.
- Deployment manifest default change unless a later plan and explicit user approval include it.
- App image rebuild.
- Registry mutation, runtime pod mutation, Kubernetes mutation, Secret/ConfigMap mutation, or database mutation during this spec-only phase.

## User Stories and Tests

### User Story 1 - Persistent Control-UI Resources (Priority: P1)

As an operator, I need a persistent runtime delivery artifact so the repaired GTClaw control-ui body survives runtime pod replacement.

**Independent test**: Create a new disposable runtime instance from the proposed persistent image/tag or equivalent resource delivery. Without any manual pod patch, open authorized GTManager `/control-ui/`, `/control-ui/chat?session=main`, and a history fallback path. The browser DOM still satisfies the T8I GTClaw and Chinese body criteria.

**Acceptance scenarios**:

1. **Given** the F-006 disposable pod after-hashes, **When** the persistent runtime artifact is inspected, **Then** it contains the same approved after-hash content for the four allowlist files or a documented equivalent resource delivery hash set.
2. **Given** a fresh disposable instance created from the persistent artifact, **When** no manual pod patch is applied, **Then** authorized GTManager `/control-ui/` root, chat, and history fallback still render `GTClaw 控制台`, `lang=zh-CN`, required Chinese body markers, no desktop fallback, and no 404 marker.
3. **Given** the same instance, **When** `/api/v1/instances/:id/proxy/` is opened through authorized desktop access, **Then** desktop regression remains preserved and the desktop route is not replaced by control-ui.

### User Story 2 - OpenClaw Technical Identity Preservation (Priority: P1)

As a reviewer, I need proof that persistent GTClaw resources do not rewrite OpenClaw Image technical identity.

**Independent test**: Scan the persistent runtime artifact and deployed instance for protected technical literals and compare against F-006 preservation rules.

**Acceptance scenarios**:

1. **Given** the persistent image/resource artifact, **When** protected literal scans run, **Then** `openclaw`, `OpenClaw Image`, `.openclaw*`, `openclaw.json`, `openclaw dashboard --no-open`, `dist/control-ui`, package identity, paths, protocols, image identifiers, and API/DB/K8S/runtime identifiers remain preserved where technical.
2. **Given** any ambiguous `OpenClaw` occurrence in minified/static resources, **When** its context is not proven to be user-visible display copy, **Then** the occurrence is preserved or explicitly deferred.
3. **Given** the artifact is named or tagged, **When** release notes or evidence describe it, **Then** it remains an OpenClaw Image derived runtime with GTClaw resources, not a renamed or forked technical runtime.

### User Story 3 - Auditable Delivery and Rollback (Priority: P1)

As an operator, I need the persistent runtime delivery to be reproducible, auditable, and reversible.

**Independent test**: Review the delivery manifest and verify base digest, new digest, file hashes, build/tag command, deployed imageID, and rollback path.

**Acceptance scenarios**:

1. **Given** the selected base image digest, **When** the persistent artifact is built or assembled, **Then** evidence records the exact base image digest, new image/index digest or equivalent resource digest, linux/arm64 manifest digest when applicable, build command, tag, and time.
2. **Given** the four allowlist files, **When** before/after hashes are compared, **Then** only those approved files differ for the control-ui repair delivery.
3. **Given** rollback is required, **When** an operator follows the documented rollback path, **Then** the runtime can return to the previous image digest/tag or previous file hash set without requiring secret material.

## Functional Requirements

- **FR-001**: The persistent delivery must include the repaired body content for exactly these default allowlist files unless a later reviewed plan expands scope: `index.html`, `assets/i18n-B06L7jQN.js`, `assets/zh-CN-B26mMdbY.js`, and `assets/index-M4TNVXB3.js`.
- **FR-002**: The persistent delivery must be either a new runtime image/tag or an equivalent auditable runtime resource delivery with stable digest/hash evidence.
- **FR-003**: The delivery must preserve OpenClaw Image technical identity and must not rename package paths, CLI/config literals, API paths, DB/K8S identifiers, runtime type literals, image lineage, or compatibility identifiers.
- **FR-004**: The delivery must not rely on manual disposable pod patching for acceptance.
- **FR-005**: A fresh disposable runtime instance must satisfy authorized GTManager `/control-ui/` root, `/control-ui/chat?session=main`, and history fallback DOM criteria without manual pod mutation.
- **FR-006**: Existing authorized `/proxy/` desktop access must remain a desktop regression check and must not be repointed to control-ui.
- **FR-007**: Evidence must record base digest, new image/resource digest, before/after file hash manifest, build command, tag or resource identity, deployed instance imageID, E2E route results, desktop regression, protected literal scan, rollback path, and lint status.
- **FR-008**: Lint status must be recorded separately and must not be silently waived by this feature.
- **FR-009**: Evidence, commands, and reports must not output token values, cookie values, credentials, secrets, token-bearing URLs, `.env`, `.codex/auth.json`, or `.codex/config.toml`.
- **FR-010**: No `passes:true`, Close, complete, accepted, or passed claim may be made before plan/tasks, explicit implementation approval, and E2E evidence exist.
- **FR-011**: Before any persistent image/resource implementation, the plan must prove the relationship between `/usr/local/lib/node_modules/openclaw/dist/control-ui` and `/opt/opensparrow/runtime/openclaw/dist/control-ui`, and must prove that the image/resource copy target is the filesystem path actually served by the runtime control-ui process.

## Required Evidence

The later implementation plan must require an evidence packet with at least:

| Evidence | Required content |
| --- | --- |
| Base image digest | Exact source OpenClaw Image digest used for the persistent delivery, including index and platform digest when available |
| New image/resource digest | New image index digest and linux/arm64 manifest digest, or equivalent resource delivery digest/hash |
| File hash manifest | Before and after SHA-256 plus byte size for the four allowlist files |
| Build command / tag | Exact redacted build or assembly command, output tag/resource identity, and build timestamp |
| Deployed instance imageID | Runtime pod image, imageID, platform digest, namespace/name, and restart count for the fresh disposable instance |
| Authorized GTManager E2E | `/control-ui/`, `/control-ui/chat?session=main`, and history fallback browser DOM criteria matching F-006 T8I body requirements |
| Desktop regression | `access?mode=desktop` and `/proxy/` still render desktop behavior |
| Protected literal scan | OpenClaw Image and protected technical literals preserved; ambiguous literals classified or deferred |
| Rollback proof | Rollback target and operator steps to return to prior image digest/tag or prior file hash set |
| Lint status | Frontend lint status recorded separately, including any existing debt and explicit waiver state if later approved |
| Secret hygiene | Confirmation that no token/cookie/secret/credential or token-bearing URL is recorded |

## Success Criteria

- **SC-001**: A new persistent runtime image/tag or equivalent resource delivery contains the body repair after-hashes for the allowlist files.
- **SC-002**: A newly created disposable runtime instance uses the persistent artifact and records the deployed imageID or resource digest.
- **SC-003**: Without manual pod patching, authorized GTManager `/control-ui/` root renders the required GTClaw Chinese DOM body.
- **SC-004**: Without manual pod patching, authorized GTManager `/control-ui/chat?session=main` renders the required GTClaw Chinese DOM body.
- **SC-005**: Without manual pod patching, authorized GTManager history fallback renders the required GTClaw Chinese DOM body and does not fall back to desktop or 404.
- **SC-006**: `/proxy/` desktop regression remains preserved.
- **SC-007**: Protected OpenClaw technical literals remain preserved.
- **SC-008**: Rollback path is explicit and does not require secret disclosure.
- **SC-009**: Lint status is separately recorded and not silently waived.
- **SC-010**: Evidence contains no token, cookie, secret, credential, or token-bearing URL.

## Risks

- **Base image drift**: A floating tag or rebuilt upstream OpenClaw Image may change file hashes, package contents, or platform manifests.
- **Local registry vs persistent registry ambiguity**: T6 and T8I used local k3d registry references; this follow-up must clarify whether the final delivery lives in local test registry, a persistent registry, or an equivalent non-registry resource path.
- **arm64 manifest/index digest mismatch**: Image index digest and linux/arm64 manifest digest are different evidence items; recording only one can make Mac/ARM validation ambiguous.
- **Static minified bundle patch auditability**: The runtime control-ui bundle is minified and may lack sourcemaps, so audit must rely on allowlist scope, exact hashes, display-string summary, and browser DOM E2E.
- **Old disposable pod evidence reuse risk**: F-006 T8I and the addendum are valid historical evidence, but they cannot prove this persistent feature unless a fresh instance uses the persistent artifact without manual patching.
- **Rollback ambiguity**: A tag-only rollback can be unsafe if the tag is mutable; rollback should prefer digest-pinned image/resource identity.
- **Secret leakage risk**: Access flow evidence must not record token-bearing URLs, cookies, credentials, or secrets.

## Gates

1. Spec review is required before `plan.md` or `tasks.md`.
2. Plan and tasks are required before any implementation.
3. Explicit user approval is required before any image build, tag, push, runtime mutation, Kubernetes mutation, registry mutation, or resource delivery mutation.
4. E2E evidence is required before any `passes:true`, Close, complete, passed, or accepted claim.
5. Deployment manifest default changes are forbidden unless a later reviewed plan and explicit user approval include them.
6. No runtime pod manual patch may be used as acceptance evidence for persistence.
7. No token, cookie, secret, credential, or token-bearing URL may appear in evidence.

## Scope Statement

This spec-only draft creates the requirements for persistent runtime control-ui image/resource delivery. It does not authorize implementation, image rebuild, registry push, runtime pod patch, app image rebuild, Kubernetes mutation, Secret/ConfigMap mutation, database mutation, deployment manifest edit, `longterm/**` write-back, old M1 file change, `passes:true`, or Close.
