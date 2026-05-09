# Execution Ledger: ClawManager Framework Migration Start

- Record ID: `EXEC-2026-05-07-01`
- Round/session: `ClawManager codeSPEC Scheme A migration start`
- Current goal: `Adopt codeSPEC minimum kernel as the short-term operating framework and preserve a path toward Scheme B contract migration.`

## Input Boundaries / Frozen Facts

- User approved Scheme A now and gradual migration toward Scheme B later.
- This ledger is non-authoritative and cannot mark acceptance, `passes:true`, or Close.
- No business code, runtime image, registry, Kubernetes, database, browser E2E, commit, push, or Mem0 write is authorized by this migration entry.
- Current active feature chain remains `gtclaw-runtime-controlui-persistent-image` post-close follow-up.

## Completed Work

- Introduced a ClawManager-adapted `UnifiedFramework/` minimum kernel.
- Introduced a non-authoritative `UnifiedFramework/ledger/` recording surface.
- Froze current GTClaw follow-up stop point for recovery.

## Changed Files

- `UnifiedFramework/`: added Scheme A framework and ledger documents.
- `AGENTS.md`: updated to point at the minimum kernel and generic role vocabulary.
- `.specify/memory/constitution.md`: clarified UnifiedFramework as a boundary contract.
- `longterm/README.md`: clarified project-memory-only boundary.
- `longterm/METHOD.md`: clarified that commit/write-back is approval-gated in ClawManager.
- `longterm/METHOD.zh-CN.md`: clarified that commit/write-back is approval-gated in ClawManager.
- `longterm/CHECKLIST.md`: clarified approval-gated end-of-session actions.
- `AgentTeam/README.md`: reduced AgentTeam to optional overlay and generic roles.
- `AgentTeam/04-Quick-Reference.md`: reduced role vocabulary and close/write-back authority.

## Verification

- `find UnifiedFramework -type f | sort`: confirmed 12 new framework/ledger files.
- Framework marker `rg`: exit `0`; confirmed Scheme A, Scheme B, minimum kernel path, generic roles, non-authoritative ledger, current GTClaw stop point, and latest candidate runtime image markers.
- `git diff --check -- AGENTS.md .specify/memory/constitution.md longterm/README.md longterm/METHOD.md longterm/METHOD.zh-CN.md longterm/CHECKLIST.md AgentTeam UnifiedFramework`: exit `0`; no whitespace errors.
- Secret-shape scan over the migration docs: no matched output.
- Scoped `git status --short`: confirmed only approved framework/doc paths plus new `UnifiedFramework/` in the migration scope.

## Output

- Scheme A is the short-term operating framework.
- Scheme B remains a long-term target for tested process contracts.

## Current Stop Point

- Latest accepted GTClaw follow-up gate:
  `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE`
- Latest candidate runtime image:
  `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- Browser/manual E2E for this image: not yet executed.
- `passes:true`, Close, and longterm write-back: forbidden until fresh E2E evidence exists and the user explicitly approves.

## Next Step

- Use the new framework to decide whether to approve the Fresh Instance / Runtime Deployment Gate.

## Related Objects

- `clawmanager-unified-framework-scheme-a`
- `gtclaw-controlui-persistence-follow-up`

## Branch / Worktree / Path / Commit

- Branch: `codex/f007-generated-artifact-cleanup`
- Worktree: `/Users/eduardogan/Desktop/GHJProject/ClawManager`
- Path: `UnifiedFramework/`
- Commit: `n/a`
