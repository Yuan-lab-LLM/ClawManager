# Fresh Instance Runtime Deployment Origin Allowlist Gate - 2026-05-06

Worker: FreshInstanceRuntimeDeploymentOriginAllowlistWorker

Verdict: `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_DONE`

Not `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_BLOCKED`: the backend, cluster, registry, and approved image preflight passed; exactly one new isolated 2Gi OpenClaw instance was created through the normal ClawManager backend API path; the new pod reached Ready and satisfied the required runtime listener, helper, and config checks without browser E2E.

This gate's only persistent repository write is this evidence file. It did not build, tag, push, pull, run browser E2E, use Chrome DevTools MCP, use Playwright, deploy or restart backend, mutate registry, manually patch a pod or Service, use `kubectl cp`, modify existing instance `9`, modify existing instance `10`, delete existing instance `9`, delete existing instance `10`, modify backend/frontend/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence, write Mem0, write longterm, set `passes:true`, run Close, stage, commit, or push.

## Dependency Gates And Approval

| Gate | Status used |
| --- | --- |
| Fresh Instance Runtime Deployment Origin Allowlist Approval Packet | `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_APPROVAL_PACKET_DONE` |
| User approval | `APPROVE_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_GATE` |
| Runtime Image Build/Tag/Push Origin Allowlist Gate | `RUNTIME_IMAGE_BUILD_TAG_PUSH_ORIGIN_ALLOWLIST_DONE` |

## Approved Image

| Field | Value |
| --- | --- |
| host tag | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656` |
| in-cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656` |
| approved image index digest | `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45` |
| approved linux/arm64 digest | `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97` |

## Read-only Preflight Results

| Check | Result |
| --- | --- |
| backend `/healthz` | HTTP `200`, body `ok` |
| `k3d cluster list` / `k3d cluster get clawmanager` | `clawmanager` server readiness `1/1`, load balancer `true` |
| Kubernetes control plane | `kubectl cluster-info --request-timeout=5s` succeeded |
| node readiness | `k3d-clawmanager-server-0` Ready |
| backend deployment | `Deployment/clawmanager-app`, image `clawmanager:control-plane-backend-ws-challenge-20260506091557`, ready `1/1` |
| registry health | `http://localhost:5001/v2/` HEAD returned HTTP `200` |
| host tag local inspect | image ID `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`, `linux/arm64` |
| in-cluster tag local inspect | image ID `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`, `linux/arm64` |
| registry HTTP manifest inspect | HTTP `200`, content type `application/vnd.oci.image.index.v1+json` |
| registry content digest | `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45`, matches approved image index digest |
| registry linux/arm64 digest | `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97`, matches approved linux/arm64 digest |

One read-only `docker manifest inspect` attempt against the local registry tag returned `no such manifest`; it did not mutate state and was superseded by the successful registry HTTP v2 manifest inspection above.

Before creation, existing runtime pods were:

