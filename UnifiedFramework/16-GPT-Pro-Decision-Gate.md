# GPT Pro Decision Gate

## Purpose

GPT Pro is a high-value external expert path for hard ClawManager decisions. It
should be used deliberately, not as another always-on agent.

This gate decides whether the current task should receive GPT Pro review,
research, or a patch proposal.

## Authority

- The Commander decides whether escalation is recommended.
- The user decides whether to send the prompt.
- GPT Pro output is advisory until the Commander reviews it.
- Any implementation still goes through normal ClawManager gates.

The Commander must not contact GPT Pro directly unless the user explicitly
authorizes that tool path. Default behavior is to provide a ready-to-forward
prompt.

## Escalation Types

### Review

Use when the current design, code, evidence, or architecture needs independent
judgment.

Examples:

- backend proxy semantics,
- K8S/runtime image delivery safety,
- browser E2E evidence interpretation,
- close/write-back readiness.

### Research

Use when facts are uncertain and local repo evidence is not enough.

Examples:

- external runtime behavior,
- ambiguous framework migration choices,
- competing implementation strategies.

### Patch Proposal

Use when a concrete fix is needed but local attempts are blocked or the blast
radius is high.

GPT Pro may propose a patch, but ClawManager still requires normal local review,
allowed write scopes, verification, and user-approved mutation gates.

## Trigger Checklist

Escalation is recommended when any of these is true:

- The Commander has meaningful uncertainty.
- The issue crosses 2+ layers, such as backend + runtime image + browser.
- There is architecture, security, data, registry, K8S, or Close/write-back risk.
- Two normal local loops have not converged.
- A wrong decision would cause costly rebuild/deploy/E2E churn.
- Evidence is contradictory or stale.
- The user explicitly requests GPT Pro review or research.

Escalation is usually unnecessary when:

- the next action is a narrow read-only verification,
- the fix is obvious and covered by existing tests,
- the task is simple documentation cleanup,
- the user has not approved sharing a prompt externally and the local path is
  still low risk.

## Prompt Packet Requirements

Every GPT Pro packet must include:

- exact decision needed,
- relevant repo paths,
- confirmed facts,
- current hypotheses,
- constraints and forbidden actions,
- evidence files and command outputs summarized,
- what kind of response is requested: review, research, or patch proposal,
- explicit instruction to avoid secrets and avoid assuming unprovided context.

Do not include tokens, cookies, passwords, private keys, or plaintext access
URLs.

## Integration Rule

After GPT Pro returns:

1. Commander reviews the output.
2. If useful, route it to `Reviewer` or `Worker` using normal gates.
3. Verify locally.
4. Record evidence.
5. Do not mark accepted, `passes:true`, or Close solely because GPT Pro agreed.
