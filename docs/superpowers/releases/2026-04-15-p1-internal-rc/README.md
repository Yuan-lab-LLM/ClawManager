# 2026-04-15 P1 Internal RC Packet

This directory is the canonical release packet for `R3-A-U3-F2-P1`.

## Current Verdict
- Packet state: `BLOCKED_INTERNAL_RC`
- This packet now freezes the honest Mac/ARM local validation path through:
  - fresh `k3d` replay from zero
  - documented live-only ARM overrides for `skill-scanner` and OpenClaw runtime bootstrap
  - `app` / `gateway` / `admin` ready
- This packet does **not** justify a full release-ready or full `U3` validation claim.

## What Is Proven
- The validated control-plane registration path is the K3S Service `9001 -> 9001`, not `https://...:8443`.
- The OpenClaw ARM bootstrap path is real and versioned.
- A fresh Mac/ARM replay can honestly reach `healthz=200`, admin login, and ARM runtime bootstrap visibility after the documented live-only ARM overrides.
- The current Source B path does not naturally surface non-empty skill rows under the observed product path.

## Blocking Boundaries
- Gate 1: repo-default `skill-scanner` image is not ARM64-capable on Mac/ARM fresh deploy.
- Gate 2: fresh replay stops at the documented operator model-bootstrap prerequisite until one active normal model and one active secure model exist.
- Gate 3: Source A is frozen at `0` rows and Source B remains empty even after a real agent inventory report.

## Honest Tutorial Claim
- The current packet can support a from-zero tutorial only up to the operator model-bootstrap boundary.
- It cannot honestly claim validation-instance readiness, direct QA readiness, or full `U3` completion.

## Canonical Artifacts
- `01-deploy-preflight.md`: deploy-preflight truth
- `02-model-bootstrap.md`: model/bootstrap truth
- `03-u3-task-inventory.md`: frozen U3 scope
- `04-acceptance-matrix.md`: per-task validation status
- `05-gate-record.md`: Gate 1-5 PASS/BLOCKED state
- `06-evidence-index.md`: evidence path index
- `07-known-issues.md`: blockers / known issues / deferred follow-ups
- `08-operator-runbook.md`: operator reproducibility guide
- `09-user-quick-start.md`: user-facing validation flow
- `10-troubleshooting.md`: bounded troubleshooting matrix
- `11-release-checklist.md`: final closure checklist

No release claim is valid unless it can be traced to these files.
