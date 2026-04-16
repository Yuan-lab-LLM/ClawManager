# R3-A-U3-F2-P1 Internal RC Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the current live-ready ClawManager state into a reproducible P1 internal release candidate from fresh K3S deploy, with frozen U3 scope, evidence-backed validation, and operator-grade docs.

**Architecture:** Keep the work layered and source-of-truth oriented. First freeze deploy preflight, ARM/model bootstrap, U3 inventory, and release evidence artifacts under one internal RC packet directory; then replay fresh deploy and validate only the frozen scope. Code changes are allowed only when a bounded packet reproduces a real defect and can point to the exact file boundary that must change.

**Tech Stack:** K3S/k3d, Kubernetes YAML, Go backend APIs, React admin/user portals, Markdown runbooks, curl/jq/kubectl for evidence capture.

---

## Planned Artifacts and Responsibilities

- `docs/superpowers/releases/2026-04-15-p1-internal-rc/README.md`
  - Canonical entrypoint for the internal RC packet.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/01-deploy-preflight.md`
  - Fresh-cluster assumptions, local resource envelope, operator inputs, and deploy-preflight truth.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/02-model-bootstrap.md`
  - Admin model bootstrap contract, secret rules, secure/non-secure requirements, and scanner-model prerequisites.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md`
  - Frozen, versioned U3 task-inventory artifact.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/04-acceptance-matrix.md`
  - One row per frozen U3 task plus gate coverage roll-up.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md`
  - PASS/BLOCKED state for Gate 1–5.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
  - Canonical evidence index for commands, API bodies, screenshots, logs, and doc sections.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/07-known-issues.md`
  - Explicit blockers, known issues, and deferred follow-ups.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/08-operator-runbook.md`
  - Operator-facing reproducible flow.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/09-user-quick-start.md`
  - Internal user flow for login, instance creation, and direct QA.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/10-troubleshooting.md`
  - Problem → check → fix matrix.
- `docs/superpowers/releases/2026-04-15-p1-internal-rc/11-release-checklist.md`
  - Final closure checklist.
- `docs/k3s-local-setup.md`
  - Must point to the new internal RC packet and remain aligned with the deploy truth.
- `deployments/k3s/clawmanager.yaml`
  - Primary deployment truth for P1.
- `deployments/k8s/clawmanager.yaml`
  - Must be checked for deploy-critical drift whenever `deployments/k3s/clawmanager.yaml` changes.
- `docs/superpowers/plans/2026-04-15-agent-control-base-url-fix.md`
  - Must be explicitly marked superseded so nobody follows the disproven `https://...:8443` route.
- `longterm/workspace/feature_list.json`
  - Final backlog truth update after P1 is actually verified.
- `longterm/workspace/claude-progress.txt`
  - Final session handoff update after P1 packet closure.

---

### Task 1: Seed the internal RC packet and supersede the disproven old plan

**Files:**
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/README.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/01-deploy-preflight.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/02-model-bootstrap.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/04-acceptance-matrix.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/07-known-issues.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/08-operator-runbook.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/09-user-quick-start.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/10-troubleshooting.md`
- Create: `docs/superpowers/releases/2026-04-15-p1-internal-rc/11-release-checklist.md`
- Modify: `docs/superpowers/plans/2026-04-15-agent-control-base-url-fix.md`
- Test: `rg --files docs/superpowers/releases/2026-04-15-p1-internal-rc docs/superpowers/plans`

- [ ] **Step 1: Create the release packet directory**

Run:

```bash
mkdir -p docs/superpowers/releases/2026-04-15-p1-internal-rc
```

Expected: the directory exists and is empty except for files created in the next steps.

- [ ] **Step 2: Seed the packet README with canonical artifact ownership**

Create `docs/superpowers/releases/2026-04-15-p1-internal-rc/README.md` with:

```md
# 2026-04-15 P1 Internal RC Packet

This directory is the canonical release packet for `R3-A-U3-F2-P1`.

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
```

- [ ] **Step 3: Seed the gate record and evidence index**

Create `05-gate-record.md` with:

```md
# Gate Record

