# P0 Temporary Ledger Mechanism

## What It Is

`UnifiedFramework/ledger/` is a lightweight recording surface for recovery and
handoff.

It records:

- what happened in a round,
- which object is current,
- where evidence lives,
- what the next legal gate is.

## What It Is Not

It is not:

- a project rule source,
- a replacement for `longterm`,
- a replacement for `specs/<feature>/evidence`,
- an acceptance system,
- a verification pass,
- durable truth promotion.

## When To Update

Update ledger when:

- a framework migration step changes the process surface,
- an active feature chain reaches a new stop point,
- a handoff needs a stable recovery anchor,
- object status would otherwise be scattered across many evidence files.

## Minimum Records

Execution records under `ledger/execution/` should include:

- current goal,
- frozen facts,
- changed files,
- verification,
- stop point,
- next step,
- related objects.

Tracking records under `ledger/tracking-ledger.md` should include:

- object name,
- type,
- current state,
- location,
- branch/worktree/path/commit if applicable,
- whether it is authoritative,
- next destination,
- latest related execution record.
