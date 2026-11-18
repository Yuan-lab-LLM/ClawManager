# Workspace follow-up and OpenClaw reliability

User-requested follow-up to the released Workspace dashboard (source6c144b4, image553416e8).

## Accepted direction

- Continue working prioritizes recently used Agents, using real user actions/conversation/model/task activity rather than infrastructure update timestamps.
- Token, resource and Agent state are separate visible panels. Token uses a number plus historical curve; resource uses speedometer gauges of assigned/quota values with eligible slots separate; state uses a prominent donut plus total/state counts. This explicit request supersedes the previous grouped tabs and previous ring preference.
- Preserve welcome, pins, activity, costs, useful native task details, CM colors, one-screen desktop, narrow groups, accessibility, truthful null/partial values and native routes.
- Investigate actual OpenClaw errors and recovery evidence before changing runtime behavior. Reuse existing bounded recovery/backoff/fencing; respect manual stop/sleep, invalid config, confirmed-writer-stop requirements and resource ownership.
- All runtime/tests/browsers/binaries/images run on54; local only source prep/review/Git/transfer. Preserve unrelated inputs; keep uiux, no merge/push.

## Work

- [x] Diagnose failures/read-only runtime evidence and existing recovery blockers; report proven cause and minimal correction.
- [x] Implement recent Agent list and independent panels with scoped tests/real browser.
- [x] Correct proven recovery defect if found, with targeted failure/fencing tests; avoid speculative or unbounded restart loops.
- [x] Independent review, exact-source cluster build/deploy and live verification, document actual findings/limits.

Recovery diagnosis and dashboard implementation are independent domains and proceed through dispatching-parallel-agents with disjoint ownership. Each has focused evidence/review; parent integrates and deploys the final concrete result to the already authorized54 environment.

Delivered CM03e7e72 to54, image3463717f. Both independent reviews approved. Live65 recovered automatically to generation2/running; all26OpenClaw running, other25generations unchanged. Native c450acb tested/committed but not worker-rolled. Detailed evidence and remaining SQLite limit: docs/uiux/2026-11-12-workspace-followup-validation.md.
