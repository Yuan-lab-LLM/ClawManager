# Lite Skill Package Materialization

Lite (gateway / Hermes) instances discover skills from the shared workspace instead of
using the instance agent `collect_skill_package` command.

## Lifecycle

1. **Inventory** — `syncLiteSkillsFromWorkspace` or runtime agent report calls
   `SyncAgentSkills`, which upserts skills and writes `instance_skills.workspace_dir`.
2. **Enqueue** — For Lite instances with empty `skill_blobs.object_key`, ClawManager
   inserts a row into `skill_package_materialize_jobs` (never `collect_skill_package`).
3. **Materialize** — The leader-only `SkillPackageMaterializeWorker` reads workspace
   directories, builds a normalized ZIP, uploads to MinIO, and runs skill-scanner.
4. **Publish** — Once `object_key` is set and scan completes, skills can be imported
   to the library and published to Skill Hub.

## Paths

| Runtime | Workspace skill root |
|---------|---------------------|
| Hermes Lite | `{workspace}/home/.hermes/skills/{name}` |
| OpenClaw Lite | `{workspace}/home/.openclaw/workspace/skills/{name}` |

The authoritative directory name is stored in `instance_skills.workspace_dir`.

## Configuration

| Environment variable | Default | Description |
|------------------------|---------|-------------|
| `SKILL_MATERIALIZE_WORKER_ENABLED` | `true` | Enable background worker |
| `SKILL_MATERIALIZE_TICK_MS` | `2000` | Worker poll interval |
| `SKILL_MATERIALIZE_BATCH_SIZE` | `5` | Jobs claimed per tick |
| `SKILL_MATERIALIZE_CONCURRENCY` | `5` | Global worker concurrency |
| `SKILL_MATERIALIZE_PER_INSTANCE_CONCURRENCY` | `2` | Max parallel jobs per instance |

## Agent commands

Pro and Shell instances still use `collect_skill_package` via the instance agent.
Lite instances **do not**; package collection is server-side only.

For Lite inventory, ClawManager treats the shared workspace scan as the authoritative
`content_md5` source. Runtime agent reports may differ; server-side materialize always
recomputes from workspace and self-heals stale blob hashes instead of failing with
`skill package md5 mismatch`.

## Backfill

On worker start, pending Lite blobs with `workspace_dir` set are enqueued automatically.
Migration `039_add_skill_package_materialize.sql` also cancels stale Lite
`collect_skill_package` commands and backfills `workspace_dir` from `install_path`.

## Hub UI blocked reasons

When enriching Skill Hub payloads without an explicit instance (catalog, "My Skills",
detail pages), ClawManager resolves a Lite instance from active `instance_skills` rows
for that skill. This prevents stale Pro-only `collect_skill_package` failures from
showing as `skill_package_collect_failed` on Lite-discovered skills.
