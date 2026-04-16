# User Quick Start

## Before the first direct QA
- operator must confirm the ARM runtime image bootstrap is in place
- operator must confirm one active normal model and one active secure model exist

## Honest Mac/ARM stopping point
- On Mac/ARM local validation, repo-default deploy currently needs the documented live-only `skill-scanner` ARM override before the control plane becomes healthy.
- After that override, the user may verify:
  - `https://localhost:30443/healthz` returns `200`
  - admin login succeeds
  - the OpenClaw ARM bootstrap image is visible in system image settings
- Stop there if the operator has not yet created one active normal model and one active secure model.

## Do Not Claim Yet
- do not claim validation-instance readiness while `/api/v1/admin/models` is empty
- do not claim direct QA readiness without both active model records
- do not claim full `U3` completion while `/api/v1/admin/skills` remains empty
