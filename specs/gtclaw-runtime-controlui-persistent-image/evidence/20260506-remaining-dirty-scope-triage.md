# Remaining Dirty Scope Triage

Date: 2026-05-06
Worker: RemainingDirtyScopeTriageWorker
Verdict: REMAINING_DIRTY_SCOPE_TRIAGE_DONE

## Gate Context

- F-007 core commit is complete: `531c268154450f1102e39859bc809665e1a585b4`.
- Frontend + docs follow-up commit is complete: `c574c54a50cb13b13abb68f6108ee3d60c9c7739`.
- Current `git diff --cached --name-only` is empty.
- This triage is read-only except this evidence file: no stage, no commit, no cleanup, no delete, no revert, no build/tag/push/pull, no runtime/K8S/database/registry/browser mutation, no Mem0 write, no `passes:true`, and no Close.

## Current Remaining Dirty Summary

Tracked dirty paths:

- `.specify/memory/constitution.md`
- `AGENTS.md`
- `longterm/CHECKLIST.md`
- `longterm/workspace/app_spec.md`
- `longterm/workspace/init.sh`

Untracked dirty paths:

- `backend/AGENTS.md`
- `frontend/AGENTS.md`
- `specs/gtmanager-gtclaw-m1-runtime-localization/`
- `specs/gtmanager-m1-branding-localization/`
- `clawmanagerArm/`
- `dist/`

`git diff --stat` currently reports only the five tracked governance/longterm files: 43 insertions and 13 deletions.

## 1. Governance / Rules Scope

Governance/rules scope:

- `AGENTS.md`
- `backend/AGENTS.md`
- `frontend/AGENTS.md`
- `.specify/memory/constitution.md`

Observed content category:

- Root workflow command updates: frontend install/check commands move from `npm install`/`npm test` to `npm ci`, `npm run lint`, and `npm run build`.
- Acceptance rule tightening: E2E evidence is required before `passes:true`, Close, complete, accepted, or passed claims.
- External Expert Escalation rules added to root `AGENTS.md`.
- Backend and frontend subproject `AGENTS.md` files define local commands, protected identifiers, localization boundaries, and prerequisite-vs-E2E evidence boundaries.
- Constitution version changes from `1.0.0` to `1.0.1` with E2E and frontend command updates.

Recommendation: these paths can be proposed for a later Governance Rules Commit Approval Packet. They should not be bundled with longterm housekeeping, historical specs, or generated/external artifacts.

## 2. Longterm Housekeeping Scope

Longterm housekeeping scope:

- `longterm/CHECKLIST.md`
- `longterm/workspace/app_spec.md`
- `longterm/workspace/init.sh`

Observed content category:

- Checklist wording changes from generic completed features to E2E-evidenced features.
- `app_spec.md` updates stable project command references, dry-run checks, and E2E acceptance criteria.
- `init.sh` adds quick command references for backend build/vet, frontend install/lint/build, K3S/K8S dry-runs, health check, and final acceptance caveats.

Recommendation: these paths can be proposed for a later Longterm Housekeeping Commit Approval Packet after governance/rules approval, because they are project memory and workflow-helper updates rather than source implementation.

## 3. Historical Specs Scope

Historical specs scope:

- `specs/gtmanager-gtclaw-m1-runtime-localization/`
- `specs/gtmanager-m1-branding-localization/`

Observed metadata:

- `specs/gtmanager-gtclaw-m1-runtime-localization/`: 12 files, including spec, plan, tasks, design amendment, and evidence markdown.
- `specs/gtmanager-m1-branding-localization/`: 210 files, including spec, plan, tasks, E2E markdown, scripts, logs, screenshots, and asset evidence.

Observed markdown summaries:

- `gtmanager-gtclaw-m1-runtime-localization` describes GTManager / GTClaw M1 runtime localization, control-ui proxy design, runtime patch/bundle evidence, and E2E rerun evidence.
- `gtmanager-m1-branding-localization` describes GTManager M1 branding and Chinese localization, frontend i18n/default locale/brand text, static assets, product-facing docs wording, and E2E evidence.