| Gate | Name | Status | Acceptance Rows | Canonical Evidence | Notes |
| --- | --- | --- | --- | --- | --- |
| Gate 1 | Repeatable deploy | BLOCKED | 7.1 | `06-evidence-index.md` | |
| Gate 2 | Repeatable startup | BLOCKED | 7.2 | `06-evidence-index.md` | |
| Gate 3 | Capability completeness | BLOCKED | 7.3 | `06-evidence-index.md` | |
| Gate 4 | Operability | BLOCKED | 7.5 | `06-evidence-index.md` | |
| Gate 5 | Release closure | BLOCKED | 7.4, 7.5 | `06-evidence-index.md` | |
```

Create `06-evidence-index.md` with:

```md
# Evidence Index

| Scope | Claim / Packet | Artifact Path | Evidence Type | Status | Notes |
| --- | --- | --- | --- | --- | --- |
| Gate 1 | Seed packet structure | `docs/superpowers/releases/2026-04-15-p1-internal-rc/README.md` | doc | PASS | |
```

- [ ] **Step 4: Seed the remaining files with one-line ownership headers**

Create each remaining file with exactly these first lines:

```md
# Deploy Preflight
```

```md
# Model Bootstrap
```

```md
# U3 Task Inventory
```

```md
# Acceptance Matrix
```

```md
# Known Issues
```

```md
# Operator Runbook
```

```md
# User Quick Start
```

```md
# Troubleshooting
```

```md
# Release Checklist
```

- [ ] **Step 5: Mark the old URL-builder plan as superseded**

Prepend this note to `docs/superpowers/plans/2026-04-15-agent-control-base-url-fix.md`:

```md
> Superseded by `docs/superpowers/specs/2026-04-15-p1-internal-release-design.md` and `docs/superpowers/plans/2026-04-15-r3-a-u3-f2-p1-internal-rc.md`.
> Do not execute this plan: the `https://...:8443` URL-builder hypothesis was disproven by the `9001 -> 9001` gateway-service evidence chain.

```

- [ ] **Step 6: Verify the packet file tree**

Run:

```bash
rg --files docs/superpowers/releases/2026-04-15-p1-internal-rc docs/superpowers/plans | sort
```

Expected: the new release packet files exist and the outdated plan still exists with the superseded note.

- [ ] **Step 7: Commit**

Run:

```bash
git add docs/superpowers/releases/2026-04-15-p1-internal-rc docs/superpowers/plans/2026-04-15-agent-control-base-url-fix.md
git commit -m "docs: seed p1 internal rc packet"
```

Expected: one docs-only commit.

### Task 2: Freeze L1 deploy-preflight truth and align the K3S entrypoint docs

**Files:**
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/01-deploy-preflight.md`
- Modify: `docs/k3s-local-setup.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
- Test: `kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml`

- [ ] **Step 1: Write the deploy-preflight artifact**

Replace `01-deploy-preflight.md` with:

```md
# Deploy Preflight

## Fresh-Cluster Rule
- Start from an empty `k3d` cluster for P1 replay.
- Do not reuse prior `clawmanager-system` or `clawmanager-user-*` namespaces.
- Do not rely on residual MySQL data, residual PVCs, or prior admin model records.

## Source of Truth
- Primary deploy truth: `deployments/k3s/clawmanager.yaml`
- Supporting operator guide: `docs/k3s-local-setup.md`
- Deploy-critical drift check target: `deployments/k8s/clawmanager.yaml`

## Local Resource Envelope
| Item | P1 minimum |
| --- | --- |
| First OpenClaw validation instance | `memory_gb=3` |
| Initial OpenClaw runtime image | `ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434` |
| Gateway control path | `http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001` |

## Required Operator Inputs
| Input | How supplied | Repo rule |
| --- | --- | --- |
| External model base URL | operator env / admin UI / admin API | never hardcode |
| External model API key | operator env / admin UI / admin API | never commit |
| Secure model selection | operator model record | never assume live DB carry-over |

## Prohibited Shortcuts
- no repo-external oral steps
- no undocumented manual DB edits
- no hidden live-state reuse
```

- [ ] **Step 2: Link the K3S setup doc to the release packet**

Add one short section near the top of `docs/k3s-local-setup.md`:

```md
## P1 Internal RC Packet

