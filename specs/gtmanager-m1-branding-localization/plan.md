# M1 GTManager Branding and Chinese Localization Plan

**Status**: Draft for M1 business implementation - Phase A governance alignment applied 2026-04-29

## 1. Goal

Create the implementation plan for M1: product-facing manager surfaces become **GTManager**, new or empty browser profiles default to Chinese, explicit user locale choices remain respected, and the existing OpenClaw runtime management path stays intact.

This draft is not a business implementation artifact. It must not be used to mark `passes:true`, Close, complete, accepted, or passed. `tasks.md` must be generated only after user approval of the business implementation scope.

## 2. Approved Decisions

1. Product-facing brand changes from ClawManager to **GTManager**.
2. Default locale becomes Chinese for empty profiles; explicit stored user locale choices are retained.
3. OpenClaw remains the runtime name. Keep `OpenClaw`, `.openclaw`, `OpenClaw Resource Management`, OpenClaw bootstrap/import/export/status, and runtime-status semantics where they name runtime behavior.
4. README and user/operator docs may update product-facing entry wording for M1. Historical release notes and evidence packets keep ClawManager for traceability.
5. OpenClaw image-internal Chinese localization is a separately gated resource package / image customization path in M1 planning. M1 must not directly modify the OpenClaw image.
6. Final logo/favicon/loading asset paths are not provided. Asset replacement is **BLOCKED** until the user provides exact source paths or files.
7. E2E is the final gate. Build, lint, unit tests, backend tests, API checks, and YAML dry-runs are prerequisite evidence only.

## 3. Non-Goals / Protected Technical Identifiers

M1 must not rename or migrate these technical identifiers:

- Kubernetes / service identifiers: `clawmanager-system`, `clawmanager-app`, `clawmanager-gateway`, `clawmanager-frontend`, `clawmanager-egress-proxy`, `clawmanager-secrets`, `clawmanager-mysql-init`, `clawmanager-app-cluster-admin`.
- API / network identifiers: `/healthz`, `/api/v1`, `https://localhost:30443`, `http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001`.
- Database / schema identifiers: database `clawmanager`, table names including `users`, `instances`, `audit_logs`, `system_image_settings`, `openclaw_config_resources`, `openclaw_config_bundles`, `skill_blobs`, `skill_versions`, `instance_skills`, and related SQL migration names.
- Runtime identifiers: `openclaw`, `OpenClaw Desktop`, `.openclaw`, OpenClaw resource/config/import/export/bootstrap/status terminology, `clawmanager-openclaw-image`.
- Code / build identifiers: Go module `clawreef`, container image `ghcr.io/yuan-lab-llm/clawmanager:latest`, existing Kubernetes deployment image names, runtime image records, and local storage key `clawmanager_locale`.
- Historical docs/evidence: prior release entries, release evidence packets, archived investigation packets, and repository history links that identify ClawManager as the historical project name.

M1 is also not authorized to introduce GTClaw, fork the runtime, change backend routes, change DB/table names, change Kubernetes object names, or directly edit deployment manifests for branding.

## 4. Architecture / Approach

Use the existing frontend i18n system as the source of truth. Change the manager product brand through i18n keys such as `app.name` and auth/layout/page strings, then route hardcoded product-facing UI text through the same translation mechanism. Do not introduce a second localization system or a second design system.

Default locale behavior should be minimal and backwards-compatible: keep `clawmanager_locale` as the storage key, preserve a valid stored locale when present, and use Chinese only when storage is empty or invalid. Separate the empty-profile default locale from the missing-key fallback locale: the default user locale should become Chinese, while missing translation keys should fall back through a stable fallback locale, recommended as English, instead of reusing the Chinese default for all fallback behavior.

Frontend branding work should cover browser metadata, login/register copy, admin/user shells, primary navigation, admin/user route titles, instance creation, desktop access, and product-facing hardcoded labels found by scan. OpenClaw runtime strings remain OpenClaw where they name runtime behavior.

