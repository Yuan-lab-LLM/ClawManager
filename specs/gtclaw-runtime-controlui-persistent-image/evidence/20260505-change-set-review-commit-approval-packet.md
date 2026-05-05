# Change Set Review / Commit Approval Packet

Date: 2026-05-05

Worker: ChangeSetReviewCommitApprovalPacketWorker

Topology: serial

## Verdict

CHANGE_SET_REVIEW_COMMIT_APPROVAL_PACKET_DONE

This packet requests user approval or rejection for a future Change Set Commit/Package Gate. It does not stage, commit, delete, clean up, revert, package, or mutate the environment.

## Dependency Gates

| Gate | Evidence / baseline | Result used |
| --- | --- | --- |
| F-007 final close/write-back | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-final-close-writeback.md` | `FINAL_CLOSE_WRITEBACK_DONE`; feature `gtclaw-runtime-controlui-persistent-image`; `feature_list.json` appended `F-007` with `passes: true`. |
| Browser E2E | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-browser-e2e.md` | `BROWSER_E2E_DONE`; instance `10` / `oc2gi-185707`; `/control-ui` HTTP `200`, non-502; WebSocket `101` plus frame traffic; `/proxy` desktop regression pass; no token exposure. |
| Commander current read-only audit baseline | Commander supplied baseline | `go test -count=1 ./...` passed; `npm run build` passed; `npm run lint` failed with `126 problems` and is treated as existing lint debt unless separately approved; `git diff --check` passed. |

## Recommended Commit / Package Layers

### Layer 1 - F-007 core backend/runtime/evidence/longterm scope

Recommended first future package, if the user approves Commit/Package Gate:

- backend control-ui route, access-scope, WebSocket bridge, Service `18789`, hostname, and tests;
- current feature spec/plan/tasks/evidence/runtime startup artifact under `specs/gtclaw-runtime-controlui-persistent-image/**`;
- F-007 longterm close/write-back state only in `longterm/workspace/feature_list.json` and `longterm/workspace/claude-progress.txt`.

This layer is the only layer recommended as the F-007 core commit/package.

### Layer 2 - frontend/branding/docs separate-review scope

Recommended separate review/package only if the user explicitly approves a separate frontend/branding/docs scope:

- frontend route/client/branding/localization changes;
- `frontend/public/gtmanager-logo.png`;
- root and subproject `AGENTS.md` files;
- README and docs changes;
- broader longterm housekeeping files outside the F-007 final close write-back pair.

Rationale: these files are present in the current worktree, but they are not required to preserve the already reviewed F-007 backend/runtime/evidence/longterm close boundary.

### Layer 3 - large generated/untracked exclude scope

Recommended exclude from any F-007 commit/package:

- `clawmanagerArm/**`
- `dist/**`
- unrelated historical `specs/gtmanager-*`
- unrelated frontend/docs/branding files unless user approves a separate scope

`clawmanagerArm/**` currently has a large untracked import tree. `dist/**` currently contains generated/package artifacts. These should not be staged, committed, deleted, or cleaned up by the F-007 core gate.

## Candidate Include List For F-007 Core

If the user approves a future Commit/Package Gate for F-007 core only, the candidate include pathspec should be limited to:

### Backend source and tests

- `backend/cmd/server/main.go`
- `backend/internal/handlers/instance_handler.go`
- `backend/internal/handlers/instance_access_contract_test.go`
- `backend/internal/services/instance_access_service.go`
- `backend/internal/services/instance_access_service_test.go`
- `backend/internal/services/instance_proxy_service.go`
- `backend/internal/services/instance_proxy_service_test.go`
- `backend/internal/services/instance_service.go`
- `backend/internal/services/instance_service_test.go`
- `backend/internal/services/k8s/client.go`
- `backend/internal/services/k8s/pod_service.go`
- `backend/internal/services/k8s/pod_service_test.go`
- `backend/internal/services/k8s/service_service.go`
- `backend/internal/services/k8s/service_service_test.go`
- `backend/internal/services/security_scan_service.go`

### Current feature artifacts

