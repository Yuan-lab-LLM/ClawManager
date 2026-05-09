# Control UI auth contract backend/runtime delivery implementation

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, implementation
Gate: CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_GATE

## Verdict

CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_BLOCKED: backend image was delivered and is healthy, but instance 16 runtime image/config does not have trusted-proxy/device-less mediated Control UI auth enabled.

Backend delivery status: complete.

Runtime readiness status: NEEDS_RUNTIME_IMAGE_GATE.

Browser rerun is not appropriate yet. This was not a browser E2E gate, and runtime trusted-proxy readiness is not confirmed.

## Approval token used

`APPROVE_CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_GATE`

## Dependency gates

- CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_APPROVAL_PACKET_DONE
- CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_DONE
- OPENCLAW_RUNTIME_AUTH_CONTRACT_SOURCE_INTAKE_DONE
- CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_RERUN_DONE

Dependency evidence reviewed:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-auth-contract-backend-runtime-delivery-approval-packet.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-signature-auth-contract-implementation.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-openclaw-runtime-auth-contract-source-intake.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-controlui-localization-expanded-capacity-recovery-and-2gi-fresh-instance.md`

## Scoped preflight

Path-limited git status before delivery:

```text
backend/internal/services/instance_proxy_service.go modified
backend/internal/services/instance_proxy_service_test.go modified
the dependency evidence files were untracked in the scoped status output
```

Scoped backend diff check:

```text
command=git diff --check -- backend/internal/services/instance_proxy_service.go backend/internal/services/instance_proxy_service_test.go
exit=0
result=no whitespace errors
```

Root Dockerfile shape:

```text
frontend builder: node:20-alpine, npm ci, npm run build
backend builder: golang:1.26.1-alpine, go build ./cmd/server
runtime image: nginx:1.27-alpine with clawreef-server and built frontend
```

Previous backend deployment image:

```text
command=kubectl -n clawmanager-system get deploy clawmanager-app -o jsonpath=...
exit=0
previous_image=clawmanager:control-plane-backend-ws-challenge-20260506091557
```

Instance 16 runtime image before backend rollout:

```text
namespace=clawmanager-user-1
pod=clawreef-16-oc2gi-loc-221427
runtime_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
runtime_image_id=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54
pod_ip=10.42.0.95
```

No secret values were printed or recorded during preflight.

## Backend test results

Required focused services test:

```text
command=cd backend && go test ./internal/services -run 'Test.*InstanceProxy|Test.*Control|Test.*WebSocket|Test.*Device|Test.*Auth' -count=1
exit=0
result=ok clawreef/internal/services 0.871s
```

Required services package test:

```text
command=cd backend && go test ./internal/services -count=1
exit=0
result=ok clawreef/internal/services 0.664s
```

## Backend image build, push, and inspect

Image tag selection:

```text
tag=gtclaw-auth-contract-backend-20260508115339
host_tag=localhost:5001/clawmanager/clawmanager:gtclaw-auth-contract-backend-20260508115339
cluster_tag=k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-auth-contract-backend-20260508115339
tag_reason=unique gate timestamp tag using the project local registry mapping
```

Build result:

```text
command=docker build -t localhost:5001/clawmanager/clawmanager:gtclaw-auth-contract-backend-20260508115339 .
exit=0
result=image built from root Dockerfile
```

Push result:

```text
command=docker push localhost:5001/clawmanager/clawmanager:gtclaw-auth-contract-backend-20260508115339
exit=0
result=push completed to local registry
index_digest=sha256:ebf7fed23ce526642f04f1995f9b8ee523883dbd187725469ddc20d9c0e86e7c
```

Required image inspect:

```text
command=docker buildx imagetools inspect localhost:5001/clawmanager/clawmanager:gtclaw-auth-contract-backend-20260508115339
exit=0
media_type=application/vnd.oci.image.index.v1+json
index_digest=sha256:ebf7fed23ce526642f04f1995f9b8ee523883dbd187725469ddc20d9c0e86e7c
linux_arm64_manifest=sha256:ad601204a4347543df4e95c307ef4c5f2f73a0a6e6bd127f5e4827e7f44f0721
attestation_manifest=sha256:864909a3043917ddb89b2bda9627816e7bf42c28883a49f2cd5e7e58f92053c4
```

## Backend rollout

Deployment update:

```text
command=kubectl -n clawmanager-system set image deployment/clawmanager-app clawmanager-app=k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-auth-contract-backend-20260508115339
exit=0
result=deployment.apps/clawmanager-app image updated
```

Required new deployment image check:

```text
command=kubectl -n clawmanager-system get deploy clawmanager-app -o jsonpath='{.spec.template.spec.containers[?(@.name=="clawmanager-app")].image}{"\n"}'
exit=0
new_image=k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-auth-contract-backend-20260508115339
```

Required rollout status:

```text
command=kubectl -n clawmanager-system rollout status deploy/clawmanager-app --timeout=180s
exit=0
result=deployment "clawmanager-app" successfully rolled out
```

Required backend pod check:

```text
command=kubectl -n clawmanager-system get pods -l app=clawmanager-app -o wide
exit=0
pod=clawmanager-app-7ff9778b6b-9s5wh
ready=1/1
status=Running
restarts=0
age_at_check=20s
pod_ip=10.42.0.97
node=k3d-clawmanager-server-0
```

Required healthz result:

```text
command=curl -k -sS -o /dev/null -w '%{http_code}\n' https://localhost:30443/healthz
exit=0
http_code=200
```

## Runtime trusted-proxy readiness

Target runtime:

```text
instance_id=16
instance_name=oc2gi-loc-221427
namespace=clawmanager-user-1
pod=clawreef-16-oc2gi-loc-221427
runtime_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-20260507211942
runtime_image_id=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:d8d3b33ce4cda05a592e7842c5115ef82c115212760af44e013cf41cfb7f7b54
pod_phase=Running
ready=true
restart_count=0
openclaw_package=openclaw@2026.4.14
```

Runtime 18789 checks:

```text
runtime 18789 status=reachable
service=clawreef-16-oc2gi-loc-221427-svc
service_port=http:3001->3001/TCP
service_port=control-ui:18789->18789/TCP
command=kubectl -n clawmanager-user-1 exec clawreef-16-oc2gi-loc-221427 -c desktop -- curl 127.0.0.1:18789 root
exit=0
http_code=200
health_payload_shape={"ok":true,"status":"live"}
```

Runtime config/auth inspection was sanitized. Secret values, env values, headers, cookies, and URLs were not printed.

```text
config_path=/config/.openclaw/openclaw.json
config_exists=true
gateway_keys=auth,bind,controlUi,mode,nodes,port,tailscale
auth_object_keys=mode,token
config_auth_mode=none
has_trusted_proxy_config=false
trusted_proxy_keys_count=0
control_ui_allowed_origins_count=3
startup_command_shape=openclaw gateway run --bind lan --auth token
env_names_inspected=true
env_values_printed=false
```

Runtime image/source signal:

```text
package=openclaw@2026.4.14
trusted_proxy_type_or_cli_mentions_present=true
trusted_proxy_enabled_in_running_config=false
actual_gateway_runtime_grep_for_trusted_proxy_marker_in_primary_gateway_bundle=no direct match in inspected gateway bundle
```

Readiness verdict:

```text
runtime_trusted_proxy_readiness=NEEDS_RUNTIME_IMAGE_GATE
reason=the running instance 16 runtime exposes 18789 and has the localized OpenClaw image, but its sanitized config/startup path does not enable trusted-proxy auth and does not prove device-less mediated Control UI acceptance
```

The backend patch is now in the running ClawManager backend, but the current runtime is still not confirmed to satisfy the device-less mediated trusted-proxy contract required by the source intake. A browser rerun could still stop at a Control UI auth failure, including `device signature invalid` or a different first-connect rejection, because runtime acceptance is not ready.

## Browser rerun decision

Browser rerun is not appropriate in this gate and is not recommended as the immediate next gate.

Required before browser rerun:

- runtime image/config must enable or prove trusted-proxy mediated Control UI auth;
- runtime tests must prove direct trusted-proxy spoofing fails;
- runtime tests must prove direct token, password, pairing, and device-signature clients do not regress;
- a fresh or explicitly current runtime instance must run that runtime image/config;
- only then should a separate browser/manual E2E gate check the UI path.

## Next gate recommendation

Exact next gate recommendation:

`CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE`

Suggested approval token for that later gate:

`APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_GATE`

Minimum scope for that next gate:

- patch or enable the OpenClaw runtime trusted-proxy/device-less mediated Control UI contract in source/config/image packaging;
- build and push a new runtime image only under that separate approval;
- prove trusted proxy spoofing is rejected for direct clients;
- preserve direct token, password, pairing, and device-signature client behavior;
- deploy a fresh/current runtime instance only under a later approved runtime deployment gate;
- continue to prohibit browser E2E until runtime readiness evidence exists.

## Forbidden actions confirmation

- no browser E2E
- no DevTools
- no Playwright
- no frontend modification
- no deployments/k3s/clawmanager.yaml modification
- no deployments/k8s modification
- no docs modification
- no longterm write-back
- no AgentTeam modification
- no UnifiedFramework modification
- no existing evidence modification
- no existing control-ui artifact modification
- no existing runtime startup artifact modification
- no existing runtime image assembly artifact modification
- no runtime image patch
- no runtime image build
- no runtime image push
- no instance mutation
- no runtime instance creation, deletion, or modification
- no database access or mutation
- no database row, instance record, session, or asset mutation
- no pod, service, or PVC deletion
- no registry cleanup
- no storage/cache/cookie cleanup
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded

Allowed backend deployment mutation performed:

- only `clawmanager-system/deployment/clawmanager-app` container `clawmanager-app` image was updated to the new backend image.

## Sensitive scan result

Final verification commands:

```text
command=git diff --check -- backend/internal/services/instance_proxy_service.go backend/internal/services/instance_proxy_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-auth-contract-backend-runtime-delivery-implementation.md
exit=0
result=no whitespace errors
```

```text
command=rg -n "CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION|device signature invalid|backend image|rollout|healthz|trusted-proxy|runtime 18789|no browser E2E|no instance mutation|no runtime image patch|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-auth-contract-backend-runtime-delivery-implementation.md
exit=0
result=required markers found
```

```text
command=rg -n -i -o sensitive-shape scan against this evidence file
exit=1
result=no matches; secret_shape_match_count=0
```

```text
command=git status --short -- backend/internal/services/instance_proxy_service.go backend/internal/services/instance_proxy_service_test.go specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-auth-contract-backend-runtime-delivery-implementation.md
exit=0
result=backend service files remain modified from the approved backend patch; this implementation evidence file is untracked
```