Docs should separate product-facing copy from technical identifiers. `README.md`, `docs/k3s-local-setup.md`, `docs/manual-skill-import.md`, and `docs/manual-skill-import_en.md` may describe the product as GTManager while preserving commands, paths, manifests, API URLs, namespace names, images, DB names, and historical release/evidence language.

OpenClaw image-internal Chinese localization is documented only as a future gated path: define expected resource package / image customization inputs and evidence requirements, but do not modify runtime image references or build an image in M1.

## 5. File Ownership Map

| Area | Files | Planned role |
| --- | --- | --- |
| Current draft | `specs/gtmanager-m1-branding-localization/plan.md` | M1 plan draft and Phase A governance alignment record. |
| Strategy / rules gaps | `AGENTS.md`, `.specify/memory/constitution.md`, `backend/AGENTS.md`, `frontend/AGENTS.md`, `longterm/workspace/init.sh`, `longterm/workspace/app_spec.md`, `longterm/CHECKLIST.md` | Phase A governance alignment applied on 2026-04-29. These files align subproject rules and verification commands / DoD with the E2E final gate. |
| Existing untracked archives/build output | `clawmanagerArm/`, `dist/` | Explicitly excluded from M1 Phase A and business implementation. Treat as pre-existing local artifacts unless the user separately authorizes cleanup or inspection. |
| Frontend i18n / locale | `frontend/src/lib/i18n.ts`, `frontend/src/contexts/I18nContext.tsx` | Change default locale strategy, product-facing GTManager strings, Chinese defaults, and fallback behavior while retaining explicit locale choice. |
| Frontend product surfaces | `frontend/index.html`, `frontend/src/components/AdminLayout.tsx`, `frontend/src/components/UserLayout.tsx`, `frontend/src/pages/instances/CreateInstancePage.tsx`, `frontend/src/components/OpenClawDesktopOverlay.tsx`, `frontend/src/components/InstanceAccess.tsx`, `frontend/src/stores/authStore.ts`, `frontend/src/types/instance.ts` | Replace product-facing ClawManager text or hardcoded English via i18n; preserve OpenClaw runtime and technical terms. |
| Static assets | `frontend/public/*`, `frontend/index.html`, layout components | **BLOCKED** until user provides exact GTManager logo/favicon/loading asset paths. No guessed filenames. |
| Frontend commands | `frontend/package.json` | Read-only command source for `npm run lint` and `npm run build`; no package change expected. |
| Product-facing docs | `README.md`, `docs/k3s-local-setup.md`, `docs/manual-skill-import.md`, `docs/manual-skill-import_en.md` | Update outward product wording to GTManager where appropriate; preserve technical names and historical release/evidence references. |
| Deployment manifests | `deployments/k3s/clawmanager.yaml`, `deployments/k8s/clawmanager.yaml` | Protected/read-only for M1 branding. Use only for dry-run verification and technical identifier inventory. |
| Backend source | `backend/` | No source change expected for M1 branding/localization. Run `go test ./...` as prerequisite evidence. |
| Evidence packet | `specs/gtmanager-m1-branding-localization/evidence/` or approved equivalent | Future E2E evidence location after tasks approval. Must record environment, routes, screenshots/logs or human feedback, and unresolved blockers. |

## 6. Implementation Phases

### Phase A: Strategy docs / project rule gaps

Applied on 2026-04-29 without modifying frontend/backend business logic or deployment manifests:

1. Added missing `backend/AGENTS.md` and `frontend/AGENTS.md` with local build/test/style commands and project-specific constraints.
2. Updated root `AGENTS.md` and `.specify/memory/constitution.md` so frontend commands reflect the actual `package.json` scripts and durable `passes:true` write-back requires E2E evidence.
3. Updated `longterm/workspace/init.sh` so command references include frontend lint/build, backend test, K3S/K8S dry-run checks, and an E2E evidence reminder.
4. Updated `longterm/workspace/app_spec.md` and `longterm/CHECKLIST.md` DoD language so E2E is the final gate and prerequisite checks cannot alone mark a feature as `passes:true`.
5. Explicitly excluded pre-existing local `clawmanagerArm/` and `dist/` artifacts from M1 Phase A and implementation scope.
6. Did not update `feature_list.json` to `passes:true`.

