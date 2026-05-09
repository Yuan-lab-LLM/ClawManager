# Migration Playbook

## Goal

Migrate ClawManager from overlapping process surfaces to the codeSPEC minimum
kernel without interrupting active GTClaw work.

## Strategy

### Scheme A: Short-Term Operating Framework

Use the minimum kernel now:

`Project Rules -> longterm -> specs -> execution -> evidence -> write-back`

Adopt immediately:

- single authority map,
- limited role vocabulary,
- non-authoritative ledger,
- explicit gate approvals,
- no close/write-back without fresh E2E evidence and user approval.

### Scheme B: Long-Term Contract Migration

Gradually add tested process contracts inspired by
`codeSPEC/memory_context_recovery` when repeated failures justify the weight.

Good candidates:

- gate state drift,
- stale handoff packages,
- verifier/reviewer boundary confusion,
- close/write-back without sufficient authority,
- agent topology or role proliferation.

Do not copy the full TypeScript contract layer until there is a concrete
ClawManager use case and an approved migration gate.

## Phases

### Phase 0: Freeze Authority

Done when the project can answer:

- Project rules: `AGENTS.md` and constitution.
- Project facts: `longterm/workspace/`.
- Feature delivery: `specs/<feature>/`.
- Execution method: current-session discipline and skills.
- Orchestration: `AgentTeam/` only as optional overlay.
- Recovery records: `UnifiedFramework/ledger/` only as non-authoritative.

### Phase 1: Create the UnifiedFramework Entry

Add this directory and ledger surface without changing business code.

### Phase 2: Tighten Existing Entrypoints

Update `AGENTS.md`, `longterm`, and `AgentTeam` docs so they point to the same
minimum kernel and do not maintain competing rules.

### Phase 3: Freeze Current Active Work

Record the GTClaw post-close follow-up status in ledger:

- latest candidate runtime image,
- latest accepted gate,
- current stop point,
- forbidden actions,
- next legal gate.

### Phase 4: Continue Feature Work Under Scheme A

Proceed only through approved gates. Use generic roles only.

### Phase 5: Introduce Scheme B Contracts Incrementally

When a process risk repeats, propose a narrow contract/test artifact instead of
adding more Markdown ceremony.