- `specs/gtclaw-runtime-controlui-persistent-image/spec.md`
- `specs/gtclaw-runtime-controlui-persistent-image/plan.md`
- `specs/gtclaw-runtime-controlui-persistent-image/tasks.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/**`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/**`

### F-007 longterm write-back

- `longterm/workspace/feature_list.json`
- `longterm/workspace/claude-progress.txt`

## Candidate Exclude / Defer List

Do not include in F-007 core unless the user explicitly approves a separate scope:

### Large generated/untracked

- `clawmanagerArm/**`
- `dist/**`

### Historical or unrelated specs

- `specs/gtmanager-gtclaw-m1-runtime-localization/**`
- `specs/gtmanager-m1-branding-localization/**`

### Frontend/branding/docs separate-review

- `frontend/index.html`
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
- `frontend/AGENTS.md`
- `frontend/public/gtmanager-logo.png`
- `AGENTS.md`
- `backend/AGENTS.md`
- `.specify/memory/constitution.md`
- `README.md`
- `docs/k3s-local-setup.md`
- `docs/manual-skill-import.md`
- `docs/manual-skill-import_en.md`
- `longterm/CHECKLIST.md`
- `longterm/workspace/app_spec.md`
- `longterm/workspace/init.sh`

## Current Worktree Summary

Read-only status/diff review observed:

- Modified tracked backend source/tests, frontend files, docs/root rule files, and longterm files.
- Untracked backend tests and `backend/AGENTS.md`.
- Untracked current feature tree `specs/gtclaw-runtime-controlui-persistent-image/**`.
- Untracked historical specs under `specs/gtmanager-*`.
- Untracked `frontend/AGENTS.md` and `frontend/public/gtmanager-logo.png`.
- Untracked large generated/package directories `clawmanagerArm/**` and `dist/**`.

This packet did not normalize, clean, stage, commit, or delete any of those paths.

## Current Verification Baseline

The current Commander read-only audit baseline for the change set is:

| Check | Result |
| --- | --- |
| `go test -count=1 ./...` | passed |
| `npm run build` | passed |
| `npm run lint` | failed with `126 problems`; treat as existing lint debt unless separately approved |
| `git diff --check` | passed |

This packet did not rerun backend tests, frontend build, frontend lint, or browser E2E. It only records the Commander baseline and verifies this approval packet file.

## Future Approval Request

Please approve or reject whether to execute a future Commit/Package Gate.

If approved, that future gate must specify which layer is authorized:

1. F-007 core backend/runtime/evidence/longterm scope only.
2. frontend/branding/docs separate-review scope.
3. another explicit package shape defined by the user.

Only after user approval may the future gate stage, commit, or package the specified scope. No stage, commit, delete cleanup, revert, or package action is authorized by this packet.

## Explicit Forbidden Actions

This packet and any unapproved future step forbid:

- no stage
- no commit
- no delete
- no delete cleanup
- no revert
- no build/tag/push/pull image
- no K8S mutation
- no runtime mutation
- no database mutation
- no registry mutation
- no browser mutation
- no Mem0 write
- no secrets, token, cookie, credential, or access URL plaintext output

## Verification Commands

```sh
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-change-set-review-commit-approval-packet.md
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-change-set-review-commit-approval-packet.md
rg -n "CHANGE_SET_REVIEW_COMMIT_APPROVAL_PACKET_DONE|CHANGE_SET_REVIEW_COMMIT_APPROVAL_PACKET_BLOCKED|gtclaw-runtime-controlui-persistent-image|F-007|go test|npm run build|npm run lint|126 problems|clawmanagerArm|dist|no stage|no commit|no delete|no revert|no K8S|no browser mutation|no Mem0" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-change-set-review-commit-approval-packet.md
secret-shape scan on this new evidence
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-change-set-review-commit-approval-packet.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-change-set-review-commit-approval-packet.md` | `0` | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-change-set-review-commit-approval-packet.md` | `1` | No output and no whitespace diagnostics; exit `1` is the expected no-index difference status for `/dev/null` vs this new file. |
| required marker `rg` scan | `0` | Required markers found, including `CHANGE_SET_REVIEW_COMMIT_APPROVAL_PACKET_DONE`, `gtclaw-runtime-controlui-persistent-image`, `F-007`, `go test`, `npm run build`, `npm run lint`, `126 problems`, `clawmanagerArm`, `dist`, `no stage`, `no commit`, `no delete`, `no revert`, `no K8S`, `no browser mutation`, and `no Mem0`. |
| secret-shape scan on this new evidence | `1` | No matches. |
| path-limited `git status --short` | `0` | Shows this new approval packet as untracked. |