### Phase B: Frontend i18n/default locale/brand text

1. Set the new empty-profile default locale to Chinese while retaining valid explicit choices stored under `clawmanager_locale`.
2. Split `DEFAULT_LOCALE` / user default semantics from `FALLBACK_LOCALE` / missing-key fallback semantics so incomplete non-Chinese resources do not accidentally fall back to Chinese unless that is explicitly intended.
3. Change manager product-facing brand strings to GTManager in i18n resources and browser metadata.
4. Route hardcoded product-facing text in admin/user shells and instance-related surfaces through i18n.
5. Keep runtime text as OpenClaw for runtime identity, `.openclaw` archive behavior, OpenClaw Resource Management, bootstrap/import/export/status, and agent/runtime status views.
6. Preserve supported locale options unless the user separately approves reducing supported locales.
7. Treat backend-originated user-visible error strings as a mapping boundary: known errors may map to frontend translations; technical/backend identifiers remain unchanged.

### Phase C: Static assets integration, blocked until user provides paths

1. **BLOCKED**: final logo asset path is not provided.
2. **BLOCKED**: final favicon asset path is not provided.
3. **BLOCKED**: final launch/loading asset path is not provided.
4. Once user paths exist, replace only the approved target assets and update alt labels to GTManager.
5. Verify no broken image requests and no guessed filenames.

### Phase D: Product-facing docs wording

1. Update `README.md` product-facing title, intro, badges/alt text, and product-flow wording to GTManager where it names the manager product.
2. Preserve historical News/release entries that mention ClawManager as historical release evidence.
3. Update `docs/k3s-local-setup.md` and manual skill import docs to describe GTManager as the product while preserving commands, paths, manifests, namespace/service/image names, API URLs, DB names, and OpenClaw runtime terminology.
4. Do not rewrite archived release/evidence docs.
5. Do not directly modify deployment manifests for wording unless a later approved plan explicitly includes deployment docs sync.

### Phase E: Verification and E2E evidence packet

1. Run prerequisite checks listed in the Verification Plan.
2. Deploy or use an existing local K3S/K8S environment at `https://localhost:30443`.
3. Record clean-profile `/login` behavior: Chinese default, GTManager product brand, browser title, favicon request status.
4. Record admin shell behavior: admin dashboard, users, instances, AI Gateway, security center, and settings surfaces.
5. Record regular-user shell behavior where credentials are available: workspace, instances, OpenClaw Resource Management, settings, instance creation, and desktop access surfaces.
6. Record `/healthz` and protected technical identifiers unchanged.
7. If assets remain blocked, evidence must explicitly state that logo/favicon/loading replacement is blocked by missing user paths and cannot be used to claim final M1 closure.

## 7. Worker Breakdown

This is a future implementation split only. No subagent was dispatched for Phase A; governance alignment was applied inline after user authorization. Any future worker prompt must be sent by the user through the star topology.

