# A1 Formal Release Evidence Packet Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Produce one evidence-backed A1 formal-release packet that proves the frozen bootstrap contract, one single OpenClaw instance reaches runtime-ready state, and one direct-QA proof exists without reopening deferred scope.

**Architecture:** Keep this packet strictly evidence-first and A1-owned. First align the A1 docs to the real `/instances/:id/status` plus `/instances/:id/runtime` API boundary and seed A1 evidence docs; then execute the frozen bootstrap and single-instance acceptance commands from the A1 packet; finally record PASS/BLOCKED honestly in A1 release docs and session handoff. Do not touch scanner/FastSkill/U3, `https://...:8443`, or broad backend/frontend logic.

**Tech Stack:** Markdown release packet docs, existing ClawManager admin APIs, K3S/k3d, `kubectl`, `curl`, `jq`, `/tmp` evidence artifacts, browser-based screenshot capture for direct QA.

---

## Planned Artifacts and Responsibilities

- `docs/superpowers/releases/2026-04-16-a1-core-release/README.md`
  - Canonical A1 packet entrypoint and artifact index.
- `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md`
  - Canonical single-instance acceptance contract; must match the real API split between `/status` and `/runtime`.
- `docs/superpowers/releases/2026-04-16-a1-core-release/05-evidence-index.md`
  - Canonical evidence index for the A1 formal-release packet.
- `docs/superpowers/releases/2026-04-16-a1-core-release/06-gate-record.md`
  - PASS/BLOCKED roll-up for the A1 gates.
- `docs/k3s-local-setup.md`
  - Operator tutorial that must keep pointing to the A1-owned bootstrap and acceptance docs.
- `longterm/workspace/claude-progress.txt`
  - Session handoff and packet-close summary.
- `/tmp/a1-*.json`, `/tmp/a1-*.txt`, `/tmp/a1-direct-qa-proof.png`
  - Ephemeral evidence artifacts captured during the packet.

## Non-Goals

- Do not modify `deployments/k3s/clawmanager.yaml` in this packet.
- Do not rerun broad deploy validation; keep using `/tmp/a1-core-release-dryrun.txt` unless the manifest changes.
- Do not change `feature_list.json` in this packet; its broader truth is still multi-instance oriented and outside this A1 closure step.
- Do not reopen scanner/FastSkill/U3, `/api/v1/admin/skills`, or `https://...:8443`.
- Do not commit provider keys or write them into docs, manifests, or prompts.

---

### Task 1: Align the A1 packet to the real status/runtime API boundary and seed evidence docs

**Files:**
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/README.md`
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md`
- Create: `docs/superpowers/releases/2026-04-16-a1-core-release/05-evidence-index.md`
- Create: `docs/superpowers/releases/2026-04-16-a1-core-release/06-gate-record.md`
- Test: `rg -n "05-evidence-index|06-gate-record|status=running|infra_status=ready" docs/superpowers/releases/2026-04-16-a1-core-release`

- [ ] **Step 1: Fix the acceptance path so it polls both `/status` and `/runtime`**

Update `docs/superpowers/releases/2026-04-16-a1-core-release/04-acceptance-path.md` so the runtime-poll section becomes:

~~~md
## Poll Until Runtime Ready

```bash
for i in $(seq 1 18); do
  curl -sk "$CM_API/instances/$A1_INSTANCE_ID/status" \
    -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data' | tee "/tmp/a1-status-$i.json"
  curl -sk "$CM_API/instances/$A1_INSTANCE_ID/runtime" \
    -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data' | tee "/tmp/a1-runtime-$i.json"
  sleep 10
done
```

Expected: one poll pair shows all of these at the same time:

- `instance_status.status=running`
- `runtime.infra_status=ready`
- `runtime.agent_status=online`
- `runtime.openclaw_status=running`
~~~

Expected: the A1 acceptance doc no longer asks `/runtime` to return `instance status`.

- [ ] **Step 2: Seed the A1 evidence index**

Create `docs/superpowers/releases/2026-04-16-a1-core-release/05-evidence-index.md` with:

