# GTClaw Runtime Control-UI Persistent Image Delivery Plan

> **For future agentic workers:** this is a plan-only draft for Commander review. It does not authorize tasks, implementation, image build, image tag, image push, image pull, runtime pod mutation, Kubernetes mutation, registry mutation, database mutation, `passes:true`, or Close.

**Goal:** Define how to turn the F-006 disposable runtime control-ui body repair into persistent, auditable runtime image/resource delivery.

**Architecture:** Prefer a digest-pinned OpenClaw Image derived runtime image layer that replaces only the four approved control-ui allowlist files. Any equivalent resource delivery must be explicitly reviewed as a Commander/user decision because it must prove the same persistence, auditability, and rollback properties as an image digest.

**Tech Stack:** ClawManager, K3S/K3D, Kubernetes-managed OpenClaw runtime image, OpenClaw `openclaw@2026.4.14`, runtime control-ui static assets, authorized GTManager `/control-ui/` and `/proxy/` routes.

---

## Current Gate

Commander has reviewed `specs/gtclaw-runtime-controlui-persistent-image/spec.md` and allowed this plan-only draft. The following remain unauthorized:

- `tasks.md`
- implementation
- image build, tag, push, or pull
- runtime pod/container file mutation
- Kubernetes resource, Secret, ConfigMap, namespace, registry, or database mutation
- fresh instance creation
- authorized browser E2E
- `passes:true`
- Close/write-back

This plan only writes `specs/gtclaw-runtime-controlui-persistent-image/plan.md`.

## Source Evidence Used

| Source | Plan use |
| --- | --- |
| `specs/gtclaw-runtime-controlui-persistent-image/spec.md` | Feature scope, gates, acceptance criteria, required evidence |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-runtime-body-fix.md` | Historical T8I route, desktop regression, hash, lint, and protected-literal evidence |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260503-runtime-controlui-body-repair-manifest-addendum.md` | Disposable pod body repair before/after hashes and rollback record |
| `specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260501001159-test-env-mutation.md` | T6 dev image digest, platform digest, registry alias, pod imageID, and original pre-body-repair hashes |
| `longterm/workspace/feature_list.json` | F-006 is recorded with `passes: true` and residual persistence risk |
| `AgentTeam/01-Commander-SOP.md` and `AgentTeam/03-Dispatch-Templates.md` | Serial planning boundary, validation evidence discipline, and future dispatch shape |

## Delivery Strategy

### Preferred Strategy: Digest-Pinned Runtime Image/Tag

The preferred delivery is a new OpenClaw Image derived runtime image/tag whose base is pinned by digest and whose delta is limited to the four repaired control-ui allowlist files.

Required properties:

- The base image must be identified by immutable digest, not a mutable tag alone.
- The new artifact must record image index digest and linux/arm64 platform manifest digest when using an OCI image index.
- The tag may be human-readable, but every acceptance and rollback reference must include digest-pinned identity.
- The artifact must remain an OpenClaw Image derived runtime with GTClaw resources. It must not be renamed as a separate technical runtime identity.
- The image layer must place repaired files at the runtime-served control-ui path proven by the path/source-of-truth gate.
- It must not alter backend, frontend, app image, deployment manifests, database, Secrets, ConfigMaps, namespaces, or unrelated runtime files.

This strategy directly addresses the disposable pod risk: a fresh runtime pod created from the digest-pinned image should contain the repaired files without manual pod patching.

### Alternative Strategy: Equivalent Resource Delivery

An equivalent resource delivery is allowed only as a Commander/user decision because it is not automatically equivalent to a persistent image.

To be considered equivalent, the resource delivery must prove all of the following:

- **Persistence:** fresh disposable runtime instances receive the four repaired files without manual pod patching.
- **Auditability:** the resource artifact has stable digest/hash identity and records before/after file hashes for the four allowlist files.
- **Runtime path correctness:** the resource is materialized at the path actually served by the running control-ui process.
- **Rollback:** rollback returns to a prior digest/hash state and does not depend on a mutable tag-only reference.
- **Scope control:** only the four allowlist files are changed unless a later reviewed plan expands scope.
- **Secret hygiene:** no token, cookie, credential, secret, or token-bearing URL is needed in the artifact or evidence.

