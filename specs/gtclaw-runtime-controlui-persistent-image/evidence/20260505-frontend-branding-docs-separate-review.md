# Frontend Branding and Docs Separate Review

Date: 2026-05-05
Worker: FrontendBrandingDocsSeparateReviewWorker
Verdict: FRONTEND_BRANDING_DOCS_SEPARATE_REVIEW_DONE

## Gate Context

- F-007 core commit is already separate: `531c268154450f1102e39859bc809665e1a585b4`, `feat: persist GTClaw control UI runtime delivery`.
- This review is read-only for the remaining dirty frontend, README, docs, AGENTS, constitution, longterm, historical specs, clawmanagerArm, and dist scopes.
- Constraints honored during review: no stage, no commit, no delete, no cleanup, no revert, no browser E2E, no K8S/runtime/database/registry mutation, no image build/tag/push/pull, no Mem0 write, and no secrets, tokens, cookies, or access URL plaintext output.

## Inputs Reviewed

- `git status --short`
- `git show --name-only --format=short 531c268154450f1102e39859bc809665e1a585b4`
- `git diff --name-status` and `git diff --stat` for allowed frontend, README, docs, AGENTS, constitution, and longterm scopes
- Targeted `git diff --unified=0` for frontend branding, GTClaw control-ui entry, i18n, docs, rules, and longterm changes
- Top-level status only for `clawmanagerArm/` and `dist/`; their contents were not traversed

## 1. Frontend Changes Recommended for a Later Independent Gate

Recommendation: keep these frontend changes as the candidate scope for a later Frontend Branding Docs Commit Approval Packet, subject to that packet's own verification and pathspec approval.

Frontend GTManager branding and i18n surfaces:

- `frontend/index.html`: default document language, favicon path, and title shift to GTManager.
- `frontend/public/gtmanager-logo.png`: static logo asset used by the frontend shell.
- `frontend/src/components/AdminLayout.tsx` and `frontend/src/components/UserLayout.tsx`: logo path and alt text route through i18n; shell labels move to translation keys.
- `frontend/src/contexts/I18nContext.tsx`: validates stored locale, sets document title from i18n, and uses English fallback instead of default-locale fallback.
- `frontend/src/lib/i18n.ts`: default locale changes to Chinese, fallback locale stays English, GTManager wording replaces product-facing ClawManager text, GTClaw runtime labels are added, and missing instance/control-ui/user-layout strings are filled across locales.
- `frontend/src/stores/authStore.ts`: auth fallback messages use localized text.

Frontend GTClaw control-ui entry and access contract surfaces:

- `frontend/src/services/instanceService.ts`: adds `InstanceAccessMode`, access response shape, access mode query parameter, and GTClaw control-ui chat URL derivation.
- `frontend/src/hooks/useInstanceDesktopAccess.ts`: separates desktop and control-ui sessions by access mode and removes `any` error handling.
- `frontend/src/components/InstanceAccess.tsx`: adds optional GTClaw control-ui button, control-ui opening/error state, and localized desktop frame title.
- `frontend/src/pages/instances/InstancePortalPage.tsx`: adds GTClaw control-ui open button for running OpenClaw instances.
- `frontend/src/pages/instances/InstanceDetailPage.tsx`: enables the GTClaw control-ui entry for OpenClaw instances.

Frontend localization cleanup surfaces:

- `frontend/src/types/instance.ts`: adds translation keys for instance types and presets while retaining technical type identifiers.
- `frontend/src/pages/instances/CreateInstancePage.tsx`: routes instance type, preset, skill injection, config preview, GPU, and OpenClaw bootstrap UI strings through i18n.
- `frontend/src/pages/admin/InstanceManagementPage.tsx`: displays localized instance type labels.
- `frontend/src/pages/admin/SystemSettingsPage.tsx`: localizes image type labels and tightens API error handling.
- `frontend/src/components/OpenClawDesktopOverlay.tsx`: routes overlay controls and gateway-controls aria label through i18n while preserving OpenClaw gateway terminology.

Review note: these changes look coherent as a frontend GTManager/GTClaw branding, i18n, and control-ui-entry set. They should not be bundled with historical specs, `clawmanagerArm/`, `dist/`, AGENTS/constitution rule changes, or unrelated longterm housekeeping.

## 2. README, Docs, AGENTS, Constitution, and Longterm Housekeeping

Recommendation: handle these through a separate docs/rules/housekeeping approval, or defer them until after the frontend gate.

