# Frontend Branding Docs Commit Approval Packet

Date: 2026-05-05
Worker: FrontendBrandingDocsCommitApprovalPacketWorker
Verdict: FRONTEND_BRANDING_DOCS_COMMIT_APPROVAL_PACKET_DONE

## Dependency Gate

- F-007 core commit is already complete and Commander-reviewed:
  - commit `531c268154450f1102e39859bc809665e1a585b4`
  - message `feat: persist GTClaw control UI runtime delivery`
- Frontend Branding and Docs Separate Review is already Commander-reviewed:
  - evidence `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-separate-review.md`
  - verdict `FRONTEND_BRANDING_DOCS_SEPARATE_REVIEW_DONE`
- This packet is only an approval request. It does not authorize no stage exceptions, no commit exceptions, browser E2E, build/deploy, K8S/runtime/database/registry mutation, image build/tag/push/pull, Mem0 write, delete, cleanup, or revert.

## Approval Request

Please approve or reject the next Frontend Branding Docs Commit Gate.

Recommended approval choices:

- `APPROVE_FRONTEND_ONLY`: commit only the default frontend and F-007 evidence scope below.
- `APPROVE_FRONTEND_PLUS_DOCS`: commit the default frontend scope plus the optional README/docs scope below.
- `REJECT_OR_BLOCK`: do not proceed to a commit gate; provide the blocking concern or revised scope.

No approval is implied by this packet. A later worker must receive explicit user approval before staging or committing.

## Recommended Default Commit Scope

Default scope for the next Frontend Branding Docs Commit Gate:

- `frontend/index.html`
- `frontend/public/gtmanager-logo.png`
- `frontend/src/components/AdminLayout.tsx`
- `frontend/src/components/InstanceAccess.tsx`
- `frontend/src/components/OpenClawDesktopOverlay.tsx`
- `frontend/src/components/UserLayout.tsx`
- `frontend/src/contexts/I18nContext.tsx`
- `frontend/src/hooks/useInstanceDesktopAccess.ts`
- `frontend/src/lib/i18n.ts`
- `frontend/src/pages/admin/InstanceManagementPage.tsx`
- `frontend/src/pages/admin/SystemSettingsPage.tsx`
- `frontend/src/pages/instances/CreateInstancePage.tsx`
- `frontend/src/pages/instances/InstanceDetailPage.tsx`
- `frontend/src/pages/instances/InstancePortalPage.tsx`
- `frontend/src/services/instanceService.ts`
- `frontend/src/stores/authStore.ts`
- `frontend/src/types/instance.ts`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-separate-review.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-commit-approval-packet.md`
- future Frontend Branding Docs Commit Gate evidence under `specs/gtclaw-runtime-controlui-persistent-image/evidence/`

Default scope intent:

- GTManager branding in the frontend shell, title, logo, and product-facing strings.
- GTClaw control-ui entry points for running OpenClaw instances.
- i18n and locale behavior, including Chinese default locale and English fallback.
- Frontend-only access-mode wiring that matches the already-committed F-007 backend access contract.

## Optional Docs Scope

Only include these files if the user explicitly approves `frontend + docs` or `APPROVE_FRONTEND_PLUS_DOCS`:

- `README.md`
- `docs/k3s-local-setup.md`
- `docs/manual-skill-import.md`
- `docs/manual-skill-import_en.md`

Docs scope intent:

- Product-facing README/docs wording from ClawManager to GTManager.
- No protected technical identifier renames unless the future gate verifies them as product-facing only.

If the user approves `APPROVE_FRONTEND_ONLY`, these README/docs files must remain unstaged and uncommitted.

## Explicitly Excluded Scope

The next Frontend Branding Docs Commit Gate must continue to exclude:

- `AGENTS.md`
- `backend/AGENTS.md`
- `frontend/AGENTS.md`
- `.specify/memory/constitution.md`
- `longterm/**`
- `clawmanagerArm/**`
- `dist/**`
- `specs/gtmanager-*`

Rationale:

- AGENTS and constitution changes are governance/rules changes, not frontend branding implementation.
- longterm changes are workspace memory/write-back housekeeping.
- clawmanagerArm and dist remain untracked generated or external directories and must not be traversed, deleted, cleaned, staged, or committed in this gate.
- specs/gtmanager-* are historical feature artifacts and must stay outside the current F-007 follow-up.

## Current Lint and Build Baseline

Commander-provided baseline for the current workstream:

- `npm run build` previously passed.
- `npm run lint` failed with 126 problems.
- The 126 problems are treated as existing lint debt unless the user explicitly approves lint cleanup.

This packet does not run `npm run build`, `npm run lint`, browser E2E, or any deployment check. It only records the baseline and approval requirements.

## Required Checks for Future Commit Gate

Before any future Frontend Branding Docs Commit Gate may stage or commit, it must run and report:

- `npm run build` from `frontend/`.
- Targeted `git status` and `git diff` checks for the approved frontend/default scope and, if approved, README/docs scope.
- Lint baseline accounting, using one of these two accepted approaches:
  - re-run `npm run lint` and compare/summarize whether the dirty frontend changes add new lint failures; or
  - explicitly carry the existing 126 problems baseline as accepted lint debt if Commander approves that accounting mode.
- Whitespace checks for every approved staged path.
- Secret-shape scan for new evidence files, with no secret, token, cookie, credential, or access URL plaintext output.
- Staged path allowlist check proving no AGENTS, constitution, longterm, clawmanagerArm, dist, or specs/gtmanager path is staged.
- No staged deletes unless explicitly authorized by a separate user approval, which is not present here.

## Non-Authorization Statement

This approval packet itself authorizes only writing this evidence file. It does not authorize:

- no stage bypass
- no commit bypass
- implementation edits
- delete, cleanup, or revert
- browser E2E
- build/deploy
- K8S/runtime/database/registry mutation
- image build/tag/push/pull
- Mem0 write
- secret, token, cookie, credential, or access URL plaintext output

## Recommendation

Proceed only after explicit user approval.

Recommended default: `APPROVE_FRONTEND_ONLY`.

Use `APPROVE_FRONTEND_PLUS_DOCS` only if the user wants README/docs GTManager wording included with the frontend branding commit.

Use `REJECT_OR_BLOCK` if the user wants lint cleanup, governance/rules changes, longterm housekeeping, historical specs, clawmanagerArm, or dist handled before the frontend branding commit.

## Verification Plan

Required checks for this packet:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-commit-approval-packet.md`
- `git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-commit-approval-packet.md`
- `rg -n "FRONTEND_BRANDING_DOCS_COMMIT_APPROVAL_PACKET_DONE|FRONTEND_BRANDING_DOCS_COMMIT_APPROVAL_PACKET_BLOCKED|Frontend Branding Docs Commit Gate|frontend|GTManager|GTClaw|i18n|README|docs|npm run build|npm run lint|126 problems|AGENTS|constitution|longterm|clawmanagerArm|dist|specs/gtmanager|no stage|no commit|no browser E2E|no Mem0" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-commit-approval-packet.md`
- secret-shape scan on this evidence file
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-commit-approval-packet.md`