Commander/user decision needed before choosing this path:

- Decide whether equivalent resource delivery is acceptable instead of a digest-pinned image.
- Decide where the resource artifact lives and how it is distributed to runtime instances.
- Decide what immutable digest/hash identity replaces image index and platform digest evidence.
- Decide how rollback is executed without relying on manual pod patching.

Until those decisions are explicit, the plan defaults to digest-pinned image delivery.

## Path / Source-of-Truth Verification

There are two known control-ui paths that must be reconciled before any implementation:

- Runtime-observed path from F-006 evidence: `/usr/local/lib/node_modules/openclaw/dist/control-ui`
- Historical image integration path from prior intake memory: `/opt/opensparrow/runtime/openclaw/dist/control-ui`

Implementation must not assume these paths are equivalent. Before any image/resource mutation, a future PathVerificationWorker must prove the source-of-truth relationship in read-only mode.

Required proof:

1. Identify the runtime process that serves `control-ui` and the filesystem path it reads.
2. Confirm whether `/usr/local/lib/node_modules/openclaw/dist/control-ui` and `/opt/opensparrow/runtime/openclaw/dist/control-ui` are the same directory, a symlink relationship, a build-time copy relationship, or unrelated paths.
3. Confirm the image copy target used by the build/resource delivery lands on the path actually read at runtime.
4. Record `readlink`, inode/device, package metadata, launcher arguments, and served-file hash evidence in redacted form.
5. Fail the gate if a file copied to one path does not change the content served through authorized `/control-ui/` in a controlled test environment.

This path verification is a precondition for both image and equivalent resource delivery.

## Hash Manifest Contract

The manifest must align three hash surfaces for the four allowlist files:

1. **Source artifact hash:** the repaired file content prepared for the new image/resource artifact.
2. **Built image/resource hash:** the same file content extracted from the built image/resource artifact.
3. **Deployed pod hash:** the same file content observed in the fresh disposable runtime pod created from the persistent artifact.

All three surfaces must match for every allowlist file:

| File | F-006 disposable after SHA-256 / size | Required future alignment |
| --- | --- | --- |
| `index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` / `3398` | source artifact hash = built artifact hash = fresh deployed pod hash |
| `assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` / `42617` | source artifact hash = built artifact hash = fresh deployed pod hash |
| `assets/zh-CN-B26mMdbY.js` | `37337fecb0197581f7985ff2a004c60416aa4731315c8f7fa0e38dfc43c68809` / `23255` | source artifact hash = built artifact hash = fresh deployed pod hash |
| `assets/index-M4TNVXB3.js` | `1dab28c0ea24a293aa511612eca0e7c8998ddbb556ec19a1ef6396ff5201b299` / `707959` | source artifact hash = built artifact hash = fresh deployed pod hash |

The manifest must also record before hashes. Known historical before states are split by evidence stage and must not be conflated:

| File | T6 dev image pre-body-repair SHA-256 | Addendum backup before SHA-256 |
| --- | --- | --- |
| `index.html` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` | `ed3560d9fa9b9156e62a405bc185c2d3495129ee3712ef8c536767f79d5778c7` |
| `assets/i18n-B06L7jQN.js` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` | `09c2150d93520982b375edbf1f59905169cbfe607d009c3434088ccb6894e406` |
| `assets/zh-CN-B26mMdbY.js` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` | `9a4ecc8992d00443ef59de0be41090099d5a1feb25cf062c5c02470044277f29` |
| `assets/index-M4TNVXB3.js` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` | `eb8379dce913df535806486c8a4f637ab27f069c95925ef836821fbde2cb321b` |

If future baseline inspection finds a different before hash, the plan must stop for Commander review. Different before hashes may indicate base image drift, stale local registry content, or copying to the wrong control-ui path.

## OpenClaw Technical Identity Preservation

