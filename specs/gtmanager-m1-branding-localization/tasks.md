# GTManager M1 Branding and Chinese Localization Tasks

**Status**: Draft tasks - user review and explicit implementation approval required.

This file breaks M1 into future work groups. Creating this file does not authorize business implementation, asset changes, evidence write-back, feature closeout, or any durable status update.

## Global Execution Rules

- [ ] Keep this task file as the only TaskDesigner write for this round.
- [ ] Do not read, modify, cite, or treat `clawmanagerArm/` or `dist/` as M1 artifacts.
- [ ] Do not modify `frontend/src/**`, `backend/internal/**`, `deployments/**`, `README.md`, `docs/**`, `longterm/**`, `frontend/public/**`, `.specify/memory/constitution.md`, `AGENTS.md`, `backend/AGENTS.md`, or `frontend/AGENTS.md` while only designing tasks.
- [ ] Do not write `passes:true` as a durable project status.
- [ ] Do not claim Close, acceptance, or final status without E2E evidence and explicit user approval.
- [ ] Use star topology for any future worker dispatch: workers report only to the user/commander, do not contact each other, and do not wait on other workers.

## Protected Terms and Identifiers

All future workers must preserve these unless a later approved plan explicitly says otherwise:

- [ ] Runtime wording: `OpenClaw`, `.openclaw`, `OpenClaw Desktop`, `OpenClaw Resource Management`, OpenClaw import/export/bootstrap/status/runtime wording.
- [ ] Kubernetes identifiers: `clawmanager-system`, `clawmanager-app`, `clawmanager-gateway`.
- [ ] API and access identifiers: `/api/v1`, `/healthz`, `https://localhost:30443`.
- [ ] Runtime control path: `http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001`.
- [ ] Database and table names, including `clawmanager`, `users`, `instances`, `audit_logs`, OpenClaw config tables, skill tables, and migration names.
- [ ] Go module: `clawreef`.
- [ ] Container and runtime image identifiers, including `ghcr.io/yuan-lab-LLM/clawmanager:latest` style manager images and `clawmanager-openclaw-image`.
- [ ] Locale storage key: `clawmanager_locale`, unless later explicitly approved otherwise.

## Worker Ownership Map

| Worker | May modify | Must not modify |
| --- | --- | --- |
| Task group 0 - Approval gate | `specs/gtmanager-m1-branding-localization/tasks.md` only | Any business, docs, frontend, backend, deployment, asset, or longterm file |
| Task group B - Frontend i18n/branding worker | `frontend/src/lib/i18n.ts`; `frontend/src/contexts/I18nContext.tsx`; `frontend/index.html`; `frontend/src/components/AdminLayout.tsx`; `frontend/src/components/UserLayout.tsx`; `frontend/src/pages/instances/CreateInstancePage.tsx`; `frontend/src/components/OpenClawDesktopOverlay.tsx`; `frontend/src/components/InstanceAccess.tsx`; `frontend/src/stores/authStore.ts`; `frontend/src/types/instance.ts` | Any additional frontend file unless first marked "requires approval before implementation"; `frontend/public/**`; backend; deployments; docs; longterm |
| Task group C - Assets worker | BLOCKED - no file ownership until exact user asset paths exist | Must not guess paths; must not modify `frontend/public/**`; must not modify logo/favicon/loading references before approval |
| Task group D - Docs worker | `README.md`; `docs/k3s-local-setup.md`; `docs/manual-skill-import.md`; `docs/manual-skill-import_en.md` | `docs/superpowers/**`; deployments; frontend; backend; longterm |
| Task group R - Review worker | No modifications | Source, docs, assets, deployments, longterm, or status files |
| Task group E - Evidence/E2E verifier | No source modifications; evidence output path requires separate user approval before writing | Source, docs, assets, deployments, longterm, or status files |
| Task group Close - Closeout/write-back | No write-back until E2E evidence plus explicit user approval | `longterm/workspace/feature_list.json`, `claude-progress.txt`, status summaries, or closeout docs before approval |

## Execution Order and Parallelism

- [ ] Task group 0 must run first.
- [ ] Task groups B and D may run in parallel only after tasks.md review approval and explicit user implementation approval.
- [ ] Task group C must remain BLOCKED until the user provides exact logo, favicon, and loading asset source paths.
- [ ] Task group R must run after implementation workers B/D, and after C only if C becomes unblocked.
- [ ] Task group E must run after implementation and review.
- [ ] Task group Close must run only after E2E evidence exists and the user explicitly approves closeout/write-back.