Product-facing README/docs wording:

- `README.md`
- `docs/k3s-local-setup.md`
- `docs/manual-skill-import.md`
- `docs/manual-skill-import_en.md`

These are mostly ClawManager-to-GTManager product-facing wording updates. They may be appropriate for a docs portion of a later Frontend Branding Docs Commit Approval Packet, but only if that packet explicitly includes README and docs pathspecs and verifies protected technical identifiers.

Rules and project-governance housekeeping:

- `AGENTS.md`
- `backend/AGENTS.md`
- `frontend/AGENTS.md`
- `.specify/memory/constitution.md`

These update command expectations, E2E acceptance rules, escalation rules, and subproject conventions. They are valuable but higher-authority process changes; they should be archived through a separate rules/governance gate rather than hidden inside a frontend branding commit.

Longterm housekeeping:

- `longterm/CHECKLIST.md`
- `longterm/workspace/app_spec.md`
- `longterm/workspace/init.sh`

These update checklist language, stable project facts, command references, dry-run commands, and E2E acceptance notes. They should be deferred or archived through a separate longterm/write-back gate because they are not frontend implementation files.

## 3. Historical specs/gtmanager-* Recommendation

Recommendation: keep all historical `specs/gtmanager-*` paths excluded from the current feature's follow-up commits.

Observed untracked historical spec scopes:

- `specs/gtmanager-gtclaw-m1-runtime-localization/`: 12 untracked files, including spec/plan/tasks, design amendment, and evidence from prior runtime-localization work.
- `specs/gtmanager-m1-branding-localization/`: 124 untracked files, including spec/plan/tasks, E2E markdown, scripts, logs, and screenshots from prior branding/localization work.

Rationale: these are historical feature artifacts, not the F-007 `gtclaw-runtime-controlui-persistent-image` delivery artifacts. They should not be folded into the current feature's later frontend/docs commit unless Commander issues a dedicated historical-spec archive approval.

## 4. clawmanagerArm and dist Recommendation

Recommendation: keep `clawmanagerArm/**` and `dist/**` excluded. Do not delete them and do not commit them in the frontend/docs follow-up.

Observed status:

- `clawmanagerArm/` remains untracked.
- `dist/` remains untracked.

Review only confirmed top-level status. Contents were not traversed or classified.

## 5. Frontend Lint Debt

Current frontend lint debt does not block this separate-review evidence because this gate is review-only and did not authorize lint fixes, build, browser E2E, or implementation.

For a future frontend commit gate, the known lint baseline must be handled explicitly. The Commander-provided baseline says `npm run lint` failed with 126 problems and should be treated as existing lint debt for the F-007 core gate. A future Frontend Branding Docs Commit Approval Packet should either:

- re-run lint and document whether the dirty frontend changes add new lint failures, or
- explicitly carry the 126-problem baseline as accepted debt while requiring `npm run build` and targeted checks.

No lint fixes were made in this review.

## 6. Recommended Next Gate

Recommended next gate: Frontend Branding Docs Commit Approval Packet.

Scope recommendation for that packet:

- Primary include: `frontend/**`, especially GTManager branding, GTClaw control-ui entry, and i18n files listed above.
- Optional include only with explicit approval: `README.md` and `docs/**` product-facing GTManager wording.
- Exclude: `AGENTS.md`, `backend/AGENTS.md`, `frontend/AGENTS.md`, `.specify/memory/constitution.md`, `longterm/**`, historical `specs/gtmanager-*`, `clawmanagerArm/**`, and `dist/**`.

Secondary later gate, if Commander wants worktree hygiene: Frontend Scope Cleanup Approval Packet. That later packet should decide what to do with historical specs, `clawmanagerArm/`, `dist/`, and governance/longterm artifacts without deleting, reverting, or staging anything unless explicitly authorized.

External Expert Escalation is not required from this review. BLOCKED is not recommended.

## Verification Plan

Required checks for this evidence:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-separate-review.md`
- `git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-separate-review.md`
- `rg -n "FRONTEND_BRANDING_DOCS_SEPARATE_REVIEW_DONE|FRONTEND_BRANDING_DOCS_SEPARATE_REVIEW_BLOCKED|frontend|branding|GTManager|GTClaw|i18n|README|docs|AGENTS|constitution|longterm|clawmanagerArm|dist|no stage|no commit|no delete|no browser E2E|no Mem0" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-separate-review.md`
- secret-shape scan on this evidence file
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-separate-review.md`
