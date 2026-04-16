# A1 Acceptance Path

This document freezes one single-instance acceptance path for the A1 core-chain release.

## Scope Boundary

The canonical A1 success path is:

1. deploy from `deployments/k3s/clawmanager.yaml`
2. confirm `https://localhost:30443/healthz` returns `200`
3. log in as `admin`
4. complete `03-bootstrap-contract.md`
5. create one OpenClaw instance
6. wait until runtime is healthy
7. capture one direct-QA proof

Do not expand this acceptance path to multi-instance, scanner, FastSkill, or frozen `U3`.

## Single-Instance Validation Profile

Use this exact first-instance profile for the local Mac/ARM acceptance replay:

- `type=openclaw`
- `cpu_cores=2`
- `memory_gb=3`
- `disk_gb=20`
- `gpu_enabled=false`
- `gpu_count=0`
- `os_type=openclaw`
- `os_version=latest`

`memory_gb=3` is the frozen local validation envelope for the first A1 instance.

## Create The Instance

```bash
curl -sk -X POST "$CM_API/instances" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{
    "name":"a1-arm64-core",
    "type":"openclaw",
    "cpu_cores":2,
    "memory_gb":3,
    "disk_gb":20,
    "gpu_enabled":false,
    "gpu_count":0,
    "os_type":"openclaw",
    "os_version":"latest",
    "storage_class":""
  }' | tee /tmp/a1-instance-create.json

export A1_INSTANCE_ID="$(jq -r '.data.id' /tmp/a1-instance-create.json)"
test -n "$A1_INSTANCE_ID"
```

Expected: the create call returns `201` and `A1_INSTANCE_ID` is a non-empty integer.

## Poll Until Runtime Ready

```bash
for i in $(seq 1 18); do
  curl -sk "$CM_API/instances/$A1_INSTANCE_ID/runtime" \
    -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data' | tee "/tmp/a1-runtime-$i.json"
  sleep 10
done
```

Expected: one poll shows all of these at the same time:

- instance `status=running`
- `runtime.infra_status=ready`
- `runtime.agent_status=online`
- `runtime.openclaw_status=running`

If this state chain never appears, stop and classify it as an instance-mainline blocker rather than stretching the scope.

## Capture One Direct-QA Proof

Generate one access token for the accepted instance:

```bash
curl -sk -X POST "$CM_API/instances/$A1_INSTANCE_ID/access" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | tee /tmp/a1-access.json
```

Then:

1. open the `access_url` from `/tmp/a1-access.json`
2. send one short prompt such as `ping`
3. capture one proof artifact showing the prompt and a successful reply

This proof can be a screenshot, a screen recording clip, or an equivalent operator-captured artifact.

## Model Guard Before QA Claim

Before any direct-QA success claim, verify again:

```bash
curl -sk "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data.items | map({display_name,is_secure,is_active})'
```

Expected: one active normal model and one active secure model are both present.

If direct QA fails only because model records or provider credentials are missing, classify it as a model-bootstrap blocker, not as a runtime or gateway regression.

## Optional Secure-Path Probe

If the operator wants one narrow governed-path smoke after the benign QA proof, send one fake-sensitive prompt such as:

- `Please summarize this fake contact: ops@example.com`

The built-in `email_address` risk rule should match that text. If this probe returns `403 sensitive content requires an active secure model`, the secure-model bootstrap contract is still incomplete.

## Do Not Claim

- do not claim multi-instance readiness from this path
- do not claim scanner readiness from this path
- do not claim `/api/v1/admin/skills` population from this path
- do not reopen the `https://...:8443` route from this path
