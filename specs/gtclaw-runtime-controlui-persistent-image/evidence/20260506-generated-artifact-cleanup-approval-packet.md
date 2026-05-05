# Generated Artifact Cleanup Approval Packet

Date: 2026-05-06
Commander: Codex
Verdict: GENERATED_ARTIFACT_CLEANUP_APPROVAL_PACKET_DONE

## Dependency Gates

- F-007 core commit is complete: `531c268154450f1102e39859bc809665e1a585b4`.
- Frontend + docs follow-up commit is complete: `c574c54a50cb13b13abb68f6108ee3d60c9c7739`.
- Governance rules commit is complete: `fa7cdf27af8f4fbf566a82cd403f3f92bcfc8fb5`.
- Longterm housekeeping commit is complete: `92236e220356ffa60faa2a2ce2f94b3ac542a3d2`.
- Historical specs archive commit is complete: `5766470ac176ecbef78bb99c6b19c38fe98eefba`.
- Current remaining dirty scope is limited to untracked generated/external artifacts:
  - `clawmanagerArm/`
  - `dist/`
- Current `git diff --cached --name-only` is empty.

## User Decision Requested

Please approve or reject the next serial gate:

`Generated Artifact Cleanup Gate`

This packet does not authorize cleanup by itself. The next gate must receive a
clear approval option before deleting, moving, archiving, staging, or committing
anything.

## Read-Only Metadata

Observed metadata from read-only inspection:

- `clawmanagerArm/`
  - approximate size: 467M
  - file count: 290
  - top-level contents include `.DS_Store`, nested `ClawManager-main`, and ARM tar archives
  - largest files include `mysql-arm64.tar` at about 218M and `skill-arm64.tar` at about 217M
- `dist/`
  - approximate size: 1.1G
  - file count: 50
  - top-level contents include A1 core-chain package directories, `.tar.gz` package archives, checksum files, and a freeze bundle
  - largest files include two full workspace tarballs at about 528M and 527M

Combined untracked generated/external artifact size is about 1.6G.

## Approval Options

Recommended option if the user wants a clean working tree:

- `APPROVE_GENERATED_ARTIFACT_DELETE`
  - Future gate may delete only:
    - `clawmanagerArm/`
    - `dist/`
  - Future gate must use an exact path command equivalent to:
    - `rm -rf -- clawmanagerArm dist`
  - Future gate must not stage or commit deletions because these paths are untracked.
  - Future gate must verify `git status --short` after deletion.

Alternative option if the user wants to keep the local artifacts:

- `APPROVE_KEEP_GENERATED_ARTIFACTS_UNTRACKED`
  - Future gate does not delete, move, archive, stage, or commit the artifacts.
  - Working tree remains dirty by design.

Alternative option if the user wants to preserve a copy outside the repository:

- `APPROVE_MOVE_GENERATED_ARTIFACTS_TO_DESKTOP_ARCHIVE`
  - Future gate may move only `clawmanagerArm/` and `dist/` to a newly created timestamped directory under the Desktop.
  - Future gate must not compress, modify, or inspect contents beyond metadata.
  - Future gate must verify the source paths no longer appear in repo `git status --short`.

Reject/defer option:

- `REJECT_OR_DEFER_GENERATED_ARTIFACT_CLEANUP`
  - Future gate performs no cleanup.

## Recommended Future Gate Scope

If the user chooses `APPROVE_GENERATED_ARTIFACT_DELETE`, the future cleanup gate
scope is limited to:

- deleting untracked `clawmanagerArm/`
- deleting untracked `dist/`
- writing generated artifact cleanup evidence under:
  `specs/gtclaw-runtime-controlui-persistent-image/evidence/`

No other paths should be touched.

## Explicit Exclusions

The future Generated Artifact Cleanup Gate must exclude:

- tracked source files
- `specs/gtclaw-runtime-controlui-persistent-image/**` except the new cleanup evidence
- `specs/gtmanager-*`
- `longterm/**`
- `AGENTS.md`
- `backend/AGENTS.md`
- `frontend/AGENTS.md`
- `.specify/memory/constitution.md`
- `README.md`
- `docs/**`
- frontend source
- backend source
- deployments
- runtime image, registry, K8S, database, browser, or fresh instance resources

## Explicit Prohibitions For This Packet Gate

This packet gate did not and must not perform:

- no cleanup
- no delete
- no move
- no archive
- no stage
- no commit
- no revert
- no build/deploy
- no runtime/K8S/database/registry/browser mutation
- no image build/tag/push/pull
- no Mem0 write
- no `passes:true`
- no Close

## Required Future Gate Checks

Before and after any future cleanup gate, it must run and report:

- `git status --short`
- `git diff --cached --name-only`
- `du -sh clawmanagerArm dist` before cleanup when paths exist
- exact cleanup command used
- post-cleanup path existence checks
- post-cleanup `git status --short`
- cleanup evidence marker scan
- cleanup evidence whitespace check
- cleanup evidence secret-shape scan

## Verification Plan

Required checks for this packet:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-generated-artifact-cleanup-approval-packet.md`
- `rg -n "GENERATED_ARTIFACT_CLEANUP_APPROVAL_PACKET_DONE|GENERATED_ARTIFACT_CLEANUP_APPROVAL_PACKET_BLOCKED|Generated Artifact Cleanup Gate|APPROVE_GENERATED_ARTIFACT_DELETE|APPROVE_KEEP_GENERATED_ARTIFACTS_UNTRACKED|APPROVE_MOVE_GENERATED_ARTIFACTS_TO_DESKTOP_ARCHIVE|clawmanagerArm|dist|1.6G|rm -rf -- clawmanagerArm dist|no cleanup|no delete|no move|no archive|no stage|no commit|no Mem0|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-generated-artifact-cleanup-approval-packet.md`
- no-index whitespace check on this approval packet
- secret-shape scan on this approval packet
- `git diff --cached --name-only`
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-generated-artifact-cleanup-approval-packet.md clawmanagerArm dist`

## Packet Boundary

This approval packet only prepares the user decision for a later generated
artifact cleanup gate. It does not cleanup, delete, move, archive, stage,
commit, mutate runtime/K8S/database/registry/browser state, write Mem0, write
`passes:true`, or Close.