Recommendation: keep these excluded from governance and longterm commits. If the user wants them retained in git, route them through a Historical Specs Archive/Commit Approval Packet with a dedicated allowlist and size/log/screenshot policy.

## 4. Generated / External / Large Artifact Scope

Generated/external/large artifact scope:

- `clawmanagerArm/`
- `dist/`

Metadata-only observations:

- `clawmanagerArm/` is untracked, about 467M, 24 entries within maxdepth 3. Top-level metadata includes `.DS_Store`, nested `ClawManager-main`, and ARM tar archives.
- `dist/` is untracked, about 1.1G, 35 entries within maxdepth 3. Top-level metadata includes A1 core-chain package directories, tarballs, checksum files, and a freeze bundle.

Recommendation: continue excluding both paths from all commit gates unless the user explicitly approves a generated artifact archive or cleanup decision. Do not read large file contents, do not stage, do not commit, no cleanup, and no delete.

## 5. Paths That Can Be Suggested for Later Separate Commit

Suggested later separate commits:

- Governance Rules Commit Approval Packet:
  - `AGENTS.md`
  - `backend/AGENTS.md`
  - `frontend/AGENTS.md`
  - `.specify/memory/constitution.md`
- Longterm Housekeeping Commit Approval Packet:
  - `longterm/CHECKLIST.md`
  - `longterm/workspace/app_spec.md`
  - `longterm/workspace/init.sh`
- Historical Specs Archive/Commit Approval Packet:
  - `specs/gtmanager-gtclaw-m1-runtime-localization/`
  - `specs/gtmanager-m1-branding-localization/`

No generated/external large artifact path should be suggested for normal commit without a dedicated archive policy.

## 6. Paths That Should Continue To Be Excluded

Continue excluding from the immediate next commit:

- `longterm/**` if the next gate is governance/rules.
- `AGENTS.md`, `backend/AGENTS.md`, `frontend/AGENTS.md`, and `.specify/memory/constitution.md` if the next gate is longterm housekeeping.
- `specs/gtmanager-*` unless the next gate is explicitly Historical Specs Archive/Commit Approval Packet.
- `clawmanagerArm/**`
- `dist/**`

## 7. Cleanup/Delete Approval Requirement

Paths requiring explicit user approval before any cleanup/delete:

- `clawmanagerArm/**`
- `dist/**`
- historical `specs/gtmanager-*` if the user wants removal instead of archive

This worker did not perform cleanup/delete. Any cleanup/delete must be handled only through a Generated Artifact Cleanup Approval Packet or another explicit user-approved cleanup gate.

## 8. Recommended Next Gate

Recommended next gate: Governance Rules Commit Approval Packet.

Reasoning:

- Governance/rules scope is small, high-authority, and directly explains the E2E, command, and escalation rules that shaped the recent F-007 and frontend/docs gates.
- Longterm housekeeping depends conceptually on those governance rules and should follow as a separate packet.
- Historical specs are larger and include logs/screenshots/scripts, so they need a dedicated archive policy.
- Generated/external artifacts are large and should remain excluded until a Generated Artifact Cleanup Approval Packet is explicitly approved.

Not BLOCKED. No implementation, stage, commit, cleanup, delete, mutation, Mem0 write, `passes:true`, or Close is recommended by this triage.

## Verification Plan

Required checks:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-remaining-dirty-scope-triage.md`
- `rg -n "REMAINING_DIRTY_SCOPE_TRIAGE_DONE|REMAINING_DIRTY_SCOPE_TRIAGE_BLOCKED|governance|longterm|historical specs|generated|external|clawmanagerArm|dist|AGENTS|constitution|no stage|no commit|no cleanup|no delete|no Mem0" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-remaining-dirty-scope-triage.md`
- `git diff --cached --name-only`
