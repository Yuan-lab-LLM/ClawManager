# A1 Bootstrap Contract

This document freezes the exact bootstrap contract for the A1 core-chain release.

## Admin API Session Bootstrap

```bash
export CM_BASE_URL="https://localhost:30443"
export CM_API="$CM_BASE_URL/api/v1"

export CM_ADMIN_TOKEN="$(curl -sk -X POST "$CM_API/auth/login" \
  -H 'Content-Type: application/json' \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.data.access_token')"

test -n "$CM_ADMIN_TOKEN"
```

Expected: `CM_ADMIN_TOKEN` is a non-empty JWT-like string.

## ARM Runtime Image Bootstrap

Freeze this exact OpenClaw ARM bootstrap image reference for A1:

- `ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:dev-arm64-pkt09-20260414170434`

Apply it through the manager API:

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

Verify it is now the active `openclaw` system image setting:

```bash
curl -sk "$CM_API/system-settings/images" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data.items[] | select(.instance_type=="openclaw")'
```

Expected: the returned `image` matches the frozen ARM bootstrap reference above.

## Model Bootstrap Contract

The A1 formal release requires both of these operator-owned model records before any instance-mainline or direct-QA claim:

- one active normal model
- one active secure model

The operator must supply these runtime values:

```bash
export LLM_BASE_URL="https://example.invalid/v1"
export LLM_MODEL="example-model"
export LLM_API_KEY="replace-at-runtime"
```

Create or update the normal model:

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
  }"
```

Create or update the secure model:

```bash
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
  }"
```

Verify both active records exist before proceeding:

```bash
curl -sk "$CM_API/admin/models" \
  -H "Authorization: Bearer $CM_ADMIN_TOKEN" | jq '.data.items | map({display_name,is_secure,is_active,provider_model_name,base_url})'
```

Expected: one item has `"is_secure": false`, one item has `"is_secure": true`, and both are active.

## Hard Rules

- do not hardcode provider keys into the repo
- do not hardcode provider keys into manifests
- do not rely on manual DB edits
- if instance creation fails with `no active models are configured`, classify it as a bootstrap-contract miss
- if direct QA returns `403 sensitive content requires an active secure model`, classify it as a bootstrap-contract miss
