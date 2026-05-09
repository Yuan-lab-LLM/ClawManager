# Long-Running Agent Method (Localized)

## Source baseline
- Article: `Effective harnesses for long-running agents` (Anthropic Engineering), published on November 26, 2025.
- Reference implementation: `anthropics/claude-quickstarts`, `autonomous-coding` demo.
- Local captures (via MCP browser automation):
  - `research/article_effective_harnesses_2025-11-26.txt`
  - `research/repo_autonomous_coding_readme.txt`
  - `research/repo_autonomous_coding_tree_page.txt`

## Core principles
1. Separate initialization from coding.
2. Persist state in files and git, not in model memory.
3. Make incremental progress (one feature at a time).
4. Enforce end-to-end verification before marking done.
5. End every session in a clean, mergeable state.

## Localized workflow in this repository
Workspaces live under `longrun/workspaces/<project_name>/`.

Each workspace must contain:
- `app_spec.md`: product and technical requirements.
- `feature_list.json`: source of truth for deliverables.
- `init.sh`: repeatable environment bootstrap/start script.
- `claude-progress.txt`: handoff notes between sessions.

## Unified Framework boundary

ClawManager now uses a codeSPEC-aligned minimum kernel:

`Project Rules -> longterm -> specs -> execution -> evidence -> write-back`

In this kernel, `longterm` owns durable project facts and progress. It does not own
feature implementation details, execution behavior, or acceptance by itself.
`UnifiedFramework/ledger/` may point to current stop points, but it is
non-authoritative and cannot mark `passes:true` or Close.

## Migration into an existing project
1. Copy `longrun/` into the target repository root.
2. Run `./longrun/scripts/bootstrap.sh <project_key>`.
3. In `app_spec.md`, fill migration constraints first.
4. Configure workspace `init.sh`:
- set `PROJECT_ROOT` when needed,
- optionally define `SMOKE_TEST_CMD` and `APP_START_CMD`.
5. Run `./init.sh` once before Session 1 to verify repeatable startup.

## `feature_list.json` contract
Required fields:
- `id`, `priority`, `category`, `description`, `steps`, `passes`

Recommended fields:
- `component`, `depends_on`, `acceptance_criteria`, `evidence_hint`, `risk`

Notes:
- `depends_on` expresses feature prerequisites.
- `next_feature.py` prioritizes unblocked items.
- `progress_report.py` surfaces dependency blockers and missing references.

## Session types

### Session 1: Initializer
Goal: create the durable scaffolding.

Required outputs:
- Complete initial `feature_list.json` (all features start with `passes: false`).
- Working `init.sh`.
- Initial project structure from `app_spec.md`.
- First `claude-progress.txt` entry.

### Session N: Coding loop
Goal: close exactly one high-priority failing feature.

Mandatory order:
1. Get bearings (`pwd`, file list, `app_spec.md`, `feature_list.json`, `claude-progress.txt`, git log).
2. Start or recover environment with `init.sh`.
3. Re-verify 1-2 already-passing core features.
4. If regressions exist, fix regressions first.
5. Pick one unblocked `passes: false` feature.
6. Implement + end-to-end test.
7. Only then request approval to set `passes` to `true` if the current project gate requires explicit approval.
8. Update `claude-progress.txt` and commit only when explicitly authorized by the current gate.

## Quality gates
A feature can be marked done only if:
- Full user-path test passes through real UI interaction.
- No known blocker remains for that feature.
- Workspace remains runnable from `init.sh`.
- Handoff notes include what changed, evidence, and next feature.

## Failure modes and controls
- Premature "done": controlled by explicit `feature_list.json` and one-feature-per-session rule.
- Lost context across windows: controlled by `claude-progress.txt` + git history.
- Dirty handoff state: controlled by end-session checklist and approval-gated commit/write-back.
- False-positive completion: controlled by mandatory e2e verification before `passes: true`.

## Decision record for this localization
- We keep the two-agent pattern concept, but implement it as reusable templates and scripts in this repo.
- We keep strict artifact contracts (`app_spec`, `feature_list`, `init`, `progress`).
- We keep incremental and test-first progression for long-horizon reliability.
