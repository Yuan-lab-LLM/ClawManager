# Longterm Housekeeping Commit Approval Packet

Date: 2026-05-06
Commander: Codex
Verdict: LONGTERM_HOUSEKEEPING_COMMIT_APPROVAL_PACKET_DONE

## Dependency Gates

- F-007 core commit is complete: `531c268154450f1102e39859bc809665e1a585b4`.
- Frontend + docs follow-up commit is complete: `c574c54a50cb13b13abb68f6108ee3d60c9c7739`.
- Governance rules commit is complete: `fa7cdf27af8f4fbf566a82cd403f3f92bcfc8fb5`.
- Remaining Dirty Scope Triage is complete:
  `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-remaining-dirty-scope-triage.md`
  with verdict `REMAINING_DIRTY_SCOPE_TRIAGE_DONE`.
- Current `git diff --cached --name-only` is empty.

## User Decision Requested

Please approve or reject the next serial gate:

`Longterm Housekeeping Commit Gate`

Approval would authorize only the next gate to evaluate, stage, and commit the
longterm housekeeping scope below. Rejection would leave the longterm dirty
scope uncommitted and require a revised instruction.

## Recommended Future Commit Scope

Recommended future commit scope is limited to:

- `longterm/CHECKLIST.md`
- `longterm/workspace/app_spec.md`
- `longterm/workspace/init.sh`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-longterm-housekeeping-commit-approval-packet.md`
- Future longterm housekeeping commit evidence created by the approved Longterm Housekeeping Commit Gate

No other paths should be included in that future longterm housekeeping commit.

## Longterm Housekeeping Change Categories

Observed longterm changes:

- `longterm/CHECKLIST.md` changes end-of-session language from completed features to E2E-evidenced features before updating `passes`.
- `longterm/workspace/app_spec.md` updates stable command references:
  - frontend install/check command sequence uses `npm ci`, `npm run lint`, and `npm run build`
  - deployment manifest validation includes K3S and K8S dry-run commands
  - final acceptance requires E2E evidence before `passes: true`
- `longterm/workspace/init.sh` adds quick command references for:
  - backend build/test/vet
  - frontend install/lint/build/dev
  - K3S/K8S dry-run
  - health check
  - final acceptance caveat that build/lint/unit/integration/dry-run/health checks are prerequisite evidence only

## Explicit Exclusions

The Longterm Housekeeping Commit Gate should exclude:

- `AGENTS.md`
- `backend/AGENTS.md`
- `frontend/AGENTS.md`
- `.specify/memory/constitution.md`
- `README.md`
- `docs/**`
- frontend source
- backend source
- `specs/gtmanager-*`
- `clawmanagerArm/**`
- `dist/**`
- `longterm/workspace/feature_list.json`
- `longterm/workspace/claude-progress.txt`

## Explicit Prohibitions For This Packet Gate

This packet gate did not and must not perform:

- no stage
- no commit
- no cleanup
- no delete
- no revert
- no build/deploy
- no runtime/K8S/database/registry/browser mutation
- no image build/tag/push/pull
- no Mem0 write
- no `passes:true`
- no Close

## Required Later Gate Order

1. If the user approves: Longterm Housekeeping Commit Gate.
2. Then, as separate user decisions:
   - Historical Specs Archive/Commit Approval Packet
   - Generated Artifact Cleanup Approval Packet

## Verification Plan

Required checks for this packet:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-longterm-housekeeping-commit-approval-packet.md`
- `rg -n "LONGTERM_HOUSEKEEPING_COMMIT_APPROVAL_PACKET_DONE|LONGTERM_HOUSEKEEPING_COMMIT_APPROVAL_PACKET_BLOCKED|Longterm Housekeeping Commit Gate|longterm/CHECKLIST.md|longterm/workspace/app_spec.md|longterm/workspace/init.sh|E2E-evidenced|passes:true|npm ci|npm run lint|npm run build|dry-run|health check|AGENTS|constitution|feature_list.json|claude-progress.txt|specs/gtmanager|clawmanagerArm|dist|no stage|no commit|no cleanup|no delete|no Mem0|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-longterm-housekeeping-commit-approval-packet.md`
- `git diff --cached --name-only`
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-longterm-housekeeping-commit-approval-packet.md`

## Packet Boundary

This approval packet only prepares the user decision for a later longterm
housekeeping commit gate. It does not modify the longterm files, source files,
historical specs, generated artifacts, registry, runtime, K8S resources,
database state, browser state, or Mem0.