The delivery must preserve OpenClaw technical identity. Protected scans must cover both the built artifact and the fresh deployed pod.

Protected literals and identifier categories:

- `openclaw`
- `OpenClaw Image`
- `.openclaw`
- `openclaw.json`
- `openclaw dashboard --no-open`
- `dist/control-ui`
- `/usr/local/lib/node_modules/openclaw/dist/control-ui`
- `/opt/opensparrow/runtime/openclaw/dist/control-ui` when present as source/build path
- package name and package version
- CLI, config, module, client, protocol, localStorage, API, DB, K8S, runtime status, image name, image tag, and image digest identifiers

Preservation rules:

- Do not use broad OpenClaw-to-GTClaw replacement.
- Lowercase `openclaw` remains technical unless a specific future review proves otherwise.
- Ambiguous `OpenClaw` occurrences default to preserve or defer.
- User-visible GTClaw display changes are limited to the four approved control-ui files and already repaired display contexts.
- Evidence must distinguish static dictionary/source strings from browser-visible DOM body text.

## Evidence Plan

Future evidence must be written only after explicit user approval for implementation and evidence collection. The evidence packet must include:

| Evidence item | Required proof |
| --- | --- |
| Base digest | Exact base OpenClaw Image digest; include tag only as label, not identity |
| New digest | New image/resource digest; for OCI image, record index digest |
| Platform digest | linux/arm64 manifest digest and platform metadata |
| Build command/tag/timestamp | Redacted build/assembly command, output tag or resource identity, timestamp, and builder host context without secrets |
| Path proof | Proof that copied files land on the runtime-served control-ui path, including `/usr/local/...` and `/opt/opensparrow/...` relationship |
| Hash manifest | Before SHA-256/size, source artifact SHA-256/size, built artifact SHA-256/size, fresh deployed pod SHA-256/size for each allowlist file |
| Deployed imageID | Fresh disposable instance namespace/name, pod image, imageID, platform digest, restart count, and runtime package version |
| Fresh disposable instance | Created from persistent artifact; no manual pod patch; no old disposable instance reuse |
| Authorized GTManager E2E | Browser DOM evidence for `/control-ui/`, `/control-ui/chat?session=main`, and history fallback with `GTClaw 控制台`, `lang=zh-CN`, Chinese body markers, no desktop fallback, and no 404 marker |
| Desktop regression | `access?mode=desktop` and `/proxy/` still render desktop behavior and keep desktop assets under `/proxy/` |
| Protected literal scan | Built artifact and deployed pod preserve protected OpenClaw technical literals |
| Rollback proof | Digest-pinned rollback target and operator steps; no mutable tag-only rollback |
| Lint status | Current frontend lint status recorded separately; any waiver must be explicit and scoped |
| Secret hygiene | No token/cookie value, credential, secret, token-bearing URL, `.env`, `.codex/auth.json`, or `.codex/config.toml` content |

Old T8I and addendum evidence may be referenced as historical baseline only. They cannot be reused as proof that this persistent feature passes because they were produced from a disposable pod state.

## Rollback Plan

Rollback must prefer digest-pinned image/resource identity.

Accepted rollback evidence:

- Prior base image digest and platform digest.
- Prior file hash set for the four allowlist files.
- Operator steps to restore the image/resource reference to the prior digest.
- Verification that a fresh runtime pod created after rollback contains the prior hash set.

Rejected rollback evidence:

- Mutable tag-only rollback.
- Manual `kubectl exec` file copy into a running pod as the only rollback path.
- Rollback that requires exposing credentials, token-bearing URLs, cookies, or secrets.
- Rollback that changes deployment defaults without later plan and explicit user approval.

The F-006 addendum's `/tmp/gtclaw-controlui-repair-backup-20260502233047` rollback record remains disposable pod rollback evidence only. It is useful as historical before-hash evidence, not as the persistent delivery rollback mechanism.

## Risk Register