```md
# A1 Evidence Index

| Gate | Evidence Item | Artifact Path | Type | Status | Notes |
| --- | --- | --- | --- | --- | --- |
| Gate 1 | Existing manifest syntax check | `/tmp/a1-core-release-dryrun.txt` | command output | PASS | carried forward because this packet does not change `deployments/k3s/clawmanager.yaml` |
| Gate 1 | Health check | `/tmp/a1-healthz.txt` | command output | PENDING | |
| Gate 1 | Admin login | `/tmp/a1-admin-login.json` | API body | PENDING | |
| Gate 1 | ARM runtime image bootstrap | `/tmp/a1-image-bootstrap.json` | API body | PENDING | |
| Gate 1 | ARM runtime image verification | `/tmp/a1-image-settings.json` | API body | PENDING | |
| Gate 1 | Normal model upsert | `/tmp/a1-model-upsert-normal.json` | API body | PENDING | |
| Gate 1 | Secure model upsert | `/tmp/a1-model-upsert-secure.json` | API body | PENDING | |
| Gate 1 | Model verification | `/tmp/a1-models.json` | API body | PENDING | |
| Gate 2 | Single instance create | `/tmp/a1-instance-create.json` | API body | PENDING | |
| Gate 2 | Instance status poll | `/tmp/a1-status-*.json` | API body | PENDING | |
| Gate 2 | Runtime status poll | `/tmp/a1-runtime-*.json` | API body | PENDING | |
| Gate 3 | Instance access token | `/tmp/a1-access.json` | API body | PENDING | |
| Gate 3 | Direct QA proof | `/tmp/a1-direct-qa-proof.png` | screenshot | PENDING | |
```

Expected: the A1 packet has one canonical place to cite every evidence artifact.

- [ ] **Step 3: Seed the A1 gate record**

Create `docs/superpowers/releases/2026-04-16-a1-core-release/06-gate-record.md` with:

```md
# A1 Gate Record

| Gate | Name | Status | Canonical Evidence | Notes |
| --- | --- | --- | --- | --- |
| Gate 1 | Bootstrap contract satisfied | BLOCKED | `05-evidence-index.md` | requires healthz, admin login, ARM runtime image bootstrap, and normal + secure model verification |
| Gate 2 | Single-instance runtime ready | BLOCKED | `05-evidence-index.md` | requires one instance with `instance_status.status=running`, `runtime.infra_status=ready`, `runtime.agent_status=online`, and `runtime.openclaw_status=running` |
| Gate 3 | Direct QA proof captured | BLOCKED | `05-evidence-index.md` | requires one access artifact plus one direct-QA proof |
| Gate 4 | A1 packet closeout | BLOCKED | `05-evidence-index.md`; `longterm/workspace/claude-progress.txt` | requires gate verdicts and handoff update |
```

Expected: the A1 packet has a single PASS/BLOCKED roll-up file.

- [ ] **Step 4: Link the new evidence docs from the A1 README**

Update the `## Canonical Artifacts` section in `docs/superpowers/releases/2026-04-16-a1-core-release/README.md` to include:

```md
- `05-evidence-index.md`: formal-release evidence inventory
- `06-gate-record.md`: A1 PASS/BLOCKED roll-up
```

Expected: the A1 README lists all six packet files in order.

- [ ] **Step 5: Verify the packet now contains the expected evidence docs**

Run:

```bash
rg -n "05-evidence-index|06-gate-record|instance_status.status=running|runtime.infra_status=ready" \
  docs/superpowers/releases/2026-04-16-a1-core-release
```

Expected: the command prints matches in `README.md`, `04-acceptance-path.md`, `05-evidence-index.md`, and `06-gate-record.md`.

---

### Task 2: Capture Gate 1 bootstrap-contract evidence without changing deploy truth

**Files:**
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/05-evidence-index.md`
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/06-gate-record.md`
- Test: `curl`; `jq`

- [ ] **Step 1: Re-check healthz and capture the result**

Run:

```bash
curl -sk "$CM_BASE_URL/healthz" | tee /tmp/a1-healthz.txt
```

Expected: `/tmp/a1-healthz.txt` contains `ok`.

- [ ] **Step 2: Log in as admin and capture the login body**

Run:

```bash
curl -sk -X POST "$CM_API/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | tee /tmp/a1-admin-login.json

export CM_ADMIN_TOKEN="$(jq -r '.data.access_token' /tmp/a1-admin-login.json)"
test -n "$CM_ADMIN_TOKEN"
```

Expected: `/tmp/a1-admin-login.json` contains a non-empty `.data.access_token`.

- [ ] **Step 3: Apply the frozen ARM runtime image bootstrap**

Run:

```bash
curl -sk -X PUT "$CM_API/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "instance_type":"openclaw",
    "display_name":"OpenClaw ARM Dev Bootstrap",
    "image":"ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434"
  }' | tee /tmp/a1-image-bootstrap.json
```

Expected: `/tmp/a1-image-bootstrap.json` shows the frozen image reference in the success body.

