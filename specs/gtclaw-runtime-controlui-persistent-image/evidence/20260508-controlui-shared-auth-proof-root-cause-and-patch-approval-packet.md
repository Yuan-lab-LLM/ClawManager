# CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Gate:

```text
CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_GATE
```

## Verdict

```text
CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_DONE
```

This is an approval packet only. This gate performed read-only source/evidence review and wrote this packet. It performed no patch, no build/tag/push/pull image, no rollout/patch/scale K8S, no instance create/stop/delete, no browser/manual E2E, no DevTools, no Playwright, no runtime env/config/secret read, no database mutation, no direct SQL, no cleanup, no Mem0 write, no longterm write-back, no passes:true, no Close, and no git stage/commit/push.

## Dependency State

```text
dependency_gate=CONTROLUI_DIAGNOSTIC_STOP_INSTANCE_16_FOR_CAPACITY_AND_LIVE_VECTOR_FAILED
replacement_instance=20 / oc2gi-diag-r-183249
backend_diagnostic_enabled=CONTROLUI_PROXY_AUTH_DIAGNOSTICS=1
runtime_diagnostic_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
```

Sanitized live vector from the dependency evidence:

```text
is_control_ui=true
role=operator
auth_method=token
auth_ok=false
shared_auth_ok=false
has_shared_auth=true
forwarded_prefix_shape=backend_control_ui_prefix_match
cond_is_control_ui=true
cond_role_operator=true
cond_shared_auth_proof=false
cond_auth_method_shared_secret=true
cond_forwarded_prefix_match=true
mediated_helper_result=false
device_identity_required_visible=true
device_signature_invalid_visible=false
connected_state=false
chat_ready_state=false
```

## Conditions Already Excluded

The live vector excludes these as root-cause candidates:

```text
isControlUi=true
role=operator
authMethod=token
forwardedPrefix=backend_control_ui_prefix_match
```

The backend bridge also confirms the mediated path reached the runtime and the browser was not treated as direct browser device-less allow:

```text
ws_upstream_shape=observed
ws_first_connect=observed
ws_first_upstream_frame=observed
upstream_auth_header_present=true
browser_cookie_seen=true
browser_auth_header_seen=false
source_query_had_token=false
source_query_had_password=false
rewritten_auth_token_present=true
rewritten_device_present=false
first_upstream_error_code=none
```

## Current Failing Condition

The unique failed proof condition is shared-auth proof:

```text
auth_ok=false
shared_auth_ok=false
cond_shared_auth_proof=false
mediated_helper_result=false
```

Runtime-side interpretation:

```text
has_shared_auth=true
auth_method=token
shared_secret_auth_shape_present=true
shared_secret_auth_proof_failed=true
```

This means the runtime saw a shared-auth-shaped request, but did not validate it as a shared proof.

## Read-Only Source Findings

Backend credential source reviewed:

```text
backend_source=backend/internal/services/instance_service.go
backend_gateway_token_field=instance.AccessToken
backend_create_path_generates_gateway_token_if_missing=true
backend_start_path_generates_gateway_token_if_missing=true
backend_pod_env_sets_OPENCLAW_GATEWAY_TOKEN_from_instance_access_token=true
backend_proxy_source=backend/internal/services/instance_proxy_service.go
backend_upstream_header_source=InstanceProxyUpstreamAuth.OpenClawGatewayToken
backend_upstream_header_value_source=instance.AccessToken
backend_first_connect_rewrite_shape=params.auth.token
backend_first_connect_removes_browser_device=true
backend_first_connect_removes_browser_auth=true
```

Runtime artifact source reviewed:

```text
runtime_config_artifact=specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/defaults/openclaw-agent/config.yaml
runtime_command_wrapper=/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract
runtime_command_auth_mode=--auth token
runtime_helper_source=specs/gtclaw-runtime-controlui-persistent-image/openclaw-runtime-source-artifact/20260508-trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
runtime_helper_requires_shared_auth_proof=true
runtime_helper_requires_backend_prefix=true
runtime_helper_keeps_direct_browser_device_less_rejected=true
```

The current evidence is not enough to decide whether the failed shared proof is a token mismatch, token source mismatch, parser mismatch, or runtime propagation mismatch, because this approval-packet gate did not read runtime env/config/secret values or database token values.

## Root-Cause Branches For Next Gate

The next gate must diagnose these branches in order, using secret-safe equality checks only:

1. Backend stored credential versus runtime active shared credential:

```text
question=backend instance.AccessToken equals replacement runtime active shared token?
next_gate_check=secret-safe equality
record_only=boolean, source enum, status code
record_values=false
```

2. Runtime token source:

```text
question=runtime --auth token reads OPENCLAW_GATEWAY_TOKEN, config token, secret file, or another token source?
next_gate_check=source enum plus secret-safe equality
record_only=enum and equality booleans
record_values=false
```

3. First-connect auth shape:

```text
question=first connect params.auth.token shape matches OpenClaw runtime shared token parser?
observed_backend_shape=params.auth.token
observed_runtime_shape=auth_method=token, has_shared_auth=true
next_gate_check=parser path and accepted property shape
record_only=shape enum and parser branch name
record_values=false
```

