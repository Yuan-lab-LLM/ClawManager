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