For `R3-A-U3-F2-P1`, use `docs/superpowers/releases/2026-04-15-p1-internal-rc/` as the canonical packet for deploy-preflight, model bootstrap, frozen U3 scope, evidence index, and release closure.
```

Also add one line near the existing `9001 -> 9001` explanation:

```md
Do not replace this path with `https://...:8443` URL-builder changes unless new evidence disproves the validated HTTP `clawmanager-gateway:9001` registration path.
```

- [ ] **Step 3: Dry-run the K3S manifest**

Run:

```bash
kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml >/tmp/p1-k3s-dryrun.txt
```

Expected: command exits `0` and `/tmp/p1-k3s-dryrun.txt` contains Kubernetes objects with no schema errors.

- [ ] **Step 4: Record Gate 1 preflight evidence**

Append one row to `06-evidence-index.md`:

```md
| Gate 1 | K3S manifest client dry-run | `/tmp/p1-k3s-dryrun.txt` | command output | PASS | deploy-preflight syntax check |
```

Update the `Gate 1` row in `05-gate-record.md` notes to mention `01-deploy-preflight.md` and `/tmp/p1-k3s-dryrun.txt`.

- [ ] **Step 5: Commit**

Run:

```bash
git add docs/superpowers/releases/2026-04-15-p1-internal-rc/01-deploy-preflight.md docs/k3s-local-setup.md docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md
git commit -m "docs: freeze p1 deploy preflight"
```

Expected: one docs-only commit.

### Task 3: Freeze the ARM runtime bootstrap and model bootstrap contract

**Files:**
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/02-model-bootstrap.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/08-operator-runbook.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/09-user-quick-start.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/10-troubleshooting.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
- Test: `curl` calls against `/api/v1/auth/login`, `/api/v1/system-settings/images`, `/api/v1/admin/models`

- [ ] **Step 1: Document the admin login and ARM runtime image bootstrap flow**

Put this content into `02-model-bootstrap.md`:

````md
# Model Bootstrap

## Admin Session Bootstrap
```bash
export CM_API="https://localhost:30443/api/v1"
export CM_ADMIN_TOKEN="$(curl -sk -X POST "$CM_API/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.data.access_token')"
test -n "$CM_ADMIN_TOKEN"
```

## ARM Runtime Image Bootstrap
```bash
curl -sk -X PUT "$CM_API/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "instance_type":"openclaw",
    "display_name":"OpenClaw ARM Dev Bootstrap",
    "image":"ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434"
  }'
```

## Required Model Records
- one active non-secure model
- one active secure model
- both supplied by operator-provided values, not repo defaults

## Example Model Upsert
```bash
export LLM_BASE_URL="https://example.invalid/v1"
export LLM_MODEL="example-model"
export LLM_API_KEY="replace-at-runtime"

curl -sk -X PUT "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
    \"display_name\":\"P1 normal model\",
    \"provider_type\":\"openai-compatible\",
    \"protocol_type\":\"openai-compatible\",
    \"base_url\":\"${LLM_BASE_URL}\",
    \"provider_model_name\":\"${LLM_MODEL}\",
    \"api_key\":\"${LLM_API_KEY}\",
    \"is_secure\":false,
    \"is_active\":true,
    \"input_price\":0,
    \"output_price\":0,
    \"currency\":\"USD\"
  }"