- [ ] **Step 4: Verify the active OpenClaw system image**

Run:

```bash
curl -sk "$CM_API/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | tee /tmp/a1-image-settings.json

jq '.data.items[] | select(.instance_type=="openclaw") | .image' /tmp/a1-image-settings.json
```

Expected: the printed image is exactly `ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434`.

- [ ] **Step 5: Upsert one active normal model and one active secure model**

Run:

```bash
curl -sk -X PUT "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
    \"display_name\":\"A1 normal model\",
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
  }" | tee /tmp/a1-model-upsert-normal.json

curl -sk -X PUT "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
    \"display_name\":\"A1 secure model\",
    \"provider_type\":\"openai-compatible\",
    \"protocol_type\":\"openai-compatible\",
    \"base_url\":\"${LLM_BASE_URL}\",
    \"provider_model_name\":\"${LLM_MODEL}\",
    \"api_key\":\"${LLM_API_KEY}\",
    \"is_secure\":true,
    \"is_active\":true,
    \"input_price\":0,
    \"output_price\":0,
    \"currency\":\"USD\"
  }" | tee /tmp/a1-model-upsert-secure.json
```

Expected: both files return success bodies instead of validation errors.

- [ ] **Step 6: Verify the active normal and secure model records**

Run:

```bash
curl -sk "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | tee /tmp/a1-models.json

jq '.data.items | map({display_name,is_secure,is_active,provider_model_name,base_url})' /tmp/a1-models.json
```

Expected: the array contains one active normal model and one active secure model.

- [ ] **Step 7: Update Gate 1 rows and verdict**

Update `05-evidence-index.md` so the Gate 1 rows move from `PENDING` to `PASS` or `BLOCKED` with exact notes. Then update `06-gate-record.md`:

```md
| Gate 1 | Bootstrap contract satisfied | PASS | `05-evidence-index.md` | healthz, admin login, ARM runtime image bootstrap, and active normal + secure model records all have artifacts |
```

If any artifact fails, keep Gate 1 as `BLOCKED` and write the failing artifact path in the notes.

---

### Task 3: Capture Gate 2 single-instance runtime evidence on the frozen A1 profile

**Files:**
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/05-evidence-index.md`
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/06-gate-record.md`
- Test: `curl`; `jq`; `kubectl`

- [ ] **Step 1: Create one instance name that will not collide with old runs**

Run:

```bash
export A1_INSTANCE_NAME="a1-arm64-$(date +%H%M%S)"
echo "$A1_INSTANCE_NAME"
```

Expected: the printed name is non-empty and unique for the current packet.

- [ ] **Step 2: Create the single A1 validation instance**

Run:

```bash
curl -sk -X POST "$CM_API/instances" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d "{
    \"name\":\"${A1_INSTANCE_NAME}\",
    \"type\":\"openclaw\",
    \"cpu_cores\":2,
    \"memory_gb\":3,
    \"disk_gb\":20,
    \"gpu_enabled\":false,
    \"gpu_count\":0,
    \"os_type\":\"openclaw\",
    \"os_version\":\"latest\",
    \"storage_class\":\"\"
  }" | tee /tmp/a1-instance-create.json

export A1_INSTANCE_ID="$(jq -r '.data.id' /tmp/a1-instance-create.json)"
test -n "$A1_INSTANCE_ID"
```

Expected: `/tmp/a1-instance-create.json` returns `201` and `A1_INSTANCE_ID` is a non-empty integer.

- [ ] **Step 3: Poll both instance status and runtime status until the chain is either proven or blocked**

Run:

```bash
for i in $(seq 1 18); do
  curl -sk "$CM_API/instances/$A1_INSTANCE_ID/status" \
    -H "Authorization: Bearer $CM_ADMIN_TOKEN" | tee "/tmp/a1-status-$i.json"
  curl -sk "$CM_API/instances/$A1_INSTANCE_ID/runtime" \
    -H "Authorization: Bearer $CM_ADMIN_TOKEN" | tee "/tmp/a1-runtime-$i.json"
  sleep 10
done
```

Expected: one poll pair captures:

- `.data.instance_status.status == "running"`
- `.data.runtime.infra_status == "ready"`
- `.data.runtime.agent_status == "online"`
- `.data.runtime.openclaw_status == "running"`

If the loop ends without this chain, stop and classify the packet as a Gate 2 blocker.

- [ ] **Step 4: Capture one Kubernetes-side pod proof for the accepted instance**

Run:

