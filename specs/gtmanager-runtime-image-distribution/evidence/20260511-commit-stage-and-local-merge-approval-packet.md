# Commit Stage and Local Merge Approval Packet

Date: 2026-05-11

Task type: COMMIT_STAGE_AND_LOCAL_MERGE_APPROVAL_PACKET

Current branch: dev

## Purpose

This packet requests approval for the next gate to execute stage plus commit.

Whether local merge to the main branch is allowed must be explicitly confirmed by the next gate. This packet requests approval only and does not perform stage, commit, merge, or push.

Approval phrase:

```text
APPROVE_COMMIT_STAGE_AND_LOCAL_MERGE_GATE
```

## Dependency Gate State

- COMMIT_AND_MERGE_READINESS_REVIEW_BLOCKED has been reconciled.
- COMMIT_AND_MERGE_READINESS_BLOCKER_RECONCILIATION_DONE is satisfied.
- Lint blocker has been classified as known lint debt / non-blocking with waiver needed.
- Newly introduced lint: none proven by changed-line classification.

## Lint Waiver Required

The next commit/merge gate must carry the lint waiver and must not claim lint clean.

- npm run lint currently fails with 126 problems / 107 errors / 19 warnings.
- This is classified as known lint debt / non-blocking with waiver needed.
- changed diff lines have no lint findings.
- The next gate may proceed only by explicitly carrying this waiver.

## Recommended Commit Grouping

Recommended order: 4 commits.

### 1. backend: harden AI gateway provider timeout

Files:

- backend/internal/aigateway/service.go
- backend/internal/aigateway/service_test.go

### 2. frontend: refresh GTManager branding theme and logo

Files:

- frontend/public/gtmanager-logo.png
- frontend/public/gtmanager-logo.svg
- frontend/src/components/BrandLogo.tsx
- frontend/src/components/AdminLayout.tsx
- frontend/src/components/UserLayout.tsx
- frontend/src/components/ConfirmDialog.tsx
- frontend/src/components/OpenClawConfigPlanSection.tsx
- frontend/src/index.css
- frontend/src/pages/** changed files

### 3. specs: record GTManager frontend and image distribution evidence

Files:

- specs/gtmanager-frontend-logo/
- specs/gtmanager-frontend-theme/
- specs/gtmanager-runtime-image-distribution/

### 4. specs: record GTClaw runtime logo rollback evidence

Files:

- specs/gtclaw-runtime-controlui-persistent-image/control-ui-runtime-artifact/20260507-official-openclaw-localization/MANIFEST.md
- specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md
- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260511-gtclaw-runtime-controlui-logo-replacement-*.md

## Next Gate Verification Commands

The next commit/merge gate should run:

```bash
git branch --show-current
git status --short
git diff --check
cd backend && go test ./internal/aigateway ./internal/handlers -count=1
cd frontend && npm run build
cd frontend && npm run lint || true
git status --short
```

## Next Gate Prohibitions

The next gate must preserve these prohibitions unless the user separately approves a narrower exception:

- no git push
- no deploy or rollout or Kubernetes mutation
- no image build, tag, or push
- no cleanup of instances, PVCs, sessions, images, or caches
- no database mutation
- no passes:true
- no Close
- no longterm write-back

## Approval Request

Request approval to allow the next gate to stage and commit the recommended groups above.

Local merge is not approved by this packet. The next gate must explicitly confirm whether local merge to the main branch is approved before taking that action.