```

Repeat the same request with `"display_name":"P1 secure model"` and `"is_secure":true`.

## Hard Rules
- never commit provider keys
- never hardcode provider keys in manifests
- never rely on manual DB edits
- if direct QA fails only because model records or provider credentials are missing, classify it as a model-bootstrap blocker
````

- [ ] **Step 2: Add the same bootstrap sequence to the operator runbook**

Append this checklist to `08-operator-runbook.md`:

```md
## Runtime and Model Bootstrap Checklist
- log in as admin through `/api/v1/auth/login`
- set the `openclaw` system image to the documented ARM dev bootstrap image
- create one active normal model and one active secure model through `/api/v1/admin/models`
- verify `/api/v1/admin/models` returns both records before any direct QA claim
```

- [ ] **Step 3: Add the user-facing direct-QA prerequisite note**

Append this note to `09-user-quick-start.md`:

```md
## Before the first direct QA
- operator must confirm the ARM runtime image bootstrap is in place
- operator must confirm one active normal model and one active secure model exist
```

Add this troubleshooting row to `10-troubleshooting.md`:

```md
| Direct QA fails but runtime is healthy | `curl -sk "$CM_API/admin/models" -H "Authorization: Bearer $CM_ADMIN_TOKEN"` | missing active normal model, missing secure model, or bad provider credential | classify as model-bootstrap blocker, not runtime blocker |
```

- [ ] **Step 4: Verify the bootstrap APIs respond**

Run:

```bash
curl -sk "$CM_API/system-settings/images" -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data.items'
curl -sk "$CM_API/admin/models" -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data.items'
```

Expected: both commands return JSON arrays; the models array is allowed to be empty before bootstrap, but the endpoint itself must be reachable.

- [ ] **Step 5: Record the bootstrap API evidence**

Append rows like these to `06-evidence-index.md`:

```md
| Gate 2 | system image settings API reachable | `curl -sk "$CM_API/system-settings/images" ...` | API body | PASS | ARM runtime bootstrap path exists |
| Gate 2 | admin models API reachable | `curl -sk "$CM_API/admin/models" ...` | API body | PASS | model bootstrap path exists |
```

- [ ] **Step 6: Commit**

Run:

```bash
git add docs/superpowers/releases/2026-04-15-p1-internal-rc/02-model-bootstrap.md docs/superpowers/releases/2026-04-15-p1-internal-rc/08-operator-runbook.md docs/superpowers/releases/2026-04-15-p1-internal-rc/09-user-quick-start.md docs/superpowers/releases/2026-04-15-p1-internal-rc/10-troubleshooting.md docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md
git commit -m "docs: freeze p1 bootstrap contract"
```

Expected: one docs-only commit.

### Task 4: Freeze the U3 inventory artifact and seed the acceptance matrix

**Files:**
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/04-acceptance-matrix.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
- Test: frozen-inventory file contains one row header and one source policy section

- [ ] **Step 1: Replace the U3 inventory file with a frozen-artifact template**

Put this content into `03-u3-task-inventory.md`:

```md
# U3 Task Inventory

Version: `u3-v1`

## Source Policy
- Source A: selected validation instance-reported skills inventory snapshot
- Source B: final FastSkill / skill-scanner discovery snapshot
- Canonical Source A collection path: `GET /api/v1/instances/:id/skills`
- Runtime `/skills` may be used as operator cross-check only; it is not canonical Source A.
- No task may enter `L3` unless it is recorded in the table below.
- If a new task appears after freeze, record it in `07-known-issues.md` as deferred scope unless Commander explicitly re-freezes `U3`.

## Frozen Rows
| Row | Source | Snapshot Method | Task Label | Validation Packet | Status | Evidence Path | Notes |
| --- | --- | --- | --- | --- | --- | --- | --- |
```

- [ ] **Step 2: Seed the acceptance matrix from the frozen artifact**

Put this content into `04-acceptance-matrix.md`:

```md
# Acceptance Matrix

