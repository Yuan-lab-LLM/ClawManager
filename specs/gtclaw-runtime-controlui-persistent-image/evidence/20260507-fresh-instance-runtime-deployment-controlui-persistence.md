# Fresh Instance Runtime Deployment Gate - Control UI Persistence

Worker: FreshInstanceRuntimeDeploymentControlUIPersistenceWorker

Topology: serial

Verdict: `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_BLOCKED`

Gate date: 2026-05-07 Asia/Shanghai

This gate used the approved fresh disposable instance path for the latest control-ui persistence runtime image. It did not run browser E2E, Chrome DevTools, Playwright, browser storage/cache/cookie cleanup, build/tag/push/pull, registry cleanup, old instance deletion, old session deletion, old asset deletion, old tag deletion, source mutation, deployment mutation, longterm write-back, Mem0 write, `passes:true`, Close, git stage, commit, or push.

## Dependency Gates And Target Image

| Gate | Status used |
| --- | --- |
| Fresh Instance Runtime Deployment Approval Packet - Control UI Persistence | `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_APPROVAL_PACKET_CONTROLUI_PERSISTENCE_DONE` |
| Runtime Image Build/Tag/Push - Control UI Persistence | `RUNTIME_IMAGE_BUILD_TAG_PUSH_CONTROLUI_PERSISTENCE_DONE` |
| User approval | `APPROVE_FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_GATE` |

| Field | Value |
| --- | --- |
| target image | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712` |
| expected image index digest | `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908` |
| expected linux/arm64 digest | `sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a` |

Read-only registry preflight:

```text
registry_http=200
registry_content_digest=sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908
registry_linux_arm64_digest=sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a
```

Existing runtime pods before creation did not include the target image. Two old non-target pods were already Pending with `Insufficient memory`.

## Fresh Instance Creation

Because the node did not have enough currently allocatable memory for a 2Gi instance and old instance cleanup was forbidden, this gate created exactly one 1Gi disposable instance through the normal backend API path:

```text
create_http_status=201
created_id=14
created_name=oc1gi-cp-143256
memory_gb=1
pod=clawmanager-user-1/clawreef-14-oc1gi-cp-143256
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
runtime_pods_before=5
runtime_pods_after=6
token_recorded=false
password_recorded=false
```

Authentication material for the normal backend API call was used transiently in process memory only. No token value, password value, key value, credential, cookie value, bearer value, auth header value, or access URL plaintext is recorded in this evidence.

## Pod Ready, Image, Restart, And OOM Evidence

`kubectl wait --for=condition=Ready pod/clawreef-14-oc1gi-cp-143256 -n clawmanager-user-1 --timeout=240s` returned success, so the pod reached Ready transiently. The container then terminated and the final pod state became Failed.

Final pod status:

```text
pod_namespace=clawmanager-user-1
pod_name=clawreef-14-oc1gi-cp-143256
phase=Failed
ready_condition=False
pod_ip=10.42.0.91
node=k3d-clawmanager-server-0
qos=Guaranteed
container=desktop
container_ready=false
restart_count=0
pod_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
pod_imageID=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908
imageID_digest_matches_expected_index=true
oom_status_match_count=1
terminated_started_at=2026-05-07T06:33:00Z
terminated_finished_at=2026-05-07T06:33:13Z
exit_code=137
termination_reason=OOMKilled
```

Events for the new pod show normal schedule, pull, create, and start events:

```text
Scheduled to k3d-clawmanager-server-0
Pulling target image
Pulled target image in 150ms; image size 1802645406 bytes
Created container desktop
Started container desktop
```

The backend API status later reported:

```text
api_instance id=14 name=oc1gi-cp-143256 status=error type=openclaw memory_gb=1
image_registry=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712
pod_namespace=clawmanager-user-1
pod_name=clawreef-14-oc1gi-cp-143256
```

## Service Ports And Endpoint Evidence

The fresh instance Service was created and exposed both required ports:

```text
service_name=clawreef-14-oc1gi-cp-143256-svc
type=ClusterIP
cluster_ip=10.43.70.248
selector={"app":"clawreef","instance-id":"14"}
service_port name=http port=3001 targetPort=3001 protocol=TCP
service_port name=control-ui port=18789 targetPort=18789 protocol=TCP
```

Endpoint state after pod failure:

```text
endpoints_name=clawreef-14-oc1gi-cp-143256-svc
subsets_count=1
addresses=
notReady=10.42.0.91
endpoint_ports=http:3001/TCP,control-ui:18789/TCP
endpointslice_name=clawreef-14-oc1gi-cp-143256-svc-fcjgm
endpoints=10.42.0.91 ready=false serving=false terminating=false
ports=http:3001/TCP,control-ui:18789/TCP
```

## Runtime HTTP And Control UI Reachability

After the OOMKilled termination, runtime HTTP/control-ui reachability failed from inside the cluster:

```text
target_service_ip=10.43.70.248 port=18789 curl_exit=7 http_code=000
symptom=Failed to connect to 10.43.70.248 port 18789: Could not connect to server

