# UnifiedFramework

This directory adapts the `codeSPEC` unified framework to ClawManager.

It is a boundary and recovery layer. It does not replace project rules, feature
specs, longterm facts, or execution discipline.

## Reading Order

1. `15-Reusable-Minimum-Kernel-Starter-Template.md`
2. `01-Unified-Framework-Architecture.md`
3. `03-Interface-Contracts.md`
4. `05-Migration-Playbook.md`
5. `12-Superpower-Execution-Bridge.md`
6. `14-P0-Temporary-Ledger-Mechanism.md`
7. `16-GPT-Pro-Decision-Gate.md`
8. `ledger/README.md`
9. `ledger/tracking-ledger.md`
10. Latest file under `ledger/execution/`

## Current Migration Strategy

- Short term: use Scheme A, the minimum kernel from `codeSPEC`, to stabilize
  current ClawManager work.
- Long term: migrate toward Scheme B by turning recurring process risks into
  tested contracts similar to `codeSPEC/memory_context_recovery`.

## Authority Boundary

- Project rules: root `AGENTS.md`, `.specify/memory/constitution.md`, and
  deeper `AGENTS.md` files.
- Project facts: `longterm/workspace/`.
- Feature delivery: `specs/<feature>/spec.md`, `plan.md`, and `tasks.md`.
- Execution method: runtime skills and current-session discipline.
- Orchestration: `AgentTeam/` only as an optional overlay.
- Recording surface: `UnifiedFramework/ledger/`, non-authoritative.
- External expert escalation: `16-GPT-Pro-Decision-Gate.md`, mediated by the
  Commander through user-forwarded prompts only.