## Task Group 0: tasks.md Review / Approval Gate

- [ ] Confirm the user reviewed this `tasks.md`.
- [ ] Confirm the user explicitly approves starting business implementation.
- [ ] Confirm whether B and D should be dispatched in parallel.
- [ ] Confirm C remains blocked unless the user has provided exact asset source paths.
- [ ] Confirm the E2E evidence route: Playwright E2E or recorded human E2E feedback.
- [ ] Confirm no Close/write-back step is authorized before E2E evidence and a later user approval.

## Task Group B: Frontend i18n/Branding Worker

**Allowed files**

- `frontend/src/lib/i18n.ts`
- `frontend/src/contexts/I18nContext.tsx`
- `frontend/index.html`
- `frontend/src/components/AdminLayout.tsx`
- `frontend/src/components/UserLayout.tsx`
- `frontend/src/pages/instances/CreateInstancePage.tsx`
- `frontend/src/components/OpenClawDesktopOverlay.tsx`
- `frontend/src/components/InstanceAccess.tsx`
- `frontend/src/stores/authStore.ts`
- `frontend/src/types/instance.ts`

**Tasks**

- [ ] Inventory product-facing manager strings in the allowed files only.
- [ ] If an additional frontend file appears necessary, stop and record: `requires approval before implementation`; do not edit it.
- [ ] Update locale strategy so an empty browser profile defaults to Chinese.
- [ ] Preserve explicit stored locale choices under `clawmanager_locale`.
- [ ] Keep `clawmanager_locale` as the storage key.
- [ ] Separate user default locale from missing translation fallback.
- [ ] Use a stable missing translation fallback, recommended English, instead of reusing the Chinese user default for missing keys.
- [ ] Keep language switching for existing supported locales; do not remove English, Chinese, Japanese, Korean, or German without separate approval.
- [ ] Update manager product-facing brand strings to GTManager in allowed i18n resources and browser metadata.
- [ ] Update browser title and document language behavior as part of frontend branding/localization.
- [ ] Route hardcoded product-facing shell labels in `AdminLayout.tsx` and `UserLayout.tsx` through i18n.
- [ ] Update logo alt text to GTManager where text is product-facing, but do not change image paths while C is blocked.
- [ ] Route product-facing hardcoded labels in `CreateInstancePage.tsx` through i18n.
- [ ] Route product-facing hardcoded labels in `OpenClawDesktopOverlay.tsx` and `InstanceAccess.tsx` through i18n where they are visible UI text.
- [ ] Review `authStore.ts` error fallback strings and map known user-visible auth errors through frontend i18n if this can be done inside the allowed files.
- [ ] Review `types/instance.ts` display labels/descriptions and adjust product-facing wording only; preserve runtime type IDs and OpenClaw names.
- [ ] Preserve OpenClaw runtime semantics everywhere they refer to runtime behavior: `OpenClaw`, `.openclaw`, `OpenClaw Desktop`, `OpenClaw Resource Management`, import/export/bootstrap/status/runtime.
- [ ] Preserve protected technical identifiers listed in this tasks file.
- [ ] Do not modify `frontend/public/**`.
- [ ] Do not introduce a second i18n framework or design system.

**Frontend prerequisite checks for this worker**

- [ ] Run `cd frontend && npm run lint`.
- [ ] Run `cd frontend && npm run build`.
- [ ] Record command outputs for Review worker and Evidence/E2E verifier.

## Task Group C: Assets Worker - BLOCKED

**Blocked reason**

- [ ] Final logo source path is not provided.
- [ ] Final favicon source path is not provided.
- [ ] Final loading asset source path is not provided.

**Rules while blocked**

- [ ] Do not guess logo, favicon, or loading filenames.
- [ ] Do not modify `frontend/public/**`.
- [ ] Do not replace `/lobster_transparent.png`, `/openclaw.png`, favicon hrefs, or loading surfaces until exact user-provided source paths exist.
- [ ] Do not mark asset integration as available evidence while paths are missing.

**When unblocked by user-provided exact paths**

- [ ] Confirm exact source paths and exact target paths with the user.
- [ ] Confirm whether replacing favicon/logo/loading assets is included in the same implementation approval or needs a separate approval.
- [ ] Modify only the approved asset target files and approved references.
- [ ] Verify no broken image requests in E2E.

## Task Group D: Docs Worker

**Allowed files**

- `README.md`
- `docs/k3s-local-setup.md`
- `docs/manual-skill-import.md`
- `docs/manual-skill-import_en.md`

**Tasks**

