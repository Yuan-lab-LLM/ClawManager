# Historical Specs Archive Commit Approval Packet

Date: 2026-05-06
Commander: Codex
Verdict: HISTORICAL_SPECS_ARCHIVE_COMMIT_APPROVAL_PACKET_DONE

## Dependency Gates

- F-007 core commit is complete: `531c268154450f1102e39859bc809665e1a585b4`.
- Frontend + docs follow-up commit is complete: `c574c54a50cb13b13abb68f6108ee3d60c9c7739`.
- Governance rules commit is complete: `fa7cdf27af8f4fbf566a82cd403f3f92bcfc8fb5`.
- Longterm housekeeping commit is complete: `92236e220356ffa60faa2a2ce2f94b3ac542a3d2`.
- Remaining dirty scope now contains only historical specs and generated/external large artifacts:
  - `specs/gtmanager-gtclaw-m1-runtime-localization/`
  - `specs/gtmanager-m1-branding-localization/`
  - `clawmanagerArm/`
  - `dist/`
- Current `git diff --cached --name-only` is empty.

## User Decision Requested

Please approve or reject the next serial gate:

`Historical Specs Archive Commit Gate`

This packet does not authorize staging or committing by itself. The next gate
must receive a clear approval option before it stages historical specs.

## Historical Specs Metadata

Observed metadata from read-only inspection:

- `specs/gtmanager-gtclaw-m1-runtime-localization/`
  - approximate size: 236K
  - file count: 12
  - contents: spec, plan, tasks, design amendment, and evidence markdown
  - purpose: GTManager / GTClaw runtime localization and control-ui proxy design record
- `specs/gtmanager-m1-branding-localization/`
  - approximate size: 20M
  - file count: 210
  - contents include 22 markdown files, 73 PNG screenshots, 86 log files, 20 JSON files, 6 TSV files, 5 MJS scripts, 4 TXT files, 3 JS files, and 3 HTML files
  - purpose: GTManager M1 branding/localization planning, E2E runs, screenshots, deployment refresh evidence, and asset rerun evidence

Largest observed files are below 1M each. The total historical specs size is
moderate compared with `clawmanagerArm/` and `dist/`, but the screenshot/log/JSON
artifact mix still requires an explicit archive decision and secret-shape scan.

## Approval Options

Recommended option:

- `APPROVE_HISTORICAL_SPECS_FULL_ARCHIVE`
  - Future gate may stage and commit both historical spec directories in full:
    - `specs/gtmanager-gtclaw-m1-runtime-localization/**`
    - `specs/gtmanager-m1-branding-localization/**`
  - Future gate must first run a secret-shape scan over text-like candidate files without printing matched plaintext.
  - If the scan finds token, cookie, credential, password, authorization, or access URL shaped plaintext, the future gate must STOP and write blocked evidence without staging or committing.
  - This option best preserves traceability because markdown, scripts, logs, JSON, and screenshots remain together.

Alternative option:

- `APPROVE_HISTORICAL_SPECS_MARKDOWN_ONLY`
  - Future gate may stage and commit only markdown/design/task/spec files and small scripts explicitly listed by the future gate.
  - Screenshots, logs, JSON, HTML, JS bundles, TSV, and other evidence artifacts remain untracked.
  - This option reduces artifact risk but leaves the workspace dirty and weakens historical evidence reproducibility.

Reject/defer option:

- `REJECT_OR_DEFER_HISTORICAL_SPECS`
  - Future gate does not stage or commit these specs.
  - Historical specs remain untracked until a later archive or cleanup decision.

## Recommended Future Commit Scope For Full Archive

If the user chooses `APPROVE_HISTORICAL_SPECS_FULL_ARCHIVE`, the future commit
scope is limited to:

- `specs/gtmanager-gtclaw-m1-runtime-localization/**`
- `specs/gtmanager-m1-branding-localization/**`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-historical-specs-archive-commit-approval-packet.md`
- Future historical specs archive commit evidence created by the approved gate

No other paths should be included.

## Required Future Gate Checks

Before any future Historical Specs Archive Commit Gate may stage or commit, it
must run and report:

- `git diff --cached --name-only` before staging; must be empty.
- `find`/`du` metadata for both historical specs directories.
- Text-like file secret-shape scan over candidate files, with matched plaintext redirected to a temporary file that is deleted and not printed.
- Whitespace checks:
  - `git diff --check -- <tracked/evidence paths>`
  - no-index whitespace checks for newly tracked text files where practical
- Staged path allowlist check proving only the approved historical specs and new evidence are staged.
- Staged deletion check proving no deletion is staged.
- Commit evidence recording chosen option, file count, size summary, scan status, and explicit exclusions.

## Explicit Exclusions

The future Historical Specs Archive Commit Gate must exclude:

- `clawmanagerArm/**`
- `dist/**`
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
- runtime image/artifacts outside the historical specs directories

## Cleanup/Delete Requirement

This packet does not authorize cleanup or deletion.

Cleanup or deletion of any of these paths requires a separate explicit cleanup
approval gate:

- `clawmanagerArm/**`
- `dist/**`
- `specs/gtmanager-*` if the user chooses removal instead of archive

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

1. If the user approves one archive option: Historical Specs Archive Commit Gate.
2. Then: Generated Artifact Cleanup Approval Packet for `clawmanagerArm/**` and `dist/**`.

## Verification Plan

Required checks for this packet:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-historical-specs-archive-commit-approval-packet.md`
- `rg -n "HISTORICAL_SPECS_ARCHIVE_COMMIT_APPROVAL_PACKET_DONE|HISTORICAL_SPECS_ARCHIVE_COMMIT_APPROVAL_PACKET_BLOCKED|Historical Specs Archive Commit Gate|APPROVE_HISTORICAL_SPECS_FULL_ARCHIVE|APPROVE_HISTORICAL_SPECS_MARKDOWN_ONLY|REJECT_OR_DEFER_HISTORICAL_SPECS|specs/gtmanager-gtclaw-m1-runtime-localization|specs/gtmanager-m1-branding-localization|secret-shape|screenshots|logs|JSON|clawmanagerArm|dist|no stage|no commit|no cleanup|no delete|no Mem0|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-historical-specs-archive-commit-approval-packet.md`
- secret-shape scan on this approval packet
- no-index whitespace check on this approval packet
- `git diff --cached --name-only`
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-historical-specs-archive-commit-approval-packet.md`

## Packet Boundary

This approval packet only prepares the user decision for a later historical
specs archive commit gate. It does not stage, commit, cleanup, delete, inspect
secret contents, mutate runtime/K8S/database/registry/browser state, write Mem0,
write `passes:true`, or Close.
