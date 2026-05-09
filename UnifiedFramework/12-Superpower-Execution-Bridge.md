# Superpower Execution Bridge

## Purpose

The execution layer is a runtime method layer, not a repository truth source.
This bridge makes that boundary visible in ClawManager without copying skill
implementations into the repository.

## Placement

The execution layer consumes:

- current user goal,
- project rules,
- longterm project facts,
- current feature specs/tasks,
- current code and evidence.

It outputs:

- session action order,
- verification discipline,
- process recommendations,
- gate decisions based on evidence.

It does not output:

- project rules,
- durable project facts,
- feature authority,
- `passes:true`,
- Close.

## Required Inputs Before Non-Trivial Work

1. Project rules are known.
2. Relevant longterm facts are known.
3. Current feature scope or equivalent gate is known.
4. Allowed mutation class is approved.

If an input is missing, the execution layer must ask to fill that upstream
layer. It must not silently become the missing layer.

## ClawManager-Specific Guardrails

- Runtime image, registry, Kubernetes, database, browser E2E, commit, push,
  close, and longterm write-back require explicit user approval.
- Evidence must be verified before a success claim.
- Current post-close GTClaw work cannot be marked complete until a fresh
  runtime deployment and browser/manual E2E pass.
