# CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_ROOT_CAUSE_AND_PATCH

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Gate phrase: APPROVE_CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_ROOT_CAUSE_AND_PATCH_GATE

## Verdict

```text
CONTROLUI_POST_CONNECT_MISSING_OPERATOR_SCOPES_ROOT_CAUSE_AND_PATCH_DONE
```

## Dependency State

The previous live vector proved that the shared-auth blocker was already resolved:

```text
webchat_connected=true
auth_ok=true
shared_auth_ok=true
mediated_helper_result=true
```

The active blocker was post-connect missing_scope:

```text
chat_ready_state=false
mg_internal=false
runtime_post_connect_error_shape=missing_scope
runtime_missing_scope_names=operator.read,operator.pairing
```

## Diagnostic Answers

1. Browser first-connect `params.scopes` actual names:

```text
browser_first_connect_params.scopes=operator.admin,operator.read,operator.write,operator.approvals,operator.pairing
source=control-ui bundle buildConnectPlan uses default Zn list and buildConnectParams forwards scopes
```

2. Backend first-connect rewrite behavior:

```text
backend_rewrite_preserves_scopes_field=true
backend_rewrite_drops_scopes=false
backend_rewrite_normalizes_scopes=false
backend_rewrite_removes_browser_auth_material=true
backend_rewrite_removes_browser_device_material=true
```

3. Runtime shared-auth session scope propagation:

```text
runtime_scope_propagation_failure=true
runtime_missing_device_mediated_allow_reached=true
runtime_unbound_scope_clear_after_mediated_allow=true
runtime_session_scopes_after_clear=[]
post_connect_scope_blocker=operator.read,operator.pairing
```

4. Post-connect method/action scope requirements:

```text
sessions.subscribe=operator.read
agent.identity.get=operator.read
agents.list=operator.read
node.list=operator.read
device.pair.list=operator.pairing
chat.history=operator.read
sessions.list=operator.read
models.list=operator.read
commands.list=operator.read
```

5. `operator.pairing` grant source:

```text
operator.pairing_should_be_backend_mediated_control_ui_policy=true
runtime_client_explicit_request_alone_is_not_authority=true
reason=the browser sends a broad operator scope list, but the accepted device-less path is the existing backend-mediated shared-auth allow path; the runtime must reduce that path to the minimal operator read and pairing set.
```

6. Read-only normalization path that omits pairing:

```text
read_without_pairing_normalization_path_exists=true
source=shared device scope normalizer adds operator.read for operator.write or operator.admin, but does not add operator.pairing
impact=that path can explain read recovery only; it cannot satisfy device.pair.list
```

## Root Cause

Root cause:

```text
runtime accepted the mediated device-less Control UI connection, then cleared the accepted first-connect scopes before storing the connected client session.
```

The browser first-connect was not missing `operator.read` or `operator.pairing`; it was over-broad. The backend rewrite preserved the `scopes` field and did not normalize it. The runtime then cleared unbound scopes after the mediated missing-device allow decision because the shared-auth branch still used the existing unbound-scope clearing rule. Post-connect method guards therefore saw an empty session scope list and returned missing_scope for `operator.read` and `operator.pairing`.

## Patch

Changed current runtime contract artifacts only:

```text
specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs
specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/patch-openclaw-trusted-proxy-contract.mjs
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/openclaw-runtime/verify-trusted-proxy-contract.mjs
specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md
```

Patch behavior:

```text
mediated_scope_normalizer_added=true
mediated_scope_normalized_to=operator.read,operator.pairing
scope_clear_suppressed_only_for_gtmanager_mediated_allow=true
connect_session_scopes_rewritten_before_clear=true
no operator.admin
no missing_scope bypass
no insecure auth
no global bypass
no direct browser device-less allow
```

The patch does not change method guards and does not bypass missing_scope. It only fixes scope propagation for the existing backend-mediated Control UI allow decision, and it drops the browser's broad operator request to the minimal required set.

## Verification

RED verifier check before runtime patch:

```text
source_verifier_red=exit_1_missing_mediated_scope_normalization_contract
assembly_verifier_red=exit_1_missing_mediated_scope_normalization_contract
```

GREEN checks after patch:

```text
node_check_source_patch=passed
node_check_source_verifier=passed
node_check_assembly_patch=passed
node_check_assembly_verifier=passed
source_verifier_against_openclaw_2026_4_14_runtime=passed
assembly_verifier_against_openclaw_2026_4_14_runtime=passed
patched_runtime_syntax_and_idempotency_check=passed
```

Backend service checks:

```text
cd backend && go test ./internal/services -run 'Test.*InstanceProxy|Test.*Control|Test.*WebSocket|Test.*Device|Test.*Auth|Test.*Scope' -count=1
result=passed

cd backend && go test ./internal/services -count=1
result=passed
```

## Boundary

This gate intentionally did not perform live delivery or browser verification:

```text
no build/tag/push
no deploy
no browser E2E
no runtime image delivery
no instance mutation
no database mutation
no old evidence modification
no frontend modification
no deployments modification
no docs modification
no longterm write-back
no AgentTeam modification
no UnifiedFramework modification
no Mem0 write
no passes:true
no Close
no git stage/commit/push
```

`chat_ready_state=true` and `mg_internal=true` are not claimed in this gate because build, deploy, and browser E2E were explicitly out of scope.
