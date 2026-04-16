# ClawManager P1 Internal Release Design

**Date:** 2026-04-15  
**Status:** Approved after written user review; ready for `writing-plans`.  
**Primary Goal:** Deliver a P1 internal-release candidate that can be reproduced from a fresh local K3S deployment and validated end-to-end, while preserving a short path to P2 external release.

---

## 1. Objective

Tonight's target is **P1 internal release**, not full public release.

P1 means:
- Start from an empty local K3S/k3d cluster
- Deploy ClawManager from repository source-of-truth
- Create an OpenClaw instance on ARM local hardware
- Reach direct OpenClaw question-answer flow after instance startup
- Validate all visible tasks under the frozen `U3` scope
- Produce internal operator documentation, troubleshooting, and acceptance evidence

P2 is deferred but must remain a short follow-up path. P1 work must not introduce one-off hacks that block later externalization.

---

## 2. Frozen Scope

### 2.1 Release target

This design targets **R3-A-U3-F2-P1**:
- `R3`: unreleased-ready including FastSkill / task-chain validation
- `A`: all visible tasks must be tested, not a sample subset
- `U3`: task scope is the union of:
  - selected validation instance-reported skills inventory snapshot
  - tasks discovered and surfaced through FastSkill / `skill-scanner`
  - canonical Source A collection path is manager-side `GET /api/v1/instances/:id/skills`
  - `U3` is not a narrative concept; it must be materialized as one frozen, versioned task-inventory artifact produced by `L2` from the selected validation instance-reported skills inventory snapshot plus the final FastSkill / `skill-scanner` discovery snapshot
- `F2`: validation starts from fresh deploy, not only the current live machine state
- `P1`: internal-release quality, with explicit operator steps allowed if they are documented, reproducible, and evidence-backed

### 2.2 Non-goals for tonight

The following are explicitly out of scope for tonight's definition of done:
- public self-service external release packaging
- SSO / LDAP / multi-cluster / GPU / full production TLS
- exhaustive combinatorial scenario testing for every task
- major architecture redesign of runtime, gateway, or frontend

---

## 3. Current Reality Baseline

The current live machine already proved several facts:
- local ARM OpenClaw runtime can be scheduled and brought to `agent online` and `openclaw running`
- k3s `clawmanager-gateway` needed `9001 -> 9001` service exposure for runtime agent registration
- for runtime bring-up, the validated control path is HTTP `clawmanager-gateway:9001`; do not reopen `https://...:8443` URL-builder changes as the primary fix unless new evidence disproves the live registration path
- AI gateway chat now works with both normal and secure model paths when live model records are configured correctly
- current live success still depends on non-source-controlled state:
  - ARM runtime dev image override using `ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434`
  - admin model records and API credentials in database
  - current live cluster state

Therefore the system is **live-ready**, but not yet **fresh-deploy unreleased-ready**. Live runtime bring-up is proven, but ARM default-image truth and fresh-deploy model/bootstrap truth remain productization gaps rather than solved source-controlled defaults.

---

## 4. Productization Strategy

The recommended strategy is **release-candidate layering**, not big-bang completion.

### 4.1 Why layered RC is required

A big-bang push would mix:
- deployment repair
- runtime repair
- model bootstrap
- task discovery
- task validation
- documentation writing

That would destroy evidence boundaries and make release confidence untrustworthy.

### 4.2 How layered RC becomes releaseable

P1 is treated as an internal release candidate with five gates:
- **Gate 1: Repeatable deploy** — fresh cluster deploy succeeds from repository truth
- **Gate 2: Repeatable startup** — instance creation and direct QA work without hidden manual repair
- **Gate 3: Capability completeness** — all `U3` tasks are frozen and validated
- **Gate 4: Operability** — deployment, admin operations, and troubleshooting are documented
- **Gate 5: Release closure** — acceptance matrix and known-issues list are explicit

A gate is considered PASS only when its corresponding acceptance rows in `7.1`-`7.5` are satisfied and linked from the release evidence index with explicit `PASS` or `BLOCKED` disposition.