| Scope | Row / Gate | Minimal Success Path | Status | Evidence Path | Notes |
| --- | --- | --- | --- | --- | --- |
| Gate | Gate 1 | fresh deploy from empty cluster | BLOCKED | `06-evidence-index.md` | |
| Gate | Gate 2 | runtime online + direct QA | BLOCKED | `06-evidence-index.md` | |
| Gate | Gate 3 | all frozen U3 rows validated | BLOCKED | `06-evidence-index.md` | |
| Gate | Gate 4 | runbook + quick-start + troubleshooting verified | BLOCKED | `06-evidence-index.md` | |
| Gate | Gate 5 | evidence index + gate record + checklist complete | BLOCKED | `06-evidence-index.md` | |
```

- [ ] **Step 3: Capture the selected validation instance skills snapshot into the inventory**

Do one bounded manual packet:
- collect the canonical Source A snapshot from `GET /api/v1/instances/:id/skills` for one selected validation instance
- transcribe each exact skill/task label returned by the selected validation instance skill inventory into `03-u3-task-inventory.md` with `Source = instance-reported-skills-inventory`
- set `Snapshot Method = GET /api/v1/instances/:id/skills`
- if runtime `/skills` is checked, record it only as operator cross-check and do not treat it as canonical Source A
- leave `Validation Packet`, `Status`, and `Evidence Path` blank until `L3`

No paraphrasing: copy the exact skill/task labels exactly as returned by the selected validation instance skill inventory.

- [ ] **Step 4: Record the frozen-inventory discipline in the gate record**

Append this note to the `Gate 3` row in `05-gate-record.md`:

```md
Gate 3 cannot start until `03-u3-task-inventory.md` is frozen and versioned.
```

Append this row to `06-evidence-index.md`:

```md
| Gate 3 | frozen U3 inventory seeded | `docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md` | doc | PASS | versioned task scope artifact |
```

- [ ] **Step 5: Verify the artifact skeletons**

Run:

```bash
rg -n "Version: `u3-v1`|Source Policy|Frozen Rows|Minimal Success Path" docs/superpowers/releases/2026-04-15-p1-internal-rc/{03-u3-task-inventory.md,04-acceptance-matrix.md}
```

Expected: all four markers are found.

- [ ] **Step 6: Commit**

Run:

```bash
git add docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md docs/superpowers/releases/2026-04-15-p1-internal-rc/04-acceptance-matrix.md docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md
git commit -m "docs: freeze u3 inventory artifact"
```

Expected: one docs-only commit.

### Task 5: Bring up the skill-scanner validation path or record a Gate 3 blocker

**Files:**
- Modify: `deployments/k3s/clawmanager.yaml`
- Modify: `docs/k3s-local-setup.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/07-known-issues.md`
- Test: `kubectl get deploy,pod -n clawmanager-system | rg 'skill-scanner|clawmanager-app'`

- [ ] **Step 1: Enable the scanner path in the K3S manifest for P1 validation**

In `deployments/k3s/clawmanager.yaml`, change:

```yaml
- name: SKILL_SCANNER_ENABLED
  value: "false"
```

to:

```yaml
- name: SKILL_SCANNER_ENABLED
  value: "true"
```

And change the `skill-scanner` Deployment replicas from:

```yaml
replicas: 0
```

to:

```yaml
replicas: 1
```

- [ ] **Step 2: Update the K3S guide to match the validation path**

Replace the current “skill-scanner 默认关闭” wording in `docs/k3s-local-setup.md` with a P1 note:

```md
For `R3-A-U3-F2-P1`, `skill-scanner` must be explicitly enabled and verified before any full `U3` validation claim.
```

- [ ] **Step 3: Apply the manifest and watch scanner status**

Run:

```bash
kubectl apply -f deployments/k3s/clawmanager.yaml >/tmp/p1-k3s-apply.txt
kubectl rollout status deployment/clawmanager-app -n clawmanager-system --timeout=180s
kubectl rollout status deployment/skill-scanner -n clawmanager-system --timeout=180s
kubectl get deploy,pod -n clawmanager-system | rg 'skill-scanner|clawmanager-app'
```

Expected: both deployments become available. If `skill-scanner` fails to roll out or crashes on ARM, stop here and record a Gate 3 blocker instead of continuing to `L3`.

- [ ] **Step 4: Capture scanner config and discovery evidence**

Run:

```bash
curl -sk "$CM_API/admin/security/config" -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data'
curl -sk "$CM_API/admin/skills" -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data'
```

Expected: `/admin/security/config` reports scanner connectivity, and `/admin/skills` returns the current skill catalog snapshot.

- [ ] **Step 5: Merge the discovery snapshot into the frozen U3 artifact**

For each task or capability surfaced by the validated FastSkill / `skill-scanner` path, append a row to `03-u3-task-inventory.md` with:
- `Source = skill-scanner`
- `Snapshot Method = /api/v1/admin/security/config + /api/v1/admin/skills + manual UI confirmation`
- exact task label as shown to the operator

If the scanner never becomes usable, append this row to `07-known-issues.md` instead:

```md
| Gate 3 blocker | skill-scanner unavailable on local ARM validation path | `06-evidence-index.md` | P1 cannot claim full U3 validation until scanner capability is restored |
```

- [ ] **Step 6: Record evidence or blocker**

Append either PASS rows or BLOCKED rows to `06-evidence-index.md`, for example:

```md
| Gate 3 | scanner config reachable | `curl -sk "$CM_API/admin/security/config" ...` | API body | PASS | scanner-on validation path |
| Gate 3 | scanner rollout failed | `kubectl rollout status deployment/skill-scanner ...` | command output | BLOCKED | stop before L3 |
```

- [ ] **Step 7: Commit**

Run:

```bash
git add deployments/k3s/clawmanager.yaml docs/k3s-local-setup.md docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md docs/superpowers/releases/2026-04-15-p1-internal-rc/07-known-issues.md
git commit -m "feat: enable scanner path for p1 validation"
```

Expected: one manifest/docs commit. If this task ended in blocker state, still commit the blocker documentation separately.

### Task 6: Replay fresh deploy from empty cluster and capture Gate 1 / Gate 2 evidence

**Files:**
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/04-acceptance-matrix.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/08-operator-runbook.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/09-user-quick-start.md`
- Test: fresh cluster creation, deploy apply, login, runtime status polling, one direct-QA proof