```text
runtime_pods_count=2
pod=clawmanager-user-1/clawreef-10-oc2gi-185707 phase=Running restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
pod=clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 phase=Running restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

## Fresh Instance Creation

API authentication succeeded through `POST /api/v1/auth/login`. Credential values and the returned token were kept only in process memory and were not printed or recorded.

Create request used normal backend API path:

```text
POST /api/v1/instances
name=oc2gi-oa-131301
type=openclaw
cpu_cores=1
memory_gb=2
disk_gb=20
gpu_enabled=false
gpu_count=0
os_type=openclaw
os_version=local
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656
```

Sanitized API result:

```text
api_instances_before_count=6 ids=1,2,3,5,9,10
create_http_status=201
api_instances_after_count=7 ids=1,2,3,5,9,10,11
```

Exactly one new API instance was created: `11` / `oc2gi-oa-131301`.

A local post-create `jq` formatting command had a quoting error after the `POST` already returned HTTP `201`; the create request was not retried.

API status read after pod readiness:

```text
api_instance id=11 name=oc2gi-oa-131301 status=running type=openclaw memory_gb=2 image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656 pod_namespace=clawmanager-user-1 pod_name=clawreef-11-oc2gi-oa-131301
```

## Pod And Image Verification

`kubectl wait --for=condition=Ready pod/clawreef-11-oc2gi-oa-131301 -n clawmanager-user-1 --timeout=240s` succeeded.

| Field | Value |
| --- | --- |
| instance id/name | `11` / `oc2gi-oa-131301` |
| namespace | `clawmanager-user-1` |
| pod | `clawreef-11-oc2gi-oa-131301` |
| container | `desktop` |
| phase | `Running` |
| Ready condition | `True` |
| PodIP | `10.42.0.86` |
| node | `k3d-clawmanager-server-0` |
| QoS | `Guaranteed` |
| restart count | `0` |
| OOMKilled | `false`; no OOM event was present |
| pod image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-20260506124656` |
| pod imageID | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45` |

Image digest comparison:

| Item | Observed | Expected | Result |
| --- | --- | --- | --- |
| pod imageID / image index digest | `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45` | `sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45` | match |
| registry linux/arm64 digest | `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97` | `sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97` | match |

New pod events were normal scheduling, image pull, container create, and container start events. No OOMKilled or failure event was recorded.

After creation, runtime pods were:

```text
runtime_pods_after_count=3
pod=clawmanager-user-1/clawreef-10-oc2gi-185707 phase=Running readyContainers=1 restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
pod=clawmanager-user-1/clawreef-11-oc2gi-oa-131301 phase=Running readyContainers=1 restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45
pod=clawmanager-user-1/clawreef-9-gtclaw-fresh-bind-lan-auth-20260505-175724 phase=Running readyContainers=1 restarts=0 imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
```

Instance `9` and instance `10` were not modified or deleted.

## Service And Endpoint Verification

| Field | Value |
| --- | --- |
| service | `clawreef-11-oc2gi-oa-131301-svc` |
| namespace | `clawmanager-user-1` |
| type | `ClusterIP` |
| ServiceIP | `10.43.221.32` |
| selector | `app=clawreef,instance-id=11` |

Service ports:

| Name | Port | TargetPort | Protocol |
| --- | ---: | ---: | --- |
| `http` | `3001` | `3001` | `TCP` |
| `control-ui` | `18789` | `18789` | `TCP` |

The Service includes both `3001` and `18789`.

Endpoint and EndpointSlice summary:

```text
endpoints_name=clawreef-11-oc2gi-oa-131301-svc
endpoint_address ip=10.42.0.86 targetRef=Pod/clawreef-11-oc2gi-oa-131301
endpoint_port name=http port=3001 protocol=TCP
endpoint_port name=control-ui port=18789 protocol=TCP
EndpointSlice name=clawreef-11-oc2gi-oa-131301-svc-6wdv9 addressType=IPv4 endpoints=10.42.0.86 ports=http:3001,control-ui:18789
```

## Listener Reachability

Listener checks were read-only `curl` checks against the new PodIP and ServiceIP from inside the new pod network context with `--noproxy '*'`.

| Target | Host | Port | curl exit | HTTP code | Result |
| --- | --- | ---: | ---: | ---: | --- |
| PodIP `18789` | `10.42.0.86` | `18789` | `0` | `200` | reachable |
| ServiceIP `18789` | `10.43.221.32` | `18789` | `0` | `200` | reachable |
| PodIP `3001` regression | `10.42.0.86` | `3001` | `0` | `400` | reachable |
| ServiceIP `3001` regression | `10.43.221.32` | `3001` | `0` | `400` | reachable |

The required PodIP `18789`, ServiceIP `18789`, and `3001` regression reachability checks passed. No browser E2E was run.

## Helper Scripts And Config Verification

Approved build evidence expected:

| Path | Expected mode | Expected sha256 |
| --- | ---: | --- |
| `/usr/local/bin/openclaw-ensure-controlui-origin` | `755` | `c4151fa9a08ee04c41b212a9b30838f1f19d474fe50b6d2fdc848994d8fba071` |
| `/usr/local/bin/openclaw-gateway-with-origin-allowlist` | `755` | `79910c9dc6a0dcd0d809af1fc21a45052afeae66732d7a0ff4185089e8c3995c` |
| `/defaults/openclaw-agent/config.yaml` | `644` | `bdc8bf155539762c02f37ffbeb27e2dcec48bc5c3badaf4a17ec2edd6cd221c9` |

Observed in the new pod:

| Path | Mode | Bytes | sha256 | Result |
| --- | ---: | ---: | --- | --- |
| `/usr/local/bin/openclaw-ensure-controlui-origin` | `755` | `2009` | `c4151fa9a08ee04c41b212a9b30838f1f19d474fe50b6d2fdc848994d8fba071` | match |
| `/usr/local/bin/openclaw-gateway-with-origin-allowlist` | `755` | `201` | `79910c9dc6a0dcd0d809af1fc21a45052afeae66732d7a0ff4185089e8c3995c` | match |
| `/defaults/openclaw-agent/config.yaml` | `644` | `843` | `bdc8bf155539762c02f37ffbeb27e2dcec48bc5c3badaf4a17ec2edd6cd221c9` | match |

`/defaults/openclaw-agent/config.yaml` marker results:

| Check | Result |
| --- | --- |
| wrapper command present | `true` |
| `--bind lan` present | `true` |
| `--auth token` mode present | `true` |
| unsafe YAML field `allowedOrigins` | count `0` |
| unsafe YAML field `controlUi` | count `0` |
| unsafe YAML field `gateway.controlUi` | count `0` |

The word `token` above is the OpenClaw auth mode flag from the reviewed config, not a secret token value.

## Runtime OpenClaw JSON Config Verification

`/config/.openclaw/openclaw.json` exists after startup:

| Path | Mode | Bytes | sha256 |
| --- | ---: | ---: | --- |
| `/config/.openclaw/openclaw.json` | `600` | `2589` | `94f095147aeeb4c93a7ca377190f0faf10da61d5c7b379808fc0f1d66594c162` |

Redacted JSON structure summary:

```text
openclaw_json_exists=true
openclaw_json_root_object=true
gateway_object=true
controlUi_object=true
gateway.controlUi.allowedOrigins_is_array=true
gateway.controlUi.allowedOrigins_count=3
gateway.controlUi.allowedOrigins_contains_https_localhost_30443=true
```

This proves `gateway.controlUi.allowedOrigins` includes `https://localhost:30443` without printing the full JSON or any secret-bearing value.

