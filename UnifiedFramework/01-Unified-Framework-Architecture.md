# Unified Framework Architecture

## Purpose

ClawManager already had project rules, `longterm`, feature specs, evidence
files, and AgentTeam templates. The problem was not missing material; the
problem was unclear ownership between layers.

This document defines the codeSPEC-aligned minimum kernel for ClawManager.

## Minimum Kernel

The normal path is:

`Project Rules -> longterm -> specs -> execution -> evidence -> write-back`

## Layers

### Project Rules Layer

Owned by:

- `AGENTS.md`
- `.specify/memory/constitution.md`
- deeper `AGENTS.md` files such as `backend/AGENTS.md` and `frontend/AGENTS.md`

Responsible for stable project rules, forbidden actions, quality gates, and
subproject conventions.

### Project Memory Layer

Owned by:

- `longterm/workspace/app_spec.md`
- `longterm/workspace/feature_list.json`
- `longterm/workspace/claude-progress.txt`

Responsible for durable project facts, backlog state, and session handoff facts.
It must not replace feature-level `spec -> plan -> tasks`.

### Feature Delivery Layer

Owned by:

- `specs/<feature>/spec.md`
- `specs/<feature>/plan.md`
- `specs/<feature>/tasks.md`
- feature evidence under `specs/<feature>/evidence/`

Responsible for one feature's scope, design, implementation tasks, and
acceptance evidence.

### Execution Layer

Owned by current-session execution discipline and runtime skills. It decides how
to proceed in the session, but it does not become a project fact store or a
feature authority.

### Orchestration Overlay

Owned by `AgentTeam/` when explicitly used. It is optional and only coordinates
work. It does not replace project rules, longterm, specs, or evidence.

For ClawManager, allowed role vocabulary is intentionally small:

- `Worker`
- `Verifier`
- `Reviewer`
- `Closer`

The Commander remains the hub. Star topology is the default.

Task types such as `Design`, `Research`, `Implementation`, and `Evidence Review`
are not permanent agent identities. They are assignments placed on a generic
role. The Commander must use them when a task genuinely needs that capability,
but must avoid creating a new long-lived agent name for each gate.

Parallel `WorkerA` / `WorkerB` / `WorkerC` style lanes are allowed only when
work is actually parallelizable and write/read ownership is disjoint. Otherwise,
the Commander should stay serial.

### Recording Surface

Owned by `UnifiedFramework/ledger/`. It records current stop points and object
status for recovery. It is non-authoritative and cannot declare acceptance,
verification pass, `passes:true`, or Close.

### External Expert Escalation

Owned by the Commander and governed by
`16-GPT-Pro-Decision-Gate.md`. GPT Pro can be used for review, research, or
patch proposals when task difficulty, uncertainty, or risk justifies it. The
Commander packages a ready-to-forward prompt; the user decides whether to send
it. GPT Pro output is advisory until reviewed and integrated through normal
ClawManager gates.

## Authority Order

When sources conflict, use this order:

1. Current user instruction.
2. Project rules in `AGENTS.md` and constitution.
3. This UnifiedFramework boundary definition.
4. `longterm` project facts and current feature specs.
5. Optional AgentTeam overlay.
6. Execution-layer method guidance.

## Completion Rule

No feature may be marked `passes:true`, closed, or written back as complete
unless fresh acceptance evidence exists and the user explicitly approves that
close/write-back scope.