- [ ] **Step 1: Reset the local cluster**

Run:

```bash
k3d cluster delete clawmanager || true
k3d cluster create clawmanager --port "30443:30443@loadbalancer"
kubectl cluster-info
```

Expected: a new `clawmanager` cluster exists and `kubectl cluster-info` succeeds.

- [ ] **Step 2: Deploy from repository truth**

Run:

```bash
kubectl apply -f deployments/k3s/clawmanager.yaml >/tmp/p1-fresh-apply.txt
kubectl rollout status deployment/clawmanager-app -n clawmanager-system --timeout=180s
kubectl get pods -n clawmanager-system >/tmp/p1-system-pods.txt
```

Expected: `clawmanager-app` is available and the system namespace pods are created without manual patches.

- [ ] **Step 3: Bootstrap runtime image, models, user, and quota**

Run:

```bash
export CM_API="https://localhost:30443/api/v1"
export CM_ADMIN_TOKEN="$(curl -sk -X POST "$CM_API/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.data.access_token')"

curl -sk -X PUT "$CM_API/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "instance_type":"openclaw",
    "display_name":"OpenClaw ARM Dev Bootstrap",
    "image":"ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434"
  }'

curl -sk -X POST "$CM_API/users" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "username":"cmup1rc01",
    "email":"cmup1rc01@example.com",
    "password":"PktUser123!",
    "role":"user"
  }' >/tmp/p1-user-create.json

export P1_USER_ID="$(jq -r '.data.id' /tmp/p1-user-create.json)"

curl -sk -X PUT "$CM_API/users/$P1_USER_ID/quota" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "max_instances":2,
    "max_cpu_cores":8,
    "max_memory_gb":16,
    "max_storage_gb":100,
    "max_gpu_count":0
  }'
```

Expected: system image bootstrap is saved, the user is created, and quota update returns success.

- [ ] **Step 4: Create the first validation instance as the user**

Run:

```bash
export CM_USER_TOKEN="$(curl -sk -X POST "$CM_API/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"cmup1rc01","password":"PktUser123!"}' | jq -r '.data.access_token')"

curl -sk -X POST "$CM_API/instances" \
  -H "Authorization: Bearer $CM_USER_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"p1-arm64-qa",
    "type":"openclaw",
    "cpu_cores":2,
    "memory_gb":3,
    "disk_gb":20,
    "gpu_enabled":false,
    "gpu_count":0,
    "os_type":"openclaw",
    "os_version":"latest",
    "storage_class":""
  }' >/tmp/p1-instance-create.json

export P1_INSTANCE_ID="$(jq -r '.data.id' /tmp/p1-instance-create.json)"
```

Expected: `P1_INSTANCE_ID` is a non-empty integer and the create call returns `201`.

- [ ] **Step 5: Poll until runtime acceptance is either proven or blocked**

Run:

```bash
for i in $(seq 1 18); do
  curl -sk "$CM_API/instances/$P1_INSTANCE_ID/runtime" \
    -H "Authorization: Bearer $CM_USER_TOKEN" | jq '.data' | tee "/tmp/p1-runtime-$i.json"
  sleep 10
done
```

Expected: one poll captures `status=running`, `infra_status=ready`, `runtime.agent_status=online`, and `runtime.openclaw_status=running`. If the loop ends without that state chain, stop and record a Gate 2 blocker.

- [ ] **Step 6: Capture one direct-QA proof**

Run:

```bash
curl -sk -X POST "$CM_API/instances/$P1_INSTANCE_ID/access" \
  -H "Authorization: Bearer $CM_USER_TOKEN" | tee /tmp/p1-access.json
```

Then:
- open the `access_url` from `/tmp/p1-access.json`
- send one short prompt such as `ping`
- capture a screenshot showing the prompt and a successful reply

Expected: one screenshot proves a direct QA reply. If runtime is healthy but the reply fails because models are missing, stop and classify it as a model-bootstrap blocker.

- [ ] **Step 7: Update the matrix, gate record, and evidence index**

Add concrete rows to `04-acceptance-matrix.md`, `05-gate-record.md`, and `06-evidence-index.md` for:
- cluster reset
- fresh deploy apply
- user creation and quota update
- instance create response
- runtime acceptance poll
- direct QA screenshot

Set `Gate 1` and `Gate 2` to `PASS` only if every required row has evidence.

- [ ] **Step 8: Commit**

Run:

```bash
git add docs/superpowers/releases/2026-04-15-p1-internal-rc/{04-acceptance-matrix.md,05-gate-record.md,06-evidence-index.md,08-operator-runbook.md,09-user-quick-start.md}
git commit -m "docs: record fresh deploy replay evidence"
```

Expected: one docs-only commit after evidence capture.

### Task 7: Validate every frozen U3 row and route any failures into explicit blocker rows

**Files:**
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/03-u3-task-inventory.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/04-acceptance-matrix.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/07-known-issues.md`
- Test: one validation packet per frozen row

- [ ] **Step 1: Expand the matrix so each frozen U3 row has one validation row**

For each row in `03-u3-task-inventory.md`, append one row to `04-acceptance-matrix.md`:

```md
| U3 | `<copy task label exactly>` | one minimal successful path | BLOCKED | pending evidence | |
```

Do not invent new task labels outside `03-u3-task-inventory.md`.

- [ ] **Step 2: Execute one bounded validation packet per frozen row**

For each frozen row, run one packet with this shape:

```text
Validate exactly one frozen U3 row.
Goal: prove one minimal successful path or produce one explicit blocker.
Do not validate any other row.
Output: PASS/BLOCKED, exact artifact paths, and next-boundary note.
```

Record each packet’s artifacts under `/tmp/` or the release packet docs and cite them in `06-evidence-index.md`.

- [ ] **Step 3: Update the task inventory and matrix immediately after each row**

For every row:
- set `Status = PASS` or `BLOCKED` in `03-u3-task-inventory.md`
- set the matching `04-acceptance-matrix.md` row to the same status
- fill `Evidence Path` with the canonical artifact path

- [ ] **Step 4: Route any incomplete row into known issues instead of hiding it**

For each failed or partial row, append a line to `07-known-issues.md`:

```md
| U3 row blocker | `<task label>` | `<artifact path>` | `<impact>` | `<next packet or P2 defer>` |
```

- [ ] **Step 5: Mark Gate 3 only after all frozen rows are resolved**

Set `Gate 3` in `05-gate-record.md` to:
- `PASS` only if every frozen `U3` row is `PASS`
- `BLOCKED` if any row remains `BLOCKED` or deferred

Append a summary row to `06-evidence-index.md`:

```md
| Gate 3 | frozen U3 validation summary | `docs/superpowers/releases/2026-04-15-p1-internal-rc/04-acceptance-matrix.md` | doc | PASS/BLOCKED | mirrors frozen rows only |
```

- [ ] **Step 6: Commit**

Run:

```bash
git add docs/superpowers/releases/2026-04-15-p1-internal-rc/{03-u3-task-inventory.md,04-acceptance-matrix.md,05-gate-record.md,06-evidence-index.md,07-known-issues.md}
git commit -m "docs: record frozen u3 validation"
```

Expected: one docs-only commit after the frozen-scope validation pass.

### Task 8: Close the internal RC packet and update long-term truth

**Files:**
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/05-gate-record.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/06-evidence-index.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/07-known-issues.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/08-operator-runbook.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/09-user-quick-start.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/10-troubleshooting.md`
- Modify: `docs/superpowers/releases/2026-04-15-p1-internal-rc/11-release-checklist.md`
- Modify: `longterm/workspace/feature_list.json`
- Modify: `longterm/workspace/claude-progress.txt`
- Test: final packet grep for unresolved states