P2 is produced by extending P1 in three dimensions:
- reduce operator-only manual setup
- reduce local-environment coupling
- upgrade internal documentation to externally consumable product documentation
- specifically reduce or productize the documented P1 model-bootstrap and secret-injection steps without assuming live-environment state

---

## 5. Workstreams

Tonight's work is decomposed into four primary workstreams.

Workstreams are coordinated by gates, not treated as fully parallel: `L1` begins by freezing one deploy-preflight artifact for cluster assumptions and required operator inputs, `L2` freezes `U3` scope, `L3` validates only the frozen scope, and `L4` may draft in parallel but cannot close before `L1`-`L3` evidence is frozen.

### L1. Fresh Deploy Baseline

Purpose:
- prove repository truth can recreate a working local environment from empty cluster state

Focus areas:
- deployment truth anchored to `deployments/k3s/clawmanager.yaml`
- one documented deploy-preflight artifact covering fresh-cluster assumptions, local resource envelope, and required operator-provided inputs
- documented ARM runtime bootstrap path acceptable for P1, without claiming repo-default ARM product truth
- gateway service exposure and validated runtime registration path over HTTP `clawmanager-gateway:9001`
- active normal/secure model records and provider credentials treated as explicit bootstrap inputs, not repository-seeded defaults or assumed live DB carry-over

Exit criteria:
- empty cluster to manager reachable and login works
- OpenClaw instance can be created and reach runtime acceptance plus direct QA through documented bootstrap inputs

### L2. Task Capability Baseline

Purpose:
- make task discovery and execution chain real, not assumed

Focus areas:
- `skill-scanner`
- FastSkill / task discovery path
- freeze final `U3` task inventory before full validation

Because the current local K3S truth keeps `skill-scanner` disabled by default, the `U3` inventory does not pre-exist and must be explicitly discovered and frozen in `L2` before any full `U3` validation claim.

For Source A, `L2` must freeze one selected validation instance-reported skills inventory snapshot collected through manager-side `GET /api/v1/instances/:id/skills`; runtime `/skills` may be used as operator cross-check only and is not canonical Source A.

Exit criteria:
- a single frozen, versioned `U3` task-inventory artifact is written and accepted as the sole validation scope for `L3`
- discovered task list is stable enough to validate without silent task appearance or disappearance during acceptance

### L3. Full U3 Validation

Purpose:
- test every task in the frozen `U3` inventory with one minimal successful path and preserve evidence

Rules:
- no cross-task combinatorial expansion
- no “should work” claims without fresh evidence
- `L3` must not add or remove task rows outside the frozen `U3` artifact
- failures remain visible and are tracked as blockers, known issues, or deferred follow-ups with evidence path, impact, and next-boundary disposition

Exit criteria:
- every frozen `U3` task row has either PASS evidence or an explicit blocker, known issue, or deferred follow-up record

### L4. Internal Release Documentation

Purpose:
- convert live operator memory into internal-release documentation

Required artifacts:
- deployment guide
- model/bootstrap/config guide
- operator runbook
- user quick-start
- troubleshooting guide
- acceptance matrix / release checklist
- release evidence index
- gate record

Exit criteria:
- another internal operator could plausibly follow the docs without oral hand-holding
- each required document is derived from a verified path and cites the packet or artifact set that proved the documented procedure; placeholder or unverified template text does not satisfy documentation acceptance

---

## 6. Star-Topology Coordination Model

The commander remains the single coordination hub.

### 6.1 Topology

- Commander is the only node that integrates conclusions
- Sub-agents are terminal workers with **no peer-to-peer communication**
- Each sub-agent receives a bounded packet with:
  - exact objective
  - exact allowed scope
  - exact output format
  - explicit stop conditions

### 6.2 Allowed sub-agent roles

Suggested worker roles:
- deploy baseline researcher
- runtime/control path researcher
- task inventory researcher
- docs/runbook drafter
- evidence verifier

### 6.3 Integration rule