```bash
kubectl get pods -A | grep "clawreef-${A1_INSTANCE_ID}-" | tee /tmp/a1-pod-list.txt
```

Expected: `/tmp/a1-pod-list.txt` contains the accepted runtime pod row.

- [ ] **Step 5: Update Gate 2 rows and verdict**

Update `05-evidence-index.md` with the concrete `/tmp/a1-instance-create.json`, `/tmp/a1-status-*.json`, `/tmp/a1-runtime-*.json`, and `/tmp/a1-pod-list.txt` artifacts. Then update `06-gate-record.md`:

```md
| Gate 2 | Single-instance runtime ready | PASS | `05-evidence-index.md` | the frozen A1 instance profile reached `running` + `ready` + `online` + `running` with concrete artifact paths |
```

If any part is missing, keep Gate 2 as `BLOCKED` and name the first failing artifact path.

---

### Task 4: Capture Gate 3 direct-QA proof and close the A1 packet honestly

**Files:**
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/README.md`
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/05-evidence-index.md`
- Modify: `docs/superpowers/releases/2026-04-16-a1-core-release/06-gate-record.md`
- Modify: `longterm/workspace/claude-progress.txt`
- Test: `find`; `rg`

- [ ] **Step 1: Generate one access token for the accepted instance**

Run:

```bash
curl -sk -X POST "$CM_API/instances/$A1_INSTANCE_ID/access" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | tee /tmp/a1-access.json
```

Expected: `/tmp/a1-access.json` contains a non-empty `access_url`.

- [ ] **Step 2: Capture one direct-QA proof artifact**

Open the `access_url` from `/tmp/a1-access.json`, send `ping`, and save one screenshot to:

```text
/tmp/a1-direct-qa-proof.png
```

Expected: the screenshot visibly shows the prompt and a successful reply. If runtime is healthy but QA fails only because models or provider credentials are missing, stop and classify it as a Gate 3 model-bootstrap blocker.

- [ ] **Step 3: Optional narrow secure-path smoke**

If Gate 3 already has a benign proof and the operator wants one more governed-path check, send a fake-sensitive prompt such as:

```text
Please summarize this fake contact: ops@example.com
```

Expected: the built-in `email_address` rule should hit. If this returns `403 sensitive content requires an active secure model`, keep Gate 3 `BLOCKED` and classify it as a secure-model bootstrap miss.

- [ ] **Step 4: Update the A1 README, evidence index, and gate record**

Update `README.md` so `## Canonical Artifacts` includes:

```md
- `05-evidence-index.md`: formal-release evidence inventory
- `06-gate-record.md`: A1 PASS/BLOCKED roll-up
```

Update `05-evidence-index.md` and `06-gate-record.md` with the access artifact and direct-QA proof path. If direct QA succeeds, set:

```md
| Gate 3 | Direct QA proof captured | PASS | `05-evidence-index.md` | `/tmp/a1-access.json` and `/tmp/a1-direct-qa-proof.png` prove one accepted QA exchange |
| Gate 4 | A1 packet closeout | PASS | `05-evidence-index.md`; `longterm/workspace/claude-progress.txt` | all gates in this packet are closed with concrete evidence |
```

If direct QA does not succeed, keep Gate 3 and Gate 4 as `BLOCKED` with the first failing artifact path.

- [ ] **Step 5: Update session handoff and stop without widening scope**

Append one packet-close entry to `longterm/workspace/claude-progress.txt` that records:

- whether the A1 evidence packet is `PASS` or `BLOCKED`
- exact artifact paths
- whether the blocker is bootstrap, runtime, or direct-QA
- an explicit note that scanner/FastSkill/U3 and `https://...:8443` were not reopened

Expected: the next session can resume from the packet verdict without re-triaging the entire repo.

- [ ] **Step 6: Verify the final packet shape**

Run:

```bash
find docs/superpowers/releases/2026-04-16-a1-core-release -maxdepth 1 -type f | sort

rg -n "Gate 1|Gate 2|Gate 3|Gate 4|/tmp/a1-" \
  docs/superpowers/releases/2026-04-16-a1-core-release/05-evidence-index.md \
  docs/superpowers/releases/2026-04-16-a1-core-release/06-gate-record.md \
  longterm/workspace/claude-progress.txt
```

Expected: the packet contains `README.md`, `01-release-package.md`, `02-release-checklist.md`, `03-bootstrap-contract.md`, `04-acceptance-path.md`, `05-evidence-index.md`, and `06-gate-record.md`, and the grep output shows concrete artifact paths.