4. Runtime sharedAuthOk propagation:

```text
question=runtime sharedAuthOk calculation uses connect params auth token for missing-device decision?
observed_shared_auth_ok=false
next_gate_check=source/readback path for sharedAuthOk computation
record_only=branch enum and boolean
record_values=false
```

5. Handshake auth versus connect params auth split:

```text
question=Authorization handshake succeeds but connect params auth is checked by a different validator/path?
observed_upstream_auth_header_present=true
observed_first_upstream_frame_error_code=none
observed_missing_device_decision_shared_auth_ok=false
next_gate_check=compare validation branch outcomes without logging credential material
record_only=boolean and branch enum
record_values=false
```

## Requested Approval

Approve the next gate with this exact口令:

```text
APPROVE_CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_GATE
```

## Next Gate Allowed Actions Draft

The next gate must be diagnostic-first:

```text
diagnostic_first=true
secret-safe equality check only=true
record_secret_values=false
record_only=boolean,enum,path shape,query key names,status/code,error code,digest,sanitized condition names
```

Proposed diagnostic permissions:

- Read existing evidence, backend code/tests, runtime source artifacts, runtime assembly artifacts, and verifier scripts.
- Read replacement instance 20 status and sanitized logs.
- Perform secret-safe equality diagnostics comparing backend credential source and replacement runtime active shared credential without printing or writing any token/password/key/cookie/bearer/authorization/header/access URL value.
- If needed, read runtime env/config/secret material only inside a command that outputs equality booleans and source enums, not values.
- If needed, read backend instance credential material only inside a command that outputs equality booleans and source enums, not values.
- If no API/service path can provide the backend credential source, allow read-only DB credential comparison only as secret-safe equality, with no direct SQL mutation and no value output.
- Inspect runtime shared-auth parser/propagation source and readback runtime file content needed to patch safely.

Proposed patch permissions after diagnostics identify the root cause:

- If backend token source and runtime shared token are inconsistent, prefer fixing backend credential source or runtime token provisioning so the same server-owned credential is used on both sides.
- If token source equality is true but runtime sharedAuthOk remains false, patch runtime shared auth propagation/parser so a valid server-owned shared proof is reflected in `sharedAuthOk`.
- If connect params auth shape is incompatible with the runtime parser, patch backend first-connect auth shape to the parser-supported shape while still stripping browser-supplied auth/device material.
- Update source and assembly verifier checks so direct browser device-less remains rejected and invalid device-signature protection remains intact.
- Run node syntax checks, verifier checks, targeted backend tests when backend code changes, and sanitized evidence scans.
- Build/push/runtime delivery and one browser/manual live-vector rerun should require the next gate to explicitly name the target image tag and allowed runtime instance handling.

## Next Gate Required Safety Invariants

```text
no insecure auth
no global bypass
no direct browser device-less allow
no broad origin workaround
no trustedProxy JSON marker trust
no prefix as credential
direct browser device-less remains rejected
invalid device-signature protection remains enabled
trustedProxyMarker remains globally forbidden
connectParams.trustedProxy remains globally forbidden
connectParams.auth.trustedProxy remains globally forbidden
trustedProxyMarker: remains globally forbidden
no secret/header/access URL logging
no token value logging
no password value logging
no key value logging
no cookie value logging
no bearer value logging
no authorization value logging
no old asset cleanup
no old session cleanup
no old evidence cleanup
no old image cleanup
no Mem0 write
no passes:true
no Close
no longterm write-back
no git stage/commit/push
```

## Current Gate Non-Actions

- no code modification
- no build/tag/push/pull image
- no rollout/patch/scale K8S
- no instance create/stop/delete
- no browser/manual E2E
- no DevTools
- no Playwright
- no runtime env/config/secret value read
- no backend credential value read
- no database mutation
- no direct SQL
- no cleanup of old asset/session/evidence/image
- no handling of instances 17/18/20
- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no Mem0 write
- no passes:true
- no Close
- no longterm write-back
- no git stage/commit/push

## Verification Commands

```bash
sed -n '1,280p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-shared-auth-proof-root-cause-and-patch-approval-packet.md
rg -n "CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_APPROVAL_PACKET_DONE|APPROVE_CONTROLUI_SHARED_AUTH_PROOF_ROOT_CAUSE_AND_PATCH_GATE|backend_control_ui_prefix_match|secret-safe equality|no insecure auth|no global bypass|no direct browser device-less allow|no trustedProxy JSON marker trust|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-shared-auth-proof-root-cause-and-patch-approval-packet.md
rg -n "(token|password|key|cookie|bearer|authorization|access URL)" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-shared-auth-proof-root-cause-and-patch-approval-packet.md || true
grep -n '[[:blank:]]$' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-shared-auth-proof-root-cause-and-patch-approval-packet.md || true
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-shared-auth-proof-root-cause-and-patch-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-shared-auth-proof-root-cause-and-patch-approval-packet.md
```

## Verification Results

```text
sed_render=passed
required_marker_scan=passed
sensitive_scan_result=only field names, source enums, boolean flags, and prohibition text; no secret values
trailing_whitespace_scan=passed
git_diff_check=passed
git_status_new_evidence=untracked
```