Sub-agent output is never treated as truth by itself.
Commander must:
- compare outputs against repository truth and live evidence
- reject unsupported conclusions
- merge only bounded findings into the next packet or plan

---

## 7. Acceptance Definition for P1

P1 is considered ready only if all conditions below are satisfied.

### 7.1 Deploy acceptance
- fresh cluster deploy completes from `deployments/k3s/clawmanager.yaml` plus documented operator steps only, without pre-existing cluster/database state, repo-external oral knowledge, or undocumented post-deploy manual patches
- `L1` includes a documented deploy-preflight artifact for fresh-cluster assumptions, local resource envelope, and required operator-supplied inputs; none may be assumed from residual live state
- manager is reachable and login works

### 7.2 Instance acceptance
- OpenClaw instance can be created on ARM local environment
- for P1, instance acceptance may use a documented ARM runtime bootstrap path and documented operator-supplied model/bootstrap prerequisites, but it must not be stated as repo-default or fresh-deploy truth unless those defaults are source-controlled
- runtime acceptance requires pod scheduled, instance `status=running`, `infra_status=ready`, `runtime.agent_status=online`, `runtime.openclaw_status=running`, and a successful direct QA response
- if runtime bring-up succeeds but direct QA fails due to missing active model records, secure-model availability, or provider credentials, classify it as a model-bootstrap / release-readiness blocker rather than a runtime bring-up failure

### 7.3 Task acceptance
- one frozen, versioned `U3` task-inventory artifact is the single source of truth for task scope
- the frozen artifact must record Source A from manager-side `GET /api/v1/instances/:id/skills` for a selected validation instance and Source B from the final FastSkill / `skill-scanner` discovery snapshot
- every task in the frozen `U3` artifact has at least one minimal successful test path with evidence
- the acceptance matrix and release checklist are downstream views derived from the frozen `U3` artifact and must not introduce independent task rows

### 7.4 Evidence acceptance
- every critical claim, blocker, known issue, deferred follow-up, and operator-only step cites at least one concrete artifact path such as command output, API body, log excerpt, screenshot, or doc section; chat summaries and oral memory do not count as evidence
- P1 uses one release evidence index and one gate record artifact, and no gate may be marked complete unless its evidence is linked there

### 7.5 Documentation acceptance
- internal docs cover deployment, configuration, usage, and troubleshooting sufficiently for internal reproducibility
- P1 may use documented operator-provided secret/config inputs, but external model API keys must never be committed to the repo, hardcoded in manifests, or introduced through undocumented manual DB edits

---

## 8. Expected Code and Artifact Touch Points

Probable touch areas for P1:
- `deployments/k3s/clawmanager.yaml`
- `deployments/k8s/clawmanager.yaml` where truth must stay aligned
- `docs/k3s-local-setup.md`
- new docs under `docs/` for internal runbook, acceptance matrix, and release checklist
- backend or frontend files only when fresh-deploy or task-chain validation reveals a bounded real defect

The default rule is **minimum viable correction**. No broad refactor unless a defect cannot be fixed at a smaller boundary.
`k3s` is the primary validation target for P1, but every deploy-critical `k3s` truth change must be checked against `deployments/k8s/clawmanager.yaml` and recorded as aligned or deferred follow-up.

---

## 9. Risks

Primary risks:
- ARM runtime truth still depends on dev image rather than productized default
- model bootstrap may remain partially operator-driven if not designed carefully
- `skill-scanner` on local ARM may introduce new image/runtime blockers
- full `U3` task count may be larger than expected, increasing validation time
- deploy-critical drift may persist between `deployments/k3s/clawmanager.yaml` and `deployments/k8s/clawmanager.yaml`

Mitigation strategy:
- freeze scope early
- validate in packets
- keep evidence index continuously updated
- stop immediately when a new blocker invalidates the current packet assumption

---

## 10. Immediate Next Step

This spec has written user approval. The next step is to invoke `writing-plans` and produce an implementation plan that breaks P1 into executable packets and assigns bounded investigation or execution prompts for star-topology sub-agents.
