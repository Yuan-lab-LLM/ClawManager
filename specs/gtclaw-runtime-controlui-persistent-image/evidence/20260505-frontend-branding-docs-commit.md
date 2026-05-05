# Frontend Branding Docs Commit Gate

Date: 2026-05-05
Commander: Codex
Verdict: FRONTEND_BRANDING_DOCS_COMMIT_DONE

## Approval

User approved `APPROVE_FRONTEND_PLUS_DOCS`.

This gate is limited to the approved frontend branding scope, optional README/docs scope, and F-007 follow-up evidence. It does not authorize AGENTS, constitution, longterm, generated large directories, historical specs, cleanup, deletion, browser E2E, deployment, runtime/K8S/database/registry mutation, image build/tag/push/pull, Mem0 write, passes:true, or Close.

## Dependency Evidence

- F-007 core commit: `531c268154450f1102e39859bc809665e1a585b4`
- Approval packet: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-commit-approval-packet.md`
- Separate review: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-separate-review.md`

## Committed Scope

Frontend scope:

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

Docs scope:

- `README.md`
- `docs/k3s-local-setup.md`
- `docs/manual-skill-import.md`
- `docs/manual-skill-import_en.md`

Evidence scope:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-separate-review.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-commit-approval-packet.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-frontend-branding-docs-commit.md`

## Summary

- Preserves GTManager product branding in frontend shell, title, logo, and user-visible strings.
- Preserves GTClaw as the user-visible OpenClaw runtime branding.
- Keeps OpenClaw technical identifiers where the code path depends on the upstream/runtime contract.
- Adds frontend access-mode wiring for GTClaw control-ui entry points matching the already committed F-007 backend access contract.
- Includes README/docs wording updates under the explicit `APPROVE_FRONTEND_PLUS_DOCS` approval.

## Verification

- `npm run build` from `frontend/`: exit 0.
- `npm run lint` from `frontend/`: exit 1 with 126 problems, matching the accepted lint debt baseline recorded by the approval packet.
- `git diff --check -- <approved tracked paths>`: exit 0.
- No-index whitespace checks for approved new markdown evidence: no diagnostics.
- Secret-shape scan for new evidence files: clean, no secret, token, cookie, credential, or access URL plaintext output.
- Staged path allowlist check: passed before commit.
- Staged deletion check: passed before commit.
- Post-commit index check: empty.

## Explicit Exclusions

Excluded paths were not staged, committed, cleaned, deleted, reverted, or otherwise modified by this gate:

- `AGENTS.md`
- `backend/AGENTS.md`
- `frontend/AGENTS.md`
- `.specify/memory/constitution.md`
- `longterm/**`
- `clawmanagerArm/**`
- `dist/**`
- `specs/gtmanager-*`

## Explicit Negatives

- no browser E2E
- no backend deploy/restart
- no frontend deploy
- no runtime/K8S/database/registry mutation
- no image build/tag/push/pull
- no Docker run/create/cp/export/save
- no cleanup/delete/revert of excluded dirty paths
- no Mem0 write
- no longterm write
- no passes:true
- no Close

## Commit Hash

The commit hash is reported in the Commander final response because a file cannot record the final hash of the commit that contains itself without changing that hash.