| Risk | Impact | Plan control |
| --- | --- | --- |
| Base image drift | The four allowlist before hashes or package path may differ from F-006 evidence | Pin base digest; stop if before hashes differ |
| Path mapping drift | Files copied to `/opt/opensparrow/...` may not be served from `/usr/local/...`, or vice versa | Require path/source-of-truth verification before implementation |
| Local vs persistent registry ambiguity | Local k3d registry evidence may not survive outside the test cluster | Commander/user decision required for final registry/resource location |
| arm64 index/platform digest mismatch | Recording only index digest may not prove the Mac/ARM runtime artifact | Record both image index digest and linux/arm64 manifest digest |
| Static minified JS auditability | No sourcemap means source-level proof is weak | Use allowlist, exact hashes, display summary, and browser DOM E2E |
| Old disposable evidence reuse | T8I/addendum can be mistaken as persistence proof | Require fresh disposable instance from persistent artifact with no manual patch |
| Query-token logging future risk | Access proof can leak token-bearing URLs if captured naively | Evidence must use redacted status/metadata and route cookies without values |
| Bridge/process supervision future risk | If runtime reachability later depends on a bridge, process failure can mimic UI failure | Keep this feature focused on persistence; any bridge/process hardening remains out of scope unless later approved |

## Gate / Approval Checklist

- Plan review must occur before `tasks.md`.
- Tasks review must occur before implementation.
- Explicit user approval is required before image build, tag, push, pull, runtime mutation, registry mutation, Kubernetes mutation, or database mutation.
- Explicit user approval is required before creating a fresh disposable instance or running authorized browser E2E.
- No `passes:true`, Close, complete, passed, or accepted claim before fresh E2E evidence and Commander/user Close approval.
- No deployment manifest default change unless a later plan and explicit user approval include it.
- No backend/frontend/source cleanup work is part of this feature unless a later approved plan expands scope.

## Future Worker Boundaries

The next artifact, if approved, should be `tasks.md`. It should keep worker boundaries narrow:

- **PathVerificationWorker:** read-only image/runtime path proof for `/usr/local/...` and `/opt/opensparrow/...`.
- **ImageDeliveryPlanWorker:** convert this plan into digest-pinned image or approved equivalent resource tasks.
- **HashManifestWorker:** define exact manifest generation and comparison steps.
- **EvidenceWorker:** collect fresh disposable instance and authorized GTManager E2E evidence only after explicit approval.
- **ReviewWorker:** verify scope, hashes, protected literals, rollback, lint status, and secret hygiene before Close consideration.

These are future task categories, not authorized task execution.

## Open Decisions

Commander/user decisions needed before implementation:

1. Final delivery type: digest-pinned runtime image/tag or equivalent resource delivery.
2. Final artifact location: local test registry, persistent registry, or approved non-registry resource path.
3. Whether an equivalent resource delivery is acceptable and what immutable digest/hash identity it must expose.
4. Whether deployment defaults remain unchanged or a later plan may propose a default image/reference change.
5. Which fresh disposable instance path and E2E collection method will be approved after tasks review.

## Spec Amendment Recommendation

SPEC AMENDMENT RECOMMENDED:

Add the following requirement to `specs/gtclaw-runtime-controlui-persistent-image/spec.md` in a future spec review, without changing it in this plan-only task:

```markdown
- **FR-011**: Before any persistent image/resource implementation, the plan must prove the relationship between `/usr/local/lib/node_modules/openclaw/dist/control-ui` and `/opt/opensparrow/runtime/openclaw/dist/control-ui`, and must prove that the image/resource copy target is the filesystem path actually served by the runtime control-ui process.
```

Reason: the current spec requires persistent delivery and evidence, but it does not explicitly name the known dual-path risk. The plan covers it as a hard gate; adding it to the spec would make the requirement first-class.

## Scope Statement

This is a plan-only draft. It does not authorize `tasks.md`, implementation, image build, image tag, image push, image pull, runtime pod patch, app image rebuild, Kubernetes mutation, registry mutation, Secret/ConfigMap mutation, namespace mutation, database mutation, deployment manifest edit, `longterm/**` write-back, old evidence changes, `passes:true`, or Close.
