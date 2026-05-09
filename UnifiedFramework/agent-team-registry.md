# Agent-Team Registry

Date: 2026-05-07
Status: canonical registry for ClawManager agent-team naming

## Purpose

This agent-team registry prevents team-name sprawl. It defines the only
canonical role names and maps legacy names to stable role plus task-type pairs.
The registry is operational guidance only; it does not authorize deployment,
runtime mutation, Close, passes:true, longterm write-back, or git operations.

## Canonical Roles

Commander:

- Single decision center.
- Owns gate sequencing, prompt packaging, scope control, and final verdicts.
- Does not skip gates.

Worker:

- Executes a bounded task with an explicit file/resource write scope.
- May be split into WorkerA, WorkerB, and WorkerC only when true parallel work
  has disjoint write sets and no shared blocking dependency.

Verifier:

- Checks evidence, command output, hashes, status, and secret-shape scans.
- Does not implement the fix being verified.

Reviewer:

- Reviews architecture, behavior, code risk, or scope risk.
- Produces findings and open questions before summaries.

Closer:

- Used only after fresh E2E evidence and explicit user approval.
- Checks Close/write-back prerequisites and never sets acceptance alone.

## Task Types

Design:

- Converts goals into specs, plans, gate packets, and approval packets.
- Former "task designer" sessions map here.

Research:

- Performs read-only investigation, root-cause analysis, and option comparison.
- Former "Investigator" and "RuntimeResearcher" sessions usually map here.

Implementation:

- Changes approved files or resources inside a gate.

Evidence Review:

- Reads evidence and validates markers, hashes, and negatives.

Architecture Review:

- Reviews cross-cutting design or migration risks.

Closeout:

- Checks final E2E, user approval, write-back, and Close requirements.

## Legacy Alias Map

These names may appear in screenshots, old sessions, or historical evidence.
They are aliases, not new standing roles.

- Investigator -> Worker with Research task type, or Verifier when strictly
  validating existing evidence.
- RuntimeResearcher -> Worker with Research task type.
- ScopeReviewer -> Reviewer.
- EvidenceVerifier -> Verifier.
- CloseGateChecker -> Closer.
- workerA, workerB, workerC -> Worker lanes for approved parallel disjoint
  scopes only.
- DeploymentRefreshWorker -> Worker with Implementation or Deployment task
  scope, only after explicit deployment approval.
- ImageDeliveryWorker -> Worker with Implementation task type, only after
  explicit image/build/push approval.
- RuntimeImageBuildTagPushOriginAllowlistWorker -> Worker with Implementation
  task type.
- RuntimeImageBuildTagPushOriginAllowlistApprovalPacketWorker -> Worker with
  Design task type.
- RuntimeImageAssemblyArtifactApprovalPacketWorker -> Worker with Design task
  type.
- FreshInstanceRuntimeDeploymentApprovalPacketWorker -> Worker with Design task
  type.
- BrowserManualE2EWorker -> Worker with Evidence Review or E2E task type, only
  after explicit browser/manual E2E approval.
- zsh -> shell/session, not an agent.

## Governance Rules

- Do not create new persistent role names.
- Prefer the five canonical roles: Commander, Worker, Verifier, Reviewer,
  Closer.
- Use task types to describe specialization instead of adding permanent roles.
- Reuse the closest existing worker lane for similar work.
- Use WorkerA/B/C only for real parallelism with disjoint scopes.
- Keep all worker prompts forwardable by the user.
- Each prompt must state recipient, serial/parallel mode, dependency gate,
  allowed writes, forbidden writes/actions, and validation commands.
- GPT Pro is a decision gate, not a standing role. The Commander decides when
  task difficulty, architecture risk, or stubborn uncertainty warrants a
  GPT Pro review/research/patch proposal packet.
- GPT Pro output is advisory. Repo-local gates, evidence, and user approvals
  remain authoritative.

## Current Operating Shape

For ClawManager, the default team shape is:

- Commander: one.
- Worker: one active lane by default.
- Verifier: used for evidence-sensitive or risky gates.
- Reviewer: used for architecture/code review or high-blast-radius changes.
- Closer: dormant until fresh E2E evidence and explicit close approval exist.

Parallel workers are allowed only when the Commander can prove the scopes are
independent and the write sets do not overlap.