- [ ] Update product-facing manager wording to GTManager where the text names the outward product.
- [ ] Preserve historical release/news/evidence wording that refers to ClawManager as a historical project/release name.
- [ ] Preserve all command examples, paths, manifest names, namespaces, service names, API URLs, DB names, and image names.
- [ ] Preserve OpenClaw runtime wording where it names runtime behavior or OpenClaw resources.
- [ ] Preserve `https://localhost:30443`, `/api/v1`, `/healthz`, and the runtime control path.
- [ ] Do not modify `docs/superpowers/**` historical evidence.
- [ ] Do not modify `deployments/**`.
- [ ] Do not rename repository, module, image, database, or Kubernetes identifiers in docs.

**Docs review scan**

- [ ] Search only the allowed docs files for product-facing `ClawManager`.
- [ ] Classify each remaining `ClawManager` occurrence as protected technical identifier, historical evidence, repository name, image/path/command, or unresolved product-facing wording.
- [ ] If any unresolved product-facing wording remains, record it for Review worker before Evidence/E2E.

## Task Group R: Review Worker

**Modification rule**

- [ ] Review worker is read-only and must not modify files.

**Scope review**

- [ ] Confirm only allowed files were modified by B/D, plus C only if C was explicitly unblocked.
- [ ] Confirm no modification under `frontend/public/**` while C remains blocked.
- [ ] Confirm no modification under `backend/internal/**`, `deployments/**`, `docs/superpowers/**`, or `longterm/workspace/feature_list.json`.
- [ ] Confirm `clawmanagerArm/` and `dist/` were not read, modified, or cited as M1 outputs.

**Content review**

- [ ] Confirm empty profile defaults to Chinese.
- [ ] Confirm explicit stored locale remains honored.
- [ ] Confirm missing translation fallback uses a stable fallback, recommended English.
- [ ] Confirm product-facing manager brand is GTManager on allowed frontend surfaces and docs.
- [ ] Confirm OpenClaw runtime wording remains OpenClaw for runtime behavior, `.openclaw`, OpenClaw Desktop, OpenClaw Resource Management, import/export/bootstrap/status/runtime.
- [ ] Confirm protected technical identifiers remain unchanged.
- [ ] Confirm no `passes:true` durable status write exists.
- [ ] Confirm prerequisite command outputs are available for Evidence/E2E verifier.

## Task Group E: Evidence / E2E Verifier

**Prerequisite evidence**

- [ ] `cd frontend && npm run lint`
- [ ] `cd frontend && npm run build`
- [ ] `cd backend && go test ./...`
- [ ] `kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml`
- [ ] `kubectl apply --dry-run=client -f deployments/k8s/clawmanager.yaml`
- [ ] `curl -sk https://localhost:30443/healthz`

These are prerequisite evidence only. They are not the final gate.

**Final gate**

- [ ] Playwright E2E against `https://localhost:30443`, or recorded human E2E feedback.

**Minimum E2E coverage**

- [ ] Clean profile `/login`: Chinese default, GTManager brand, browser title, and favicon request.
- [ ] Admin shell routes: dashboard, users, instances, AI Gateway, security center, and settings.
- [ ] User shell routes where credentials are available: workspace, instances, OpenClaw Resource Management, settings, and relevant instance access surfaces.
- [ ] OpenClaw runtime wording remains OpenClaw.
- [ ] `/healthz` responds on `https://localhost:30443/healthz`.
- [ ] Protected identifiers remain unchanged in UI/docs/API behavior: `clawmanager-system`, `clawmanager-app`, `clawmanager-gateway`, `/api/v1`, `/healthz`, `https://localhost:30443`, runtime control path, database/table names, Go module `clawreef`, image identifiers, and `clawmanager_locale`.
- [ ] Asset requests are either verified from user-approved assets or explicitly recorded as blocked because C is still blocked.

## Task Group Close: Gated Closeout / Write-back

- [ ] Confirm prerequisite evidence exists.
- [ ] Confirm Playwright E2E or recorded human E2E feedback exists.
- [ ] Confirm Review worker has no Critical, Important, or blocking scope findings.
- [ ] Ask the user for explicit closeout/write-back approval.
- [ ] Do not write any closeout/status file before that approval.
- [ ] Do not update `longterm/workspace/feature_list.json`.
- [ ] Do not write `passes:true`.
- [ ] If user later approves write-back after E2E evidence, use a separate prompt with an explicit allowed file list.

## Not Authorized Yet

- [ ] No business implementation before user approval.
- [ ] No assets before paths.
- [ ] No passes:true before E2E.
- [ ] No Close before user approval.
