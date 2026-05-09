# Reusable Minimum Kernel Starter Template

## Short Form

For ClawManager, the codeSPEC-aligned operating path is:

1. Read project rules.
2. Read `longterm`.
3. Bind the active feature to `specs/<feature>/`.
4. Execute through explicit gates.
5. Capture evidence.
6. Write back only after acceptance evidence and explicit approval.

## Minimum Kernel

### A. Project Rules

Read:

- `AGENTS.md`
- `.specify/memory/constitution.md`
- deeper `AGENTS.md` files for touched subprojects.

### B. Project Memory

Read:

- `longterm/workspace/app_spec.md`
- `longterm/workspace/feature_list.json`
- `longterm/workspace/claude-progress.txt`

Do not update `passes:true` or progress unless acceptance evidence and user
approval exist.

### C. Feature Delivery

Read or create:

- `specs/<feature>/spec.md`
- `specs/<feature>/plan.md`
- `specs/<feature>/tasks.md`
- `specs/<feature>/evidence/`

### D. Execution

Use runtime execution discipline and skills to decide how to proceed, but do not
let execution methods replace project facts or feature specs.

### E. Optional Orchestration

Use only generic roles:

- `Worker`
- `Verifier`
- `Reviewer`
- `Closer`

### F. Optional Ledger

Use `UnifiedFramework/ledger/` for recovery and handoff, not for acceptance.

## Current GTClaw Follow-Up Stop Point

As of the 2026-05-07 migration start:

- Latest candidate runtime image:
  `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712`
- Latest accepted gate:
  `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE`
- Next legal gate:
  user approval for `Fresh Instance / Runtime Deployment Gate`.
- Browser/manual E2E:
  not yet executed for this latest image.
- Close/write-back:
  forbidden until fresh E2E evidence exists and the user explicitly approves.