## Secret Hygiene

No token value, cookie value, credential, secret, registry credential, bearer value, auth header value, or access URL plaintext is recorded in this evidence.

Authentication material was used only transiently for the normal backend API calls and was not written to this file. The runtime JSON was summarized structurally and was not printed.

## Blockers

None for this gate.

This gate does not claim browser E2E, `passes:true`, Close, or final feature acceptance.

## Recommended Next Gate

Recommended next gate: `Browser/Manual E2E Approval Packet` for authorized GTManager `/control-ui/` root, chat, history fallback, and desktop `/proxy/` regression validation.

## Explicit Negatives

- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no build/tag/push/pull
- no backend deploy/restart
- no registry mutation
- no manual database write or migration
- no database mutation except normal fresh-instance backend API creation flow
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no backend Origin rewrite
- no modification to backend
- no modification to frontend
- no modification to deployments
- no modification to docs
- no modification to longterm
- no modification to AgentTeam
- no modification to spec/plan/tasks
- no modification to existing evidence
- no modification to runtime startup artifact
- no token/cookie/credential/secret/access URL plaintext output
- no Mem0
- no passes:true
- no Close
- no git stage/commit/push

## Verification Plan

Required checks for this evidence:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist.md
rg -n "FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_DONE|FRESH_INSTANCE_RUNTIME_DEPLOYMENT_ORIGIN_ALLOWLIST_BLOCKED|gtclaw-controlui-persistent-origin-allowlist-20260506124656|sha256:64d12df787a9d46a883c23b9870b1763f1a0099e5fee1106d86901b7c666ff45|sha256:6b224147cba6613ebe622564edc9286e80254b370a9da4d2fafd26c65cc83e97|2Gi|Ready|restart|OOM|3001|18789|gateway.controlUi.allowedOrigins|https://localhost:30443|helper scripts|no browser E2E|no build/tag/push/pull|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist.md
```

Also required:

- secret-shape scan on this new evidence with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist.md`

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist.md` | `0` | No whitespace errors. |
| required marker `rg` scan | `0` | Required markers found, including the DONE/BLOCKED verdicts, approved tag, image index digest, linux/arm64 digest, `2Gi`, `Ready`, restart/OOM, `3001`, `18789`, `gateway.controlUi.allowedOrigins`, `https://localhost:30443`, `helper scripts`, `no browser E2E`, `no build/tag/push/pull`, `no passes:true`, and `no Close`. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-fresh-instance-runtime-deployment-origin-allowlist.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |
