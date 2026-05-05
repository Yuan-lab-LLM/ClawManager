# Governance Rules Commit Approval Packet

Date: 2026-05-06
Worker: GovernanceRulesCommitApprovalPacketWorker
Verdict: GOVERNANCE_RULES_COMMIT_APPROVAL_PACKET_DONE

## Gate Context

- F-007 core commit is complete: `531c268154450f1102e39859bc809665e1a585b4`.
- Frontend + docs follow-up commit is complete: `c574c54a50cb13b13abb68f6108ee3d60c9c7739`.
- Remaining Dirty Scope Triage is complete:
  `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-remaining-dirty-scope-triage.md`
  with verdict `REMAINING_DIRTY_SCOPE_TRIAGE_DONE`.
- Current git index was checked before writing this packet and was empty.
- This packet gate is documentation-only evidence. It authorizes no stage, no commit, no cleanup, no delete, no revert, no build, no deploy, no runtime/K8S/database/registry/browser mutation, no Mem0 write, no `passes:true`, and no Close.

## User Decision Requested

Please approve or reject whether the next serial gate should execute the
Governance Rules Commit Gate.

Approval would authorize only the next gate to evaluate, stage, and commit the
recommended governance/rules scope below. Rejection would leave the governance
dirty scope uncommitted and require a revised instruction.

## Recommended Future Commit Scope

Recommended future commit scope is limited to:

- `AGENTS.md`
- `backend/AGENTS.md`
- `frontend/AGENTS.md`
- `.specify/memory/constitution.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-governance-rules-commit-approval-packet.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-remaining-dirty-scope-triage.md`
- Future governance commit evidence created by the approved Governance Rules Commit Gate

No other paths should be included in that future governance commit.

## Governance / Rules Change Categories

The observed governance/rules changes fall into these categories:

- E2E evidence before `passes:true`, Close, accepted, complete, passed, or final acceptance claims.
- Frontend command updates from stale commands to `npm ci`, `npm run lint`, and `npm run build`.
- External expert escalation rules for architecture/design uncertainty and stubborn bugs.
- Backend/frontend subproject local conventions in `backend/AGENTS.md` and `frontend/AGENTS.md`.
- Protected identifiers and localization boundaries, including preserving technical OpenClaw, Kubernetes, API, storage, database, and runtime identifiers unless an approved plan changes them.

## Explicit Exclusions

The Governance Rules Commit Gate should exclude:

- `longterm/**`
- `specs/gtmanager-*`
- `clawmanagerArm/**`
- `dist/**`
- `README.md`
- `docs/**`
- Frontend source beyond `frontend/AGENTS.md`
- Backend source beyond `backend/AGENTS.md`

## Explicit Prohibitions For This Packet Gate

This packet gate did not and must not perform:

- no stage
- no commit
- no cleanup
- no delete
- no revert
- no build
- no deploy
- no runtime/K8S/database/registry/browser mutation
- no Mem0 write
- no `passes:true`
- no Close

## Required Later Gate Order

1. If the user approves: Governance Rules Commit Gate.
2. Then: Longterm Housekeeping Commit Approval Packet.
3. Then, as separate user decisions: Historical Specs Archive/Commit Approval Packet or Generated Artifact Cleanup Approval Packet.

## Packet Boundary

This approval packet only prepares the user decision for a later governance
commit gate. It does not modify `AGENTS.md`, `backend/AGENTS.md`,
`frontend/AGENTS.md`, `.specify/memory/constitution.md`, `longterm/**`,
`README.md`, `docs/**`, frontend source, backend source, `specs/gtmanager-*`,
`clawmanagerArm/**`, or `dist/**`.
