# Longrun Harness Kit

This directory provides a reusable, migration-friendly long-running development template.
Use it as a standardized workspace contract and project-memory layer across projects.

Boundary notes:
- `longterm` is the Project Memory Layer, not the feature-delivery authority
- Feature definition still belongs to `spec规范` (`spec -> plan -> tasks`)
- Runtime execution behavior still belongs to `superpower skill`
- The prompt templates in `templates/` should be treated as compatibility scaffolds, not as the long-term Execution Layer authority

## Quick start
1. Bootstrap a workspace:
   ```bash
   ./longrun/scripts/bootstrap.sh my-first-project
   ```
2. Fill `longrun/workspaces/my-first-project/app_spec.md`.
3. Run Session 1 using the project rules + `longterm` facts as primary inputs. `longrun/templates/initializer_prompt.template.md` is an optional compatibility scaffold.
4. Run continuation sessions with project rules, `longterm` facts, and the feature-delivery path as primary inputs. `longrun/templates/coding_prompt.template.md` is an optional compatibility scaffold.
5. Track progress:
   ```bash
   python3 longrun/scripts/progress_report.py longrun/workspaces/my-first-project/feature_list.json
   ```
6. Pick next feature:
   ```bash
   python3 longrun/scripts/next_feature.py longrun/workspaces/my-first-project/feature_list.json
   ```

## Migration playbook (for existing projects)
1. Copy `longrun/` into the target repository root.
2. Create workspace:
   ```bash
   ./longrun/scripts/bootstrap.sh <project-key>
   ```
3. In `app_spec.md`, fill "Existing-project migration constraints" first.
4. Customize workspace `init.sh`:
- set `PROJECT_ROOT` if workspace is not inside the repo root.
- optionally set `SMOKE_TEST_CMD` and `APP_START_CMD`.
5. Run a dry start:
   ```bash
   cd longrun/workspaces/<project-key>
   ./init.sh
   ```
6. Start Session 1 and generate a complete `feature_list.json`.

## Feature schema contract
Required fields:
- `id`, `priority`, `category`, `description`, `steps`, `passes`

Recommended fields for stronger handoff:
- `component`, `depends_on`, `acceptance_criteria`, `evidence_hint`, `risk`

Interpretation rule:
- `steps` should stay as project-level summary steps, not as a replacement for `spec/tasks` feature breakdown
- `passes: true` should only be written after the corresponding feature can be traced to acceptance evidence or equivalent verification record

Compatibility rule:
- Existing scripts remain compatible as long as required fields are present.

## Directory map
- `METHOD.md`: localized methodology and operating rules.
- `METHOD.zh-CN.md`: Chinese version of core method.
- `CHECKLIST.md`: per-session checklist.
- `templates/`: canonical artifacts and prompt templates.
- `scripts/`: bootstrap and progress utilities.
- `workspaces/`: generated project workspaces.

## Session contract
Every coding session must:
- read bearings (`app_spec`, `feature_list`, `claude-progress`, git log),
- run environment startup from `init.sh`,
- re-verify previously passing behavior,
- complete one unpassed feature end-to-end,
- update `passes`, notes, and commit.

This session contract defines the minimum project-memory handshake. It does not replace the feature-delivery authority (`spec规范`) or the runtime execution authority (`superpower skill`).
