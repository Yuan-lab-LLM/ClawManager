# CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH

Verdict: CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_DONE

Gate: APPROVE_CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_GATE

Scope:
- replacement instance: 20 / oc2gi-diag-r-183249
- replacement pod: clawmanager-user-1/clawreef-20-oc2gi-diag-r-183249
- backend diagnostic env: CONTROLUI_PROXY_AUTH_DIAGNOSTICS=1
- runtime diagnostic image observed: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
- no build/tag/push/pull, rollout, scale, instance lifecycle, browser E2E, cleanup, stage, commit, or push was performed in this gate

## Starting Live Vector

The prior live vector narrowed the failure to the shared auth proof branch:
- is_control_ui=true
- role=operator
- auth_method=token
- auth_ok=false
- shared_auth_ok=false
- has_shared_auth=true
- forwarded_prefix_shape=backend_control_ui_prefix_match
- cond_is_control_ui=true
- cond_role_operator=true
- cond_shared_auth_proof=false
- cond_auth_method_shared_secret=true
- cond_forwarded_prefix_match=true
- mediated_helper_result=false

Conditions already excluded by the live vector:
- control UI detection was true
- operator role was true
- shared-secret auth method was true
- backend control UI forwarded prefix match was true

Current unique failing condition:
- auth_ok=false
- shared_auth_ok=false
- cond_shared_auth_proof=false

## Secret-Safe Root Cause Diagnostics

Only booleans, enums, branch names, source names, and path shapes were recorded.

secret-safe equality:
- backend_runtime_token_equal=true
- backend_runtime_active_process_token_equal=true
- runtime_token_source=OPENCLAW_GATEWAY_TOKEN
- runtime_env_token_present=true
- runtime_config_auth_mode=none
- runtime_config_token_present=true
- backend_config_token_equal=false
- backend_env_token_equal=true
- config_env_token_equal=false
- resolved_auth_mode_with_cli_override_shape=token
- resolved_auth_mode_source_with_cli_override_shape=override
- resolved_auth_token_present_with_cli_override_shape=true
- resolved_auth_token_equals_env_with_cli_override_shape=false
- resolved_auth_token_equals_backend_with_cli_override_shape=false

Parser and path diagnostics:
- params.auth.token shape supported=true
- sharedAuthOk parser uses connect params auth token when the resolved shared credential matches
- handshake header validator and connect params auth validator use different call paths
- runtime resolver credential branch: config-first credential selected before env-backed server-owned credential

Root cause:
- backend instance credential equals the replacement runtime OPENCLAW_GATEWAY_TOKEN
- runtime active process env credential equals the backend instance credential
- runtime persisted config contains a different gateway.auth token branch
- runtime config-first resolver preserves the stale config credential even when the CLI/auth mode branch is token
- therefore first-connect params.auth.token carries the server-owned shared proof, but sharedAuthOk=false because the runtime compares it against the stale config credential

## Patch

Minimal patch applied in the runtime assembly artifact:
- `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/usr/local/bin/openclaw-ensure-controlui-origin`
  - when OPENCLAW_GATEWAY_TOKEN is present, synchronize gateway.auth.mode to token and gateway.auth.token to the server-owned runtime credential during startup config materialization
  - preserves existing control UI origin materialization
  - does not treat forwarded prefix as a credential
- `runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs`
  - verifies the assembly startup script contains the server-owned gateway credential sync contract
  - preserves global forbidden marker checks and scoped GTClaw helper unsafe logging checks

Safety invariants retained:
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no prefix-as-credential behavior
- direct browser device-less requests remain rejected by the runtime contract
- invalid device-signature protection remains present in the runtime contract
- trustedProxy marker strings remain globally forbidden
- diagnostic logging remains value-free
- no passes:true
- no Close
- no git stage/commit/push

## Verification

Syntax and script checks:
- node --check source patch script: passed
- node --check source verifier: passed
- node --check assembly patch script: passed
- node --check assembly verifier: passed
- bash -n assembly startup script: passed

Runtime contract checks:
- source verifier against replacement readback server.impl-BbJvXoPb.js: passed
- assembly verifier against replacement readback server.impl-BbJvXoPb.js: passed
- startup config transform probe: startup_sync_auth_mode=token, startup_sync_token_equals_env=true, startup_sync_origin_present=true, credential_values_recorded=false

Backend service tests:
- `go test ./internal/services -run 'Test.*InstanceProxy|Test.*Control|Test.*WebSocket|Test.*Device|Test.*Auth|Test.*Gateway|Test.*OpenClaw' -count=1`: passed
- `go test ./internal/services -count=1`: passed

## Delivery Boundary

This gate completed root cause and local patch verification only.

Next gate requires explicit delivery/live-vector approval for:
- build/push of a new runtime diagnostic image from the patched assembly artifact
- delivery of that runtime diagnostic image to a fresh diagnostic runtime instance
- one browser/manual E2E run
- sanitized backend/runtime/browser live vector collection
