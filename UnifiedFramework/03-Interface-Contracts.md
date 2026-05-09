# Interface Contracts

## Core Principle

Read access may cross layers. Write authority must stay with the layer that owns
the fact being changed.

## Read / Write Matrix

| Layer | May read | May write |
| --- | --- | --- |
| Project rules | N/A | Stable project rules only |
| `longterm` | Project evidence and accepted outcomes | Durable project facts and progress |
| `specs/<feature>` | Project rules and longterm facts | Feature-local scope, plan, tasks, evidence |
| Execution layer | Rules, longterm, specs, code, evidence | Session actions and verification results |
| `AgentTeam` | Rules, longterm, specs, execution constraints | Dispatch prompts and result summaries only |
| `ledger` | Rules, longterm, specs, evidence, git status | Non-authoritative execution/tracking records |

## Required Handshakes

### Before Feature Work

1. Read project rules.
2. Read `longterm/workspace/app_spec.md`.
3. Read `longterm/workspace/feature_list.json`.
4. Identify one active feature and its `specs/<feature>/` directory.
5. Confirm allowed and forbidden actions.

### Before Implementation

1. Confirm a spec/plan/tasks or explicit equivalent gate exists.
2. Confirm approval for any mutation class: code, runtime image, registry,
   database, Kubernetes, browser E2E, commit, write-back.
3. Declare allowed write paths and forbidden paths.

### Before Verification Claims

1. Run the verification commands for the claim.
2. Read command output and exit codes.
3. Report only what is proven.

### Before Close / Write-Back

1. Confirm fresh acceptance evidence.
2. Confirm user approval for close/write-back.
3. Update only the approved longterm/project facts.
4. Record the close in ledger if enabled.

## AgentTeam Contract

AgentTeam is an overlay. It may package work for:

- `Worker`: executes a bounded task.
- `Verifier`: performs read-only or explicitly approved verification.
- `Reviewer`: reviews code, architecture, risks, or evidence.
- `Closer`: performs approved close/write-back/commit work.

Do not create role names for each small gate. Put the gate name in `任务类型`.

Task types are mandatory when appropriate:

- Use `Design` when requirements, task structure, or gate design is unclear.
- Use `Research` when repo facts, runtime behavior, or external facts must be
  established before action.
- Use `Implementation` only after scope and write permissions are clear.
- Use `Evidence Review` before trusting a worker's completion report.

The Commander must not skip a needed task type just to keep the team small. The
anti-bloat rule is about persistent agent identities, not about skipping real
work.

Parallel `WorkerA` / `WorkerB` / `WorkerC` lanes require all of the following:

1. Each lane has a distinct task type and scope.
2. Write sets do not overlap.
3. Verification paths are clear.
4. The Commander can integrate results without making subagents communicate.

If any condition is missing, run the work serially.

## GPT Pro Decision Contract

Before packaging a GPT Pro prompt, the Commander must classify the need as one
of:

- `review`: evaluate design, code, evidence, or architecture risk.
- `research`: investigate uncertain behavior, external constraints, or
  alternative approaches.
- `patch proposal`: propose a concrete fix when local work is blocked or risk is
  high.

GPT Pro escalation is recommended when at least one trigger is present:

- architecture or security risk is meaningful,
- root cause remains uncertain after local investigation,
- two normal worker/reviewer loops do not converge,
- runtime/K8S/image/browser behavior crosses multiple layers,
- a wrong decision would cause expensive rework or unsafe mutation.

The output of GPT Pro is advisory. It must be reviewed by the Commander and, if
code changes are needed, routed through normal Worker/Reviewer/Verifier gates.

## Ledger Contract

Ledger records are:

- useful for recovery,
- traceable to evidence,
- non-authoritative,
- not acceptance,
- not verification pass,
- not durable truth promotion.