| Worker | Scope | Allowed modification files | Ordering |
| --- | --- | --- | --- |
| Worker A - Strategy docs | Resolve project rule gaps and verification command drift. | `AGENTS.md`, `.specify/memory/constitution.md`, `backend/AGENTS.md`, `frontend/AGENTS.md`, `longterm/workspace/init.sh`, `longterm/workspace/app_spec.md`, `longterm/CHECKLIST.md` | Already handled inline on 2026-04-29; future reviewer can inspect these files read-only. |
| Worker B - Frontend i18n/brand text | Chinese default locale, GTManager product text, i18n routing, hardcoded text cleanup. | `frontend/src/lib/i18n.ts`, `frontend/src/contexts/I18nContext.tsx`, `frontend/index.html`, `frontend/src/components/AdminLayout.tsx`, `frontend/src/components/UserLayout.tsx`, `frontend/src/pages/instances/CreateInstancePage.tsx`, `frontend/src/components/OpenClawDesktopOverlay.tsx`, `frontend/src/components/InstanceAccess.tsx`, `frontend/src/stores/authStore.ts`, `frontend/src/types/instance.ts` | After plan review, `tasks.md` approval, and explicit user implementation approval; can run in parallel with D. |
| Worker C - Assets | Integrate approved GTManager logo/favicon/loading assets. | **BLOCKED**. Later allowed files must be exact user-provided asset source paths plus approved targets under `frontend/public/`, `frontend/index.html`, `frontend/src/components/AdminLayout.tsx`, `frontend/src/components/UserLayout.tsx`. | Blocked until user provides paths; cannot block B/D text work, but blocks final M1 closure unless user re-scopes. |
| Worker D - Docs | Product-facing docs wording while preserving technical identifiers and history. | `README.md`, `docs/k3s-local-setup.md`, `docs/manual-skill-import.md`, `docs/manual-skill-import_en.md` | After plan review, `tasks.md` approval, and explicit user implementation approval; can run in parallel with B. |
| Worker E - Verification/E2E | Run checks and create evidence packet. | No source modifications. Future evidence file only under `specs/gtmanager-m1-branding-localization/evidence/` or another user-approved evidence path. | Must run after B/D and after C if asset paths are supplied. |

No worker may modify `deployments/k3s/clawmanager.yaml`, `deployments/k8s/clawmanager.yaml`, backend source, `.codex/auth.json`, `.codex/config.toml`, secrets, or `longterm/workspace/feature_list.json` unless a later user-approved plan explicitly changes scope.

## 8. Verification Plan

Prerequisite commands:

```bash
cd frontend && npm run lint
cd frontend && npm run build
cd backend && go test ./...
kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml
kubectl apply --dry-run=client -f deployments/k8s/clawmanager.yaml
curl -sk https://localhost:30443/healthz
```

Final gate:

- Playwright E2E or recorded human E2E feedback against `https://localhost:30443`.
- E2E must cover clean-profile locale default, login page, admin shell, user shell where available, OpenClaw runtime wording, browser title/favicon requests, `/healthz`, and unchanged protected identifiers.
- Build/lint/unit/backend/dry-run/curl checks are prerequisite evidence only.
- No artifact may be marked `passes:true`, completed, accepted, or passed without E2E evidence and user approval.

## 9. Risks / Blockers

1. **BLOCKED - assets**: final logo/favicon/loading asset paths are not provided. Do not guess paths or filenames.
2. **BLOCKED - OpenClaw image-internal localization**: M1 only defines the resource/image localization path and evidence requirements; image changes require later approval.
3. Phase A governance alignment was applied inline on 2026-04-29; it still needs normal review before later workers rely on it as their execution packet.
4. `feature_list.json` remains unchanged and must not be set to `passes:true` without E2E evidence.
5. Root and subproject rule surfaces now express E2E as the final gate, but this does not validate any product-facing M1 behavior.
6. `frontend/src/lib/i18n.ts` is large and multilingual; product-facing changes must avoid deleting supported locale keys or breaking fallback behavior.
7. Some product-facing strings are hardcoded outside i18n, including layout labels and instance-creation OpenClaw helper copy. Missing any visible route text would leave M1 E2E pending.
8. Backend error messages can surface in frontend flows. M1 should map known user-facing errors through frontend i18n while preserving technical identifiers.
9. Deployment manifests contain protected ClawManager technical names and should remain unchanged for M1 branding.
10. Existing untracked or modified files in the working tree must not be reverted or overwritten by M1 work.

## 10. Approval Gates before implementation

1. User reviews and approves this `plan.md` draft.
2. User confirms TaskDesigner may generate `tasks.md`; no business implementation starts before tasks are approved.
3. User provides exact asset paths for logo/favicon/loading work, or explicitly accepts Phase C remaining blocked.
4. User confirms E2E evidence route: automated Playwright or recorded human E2E feedback.
5. User confirms no `passes:true`, Close, complete, accepted, or passed language may be written until E2E evidence exists and the user approves the result.