- [ ] **Step 1: Finalize the release checklist**

Replace `11-release-checklist.md` with:

```md
# Release Checklist

- [ ] Gate 1 marked PASS/BLOCKED in `05-gate-record.md`
- [ ] Gate 2 marked PASS/BLOCKED in `05-gate-record.md`
- [ ] Gate 3 marked PASS/BLOCKED in `05-gate-record.md`
- [ ] Gate 4 marked PASS/BLOCKED in `05-gate-record.md`
- [ ] Gate 5 marked PASS/BLOCKED in `05-gate-record.md`
- [ ] every frozen U3 row has a final status
- [ ] every blocker / known issue / deferred follow-up has an artifact path
- [ ] operator runbook references only verified paths
- [ ] user quick-start references only verified paths
- [ ] troubleshooting references only verified paths
```

- [ ] **Step 2: Close Gate 4 and Gate 5 using the packet artifacts**

Update:
- `05-gate-record.md`
- `06-evidence-index.md`

Set:
- `Gate 4 = PASS` only if the runbook, quick-start, and troubleshooting docs point to verified paths
- `Gate 5 = PASS` only if the checklist, gate record, evidence index, and known-issues file are complete

- [ ] **Step 3: Search for unresolved placeholders or silent gaps**

Run:

```bash
rg -n "TBD|TODO|FIXME|pending evidence|PASS/BLOCKED|oral|placeholder" docs/superpowers/releases/2026-04-15-p1-internal-rc longterm/workspace
```

Expected:
- no `TBD`, `TODO`, `FIXME`, or `placeholder`
- remaining `pending evidence` or `PASS/BLOCKED` text is allowed only if the item is intentionally unresolved and already called out in `07-known-issues.md`

- [ ] **Step 4: Update backlog truth and session memory**

Edit `longterm/workspace/feature_list.json`:
- update `F-002` evidence notes to point to the internal RC packet directory
- set `passes: true` only if Gate 1–5 are all `PASS`
- otherwise keep `passes: false` and point to the blocking rows in `07-known-issues.md`

Append to `longterm/workspace/claude-progress.txt`:

```text
2026-04-15: P1 internal RC packet updated at docs/superpowers/releases/2026-04-15-p1-internal-rc/.
Final gate state: <copy from 05-gate-record.md>.
Blocking rows or deferred follow-ups: <copy exact rows from 07-known-issues.md if any>.
```

- [ ] **Step 5: Commit**

Run:

```bash
git add docs/superpowers/releases/2026-04-15-p1-internal-rc longterm/workspace/feature_list.json longterm/workspace/claude-progress.txt
git commit -m "docs: close p1 internal rc packet"
```

Expected: one final closure commit.

---

## Self-Review

- Spec coverage:
  - `L1` maps to Tasks 2, 3, and 6.
  - `L2` maps to Tasks 4 and 5.
  - `L3` maps to Task 7.
  - `L4` maps to Tasks 1, 2, 3, and 8.
- Placeholder scan:
  - No `TBD`, `TODO`, or “implement later” markers are present in the task instructions.
- Type and route consistency:
  - auth route: `/api/v1/auth/login`
  - model routes: `/api/v1/admin/models`
  - scanner routes: `/api/v1/admin/security/config`, `/api/v1/admin/skills`
  - image route: `/api/v1/system-settings/images`
  - instance routes: `/api/v1/instances`, `/api/v1/instances/:id/runtime`, `/api/v1/instances/:id/access`
