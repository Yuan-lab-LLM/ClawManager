# ClawManager Memory Setup

## How it works

- `./scripts/codex` sets `CODEX_HOME` to the repo-local `.codex`, so this project reads `.codex/config.toml` instead of `~/.codex/config.toml`.
- `.codex/config.toml` connects Codex to the official Mem0 MCP endpoint at `https://mcp.mem0.ai/mcp/`.
- Authentication is injected from the shell with `MEM0_AUTH_HEADER` through `env_http_headers`; no secret value is stored in git.
- Restart Codex after changing `.codex/config.toml`. The current session does not hot-load new MCP tools.
- Use `project-memory` as the shared project memory scope when importing, searching, and updating project-level memories.

## Legacy mapping

- Stable always-load rules stay in `AGENTS.md`, `.specify/memory/constitution.md`, `longterm/workspace/app_spec.md`, and the small session-handshake sources under `longterm/`.
- Durable facts, decisions, anti-patterns, user preferences, environment findings, and active `session_state` should be distilled into Mem0, not kept as full-text copies.
- `memory/mem0_import.json` is the migration payload for the first import batch.
- `longterm/workspace/*`, `AgentTeam/*`, and `docs/superpowers/*` remain source material and evidence trails; they are not long-term full-text dual-write targets.

## Write to `AGENTS.md`

- Repo-wide operating rules that every future agent must obey on every task.
- Memory hygiene rules such as `search_memories` first, `update_memory` on replacement, and `session_state` before context loss.
- Small stable constraints that should always load with the repo.

## Write to Mem0

- Durable project facts and architecture decisions.
- Repeatedly useful validated learnings, anti-patterns, and environment discoveries.
- User preferences and collaboration style that affect future execution.
- Current `session_state` only when it helps the next packet and will matter after the current chat context is gone.

## Do not write to Mem0

- Full legacy files.
- Chat fragments, command echo, or raw long logs without a distilled conclusion.
- Duplicate copies of facts that already live in a stable rule file.
- One-off noise that is unlikely to matter for the next meaningful task.

## Correct bad memory

- Search first.
- If the fact is the same but needs refinement, update the existing memory instead of adding a near-duplicate.
- If the old memory is wrong or obsolete, replace or delete it instead of keeping both active.
- When a blocker is cleared, update or remove stale `session_state` entries.

## Re-import legacy material

1. Review `AGENTS.md`, `.specify/memory/constitution.md`, `longterm/*`, `AgentTeam/*`, and `docs/superpowers/*`.
2. Regenerate `memory/mem0_import.json` with deduplicated durable entries only.
3. Run `mem0 import memory/mem0_import.json --user-id project-memory` if the CLI is available.
4. Re-run a narrow `mem0 search "ClawManager" --user-id project-memory` check.
5. Do not append full files to Mem0 and do not keep dual-writing the same fact to both legacy files and Mem0.

## Stable vs session state

- Stable facts include what ClawManager is, the repo layout, K3S-first deployment, the validated `9001 -> 9001` control path, the anti-pattern against the disproven `https://...:8443` route, and the collaboration preferences.
- Current `session_state` includes the ARM live path still depending on the dev runtime image, local `skill-scanner` remaining off by default, F-002 / P1 still not passed, and model-bootstrap blockers that remain active.
- Durable direction candidate: ClawManager should eventually connect to external standalone hosts or Claw boxes and dispatch work from one central control plane. That is a future architecture direction, not this packet's delivery scope.