target_service_ip=10.43.70.248 port=3001 curl_exit=7 http_code=000
symptom=Failed to connect to 10.43.70.248 port 3001: Could not connect to server
```

The running-container path verification could not be completed because the container was no longer running:

```text
path=/usr/local/lib/node_modules/openclaw/dist/control-ui
kubectl_exec_result=failed
symptom=error: cannot exec into a container in a completed pod; current phase is Failed
running_container_contains_control_ui_path=not_verified_due_to_oom
```

The image build/tag/push dependency evidence had already proven the target image layer contains `/usr/local/lib/node_modules/openclaw/dist/control-ui`, but this gate cannot count that as running-container proof because the fresh runtime container failed.

## Blocker

Primary blocker: the fresh disposable instance could not remain running long enough for runtime HTTP/control-ui and running-container path verification.

Specific blocker evidence:

- The target image was pulled and started by Kubernetes.
- The pod imageID matched the expected image index digest `sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908`.
- The registry linux/arm64 manifest digest matched `sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a`.
- The Service exposed `3001` and `18789`.
- The container terminated after about 13 seconds with `exit_code=137` and `termination_reason=OOMKilled`.
- Final `Pod Ready` was `False`; the API instance status was `error`.
- Service endpoints were not ready and runtime HTTP/control-ui returned connection failures.
- `/usr/local/lib/node_modules/openclaw/dist/control-ui` could not be verified inside a running container because the pod was already Failed.

Last successful step:

```text
Kubernetes scheduled the fresh instance pod, pulled the target image, created and started the desktop container, created the Service, exposed service ports 3001 and 18789, and reported the target imageID digest.
```

Failed command/symptom:

```text
kubectl get pod clawreef-14-oc1gi-cp-143256 -n clawmanager-user-1 -o json
state.terminated.reason=OOMKilled
state.terminated.exitCode=137

curl http://10.43.70.248:18789/
curl_exit=7
http_code=000
symptom=Could not connect to server

kubectl exec -n clawmanager-user-1 clawreef-14-oc1gi-cp-143256 -c desktop -- sh -lc 'test -d /usr/local/lib/node_modules/openclaw/dist/control-ui'
symptom=cannot exec into a container in a completed pod; current phase is Failed
```

GPT Pro escalation: not needed for this gate. The blocker is concrete runtime capacity/OOM evidence, not an unresolved architecture question. The next useful action is a separate approval to free or expand cluster capacity, remove/stop approved disposable old instances, or otherwise create a standard 2Gi fresh instance for this target image.

## Explicit Negatives

- no browser E2E
- no Chrome DevTools
- no Playwright
- no browser storage cleanup
- no browser cache cleanup
- no browser cookie cleanup
- no token/password/key value recorded
- no backend source modification
- no frontend source modification
- no runtime-startup source modification
- no control-ui source modification
- no assembly artifact modification
- no deployment modification
- no docs modification
- no longterm modification
- no AgentTeam modification
- no spec/plan/tasks modification
- no existing evidence modification
- no build/tag/push/pull
- no old instance deletion
- no old session deletion
- no old asset deletion
- no old tag deletion
- no registry cleanup
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push

## Verification Commands

Required checks for this evidence:

```bash
rg -n "FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_DONE|FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_BLOCKED|gtclaw-controlui-persistent-origin-allowlist-persistence-20260506183712|sha256:b36331e5122101a23caa4985b047dced529009e429e91c2dbbc08fd0601e0908|sha256:0616ee761cee6ed35addd9becfe7ec0cbc6df4821c67c9f8a0607490f6e33d0a|Pod Ready|restart|OOM|service ports|/usr/local/lib/node_modules/openclaw/dist/control-ui|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-fresh-instance-runtime-deployment-controlui-persistence.md
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-fresh-instance-runtime-deployment-controlui-persistence.md
secret-shape scan with matched values suppressed
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-fresh-instance-runtime-deployment-controlui-persistence.md
```

## Verification Results

| Command | Exit | Result |
| --- | ---: | --- |
| required marker `rg` scan | `0` | Required markers found, including `FRESH_INSTANCE_RUNTIME_DEPLOYMENT_CONTROLUI_PERSISTENCE_BLOCKED`, target tag, image index digest, linux/arm64 digest, `Pod Ready`, restart/OOM, service ports, `/usr/local/lib/node_modules/openclaw/dist/control-ui`, `no browser E2E`, `no passes:true`, and `no Close`. |
| `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-fresh-instance-runtime-deployment-controlui-persistence.md` | `0` | No whitespace errors. |
| secret-shape scan with matched values suppressed | `0` | `secret_shape_match_count=0`. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260507-fresh-instance-runtime-deployment-controlui-persistence.md` | `0` | Shows only this new evidence file as untracked in the requested path scope. |
