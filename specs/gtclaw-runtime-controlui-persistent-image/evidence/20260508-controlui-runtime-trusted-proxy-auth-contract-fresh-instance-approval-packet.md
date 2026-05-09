# Control UI runtime trusted-proxy auth contract fresh-instance approval packet

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology, approval packet
Gate: CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_APPROVAL_PACKET

## Verdict

CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_APPROVAL_PACKET_DONE

This is an approval packet only. It did not create, delete, or modify any instance, pod, Service, PVC, database row, runtime image, source artifact, browser session, deployment, longterm file, Mem0 memory, or git state.

## Requested approval token

APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_GATE

The later fresh-instance gate must not proceed unless Commander/user provides this exact token.

## Dependency gates

Reviewed dependency status:

| Gate | Status used |
| --- | --- |
| CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_IMAGE_CONFIG_DONE | Done; final trusted-proxy runtime image tag and digest produced. |
| CONTROLUI_AUTH_CONTRACT_BACKEND_RUNTIME_DELIVERY_IMPLEMENTATION_BLOCKED | Backend delivery complete, but old running runtime lacked trusted-proxy readiness. |
| CONTROLUI_DEVICE_SIGNATURE_AUTH_CONTRACT_IMPLEMENTATION_DONE | Backend bridge sanitizer done; runtime trusted-proxy readiness still required. |
| CONTROLUI_LOCALIZATION_BROWSER_MANUAL_E2E_RERUN_DONE | Browser route/localization observed; known `device signature invalid` blocker recorded. |

Evidence reviewed:

- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-image-config.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-auth-contract-backend-runtime-delivery-implementation.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-device-signature-auth-contract-implementation.md`
- `specs/gtclaw-runtime-controlui-persistent-image/evidence/controlui-localization-browser-manual-e2e-rerun.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-image-assembly-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md`
- `specs/gtclaw-runtime-controlui-persistent-image/runtime-startup-artifact/20260508-trusted-proxy-auth-contract/MANIFEST.md`

## Exact runtime image identity

The later fresh-instance gate is approved to target only this runtime image identity:

| Field | Value |
| --- | --- |
| cluster tag | `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130` |
| host tag used for prior build/push evidence | `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130` |
| image index digest | `sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010` |
| linux/arm64 manifest digest | `sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb` |
| expected OpenClaw package | `openclaw@2026.4.14` |

Do not use the superseded candidate `gtclaw-controlui-localization-trusted-proxy-20260508125928` / `sha256:187153d19538655e56016c51f7da0f23874eac1600aae005b1d2411043958087`.

## Fresh instance scope

The later gate scope is exactly one new standard 2Gi OpenClaw runtime instance using the exact target runtime image above.

Required scope constraints:

- create exactly one new standard `2Gi` runtime instance through the normal ClawManager path;
- prove exactly one new instance ID appears after creation;
- prove the instance type is OpenClaw and its runtime image is the target tag/digest above;
- do not create a second instance for retry without a new approval packet;
- do not repurpose old instance `16 / oc2gi-loc-221427` as the success target unless the gate is explicitly re-scoped to a current-instance readback gate;
- do not run browser E2E in this gate.

## Required readiness checks

The later fresh-instance gate must collect these readiness facts for the new instance:

| Area | Required evidence |
| --- | --- |
| Pod | Pod phase `Running`; Pod condition `Ready=True`; target container ready. |
| Restart/OOM | restart count `0` after stability window; no `OOMKilled` in current or last state; no OOM event. |
| Image | pod image equals target cluster tag; imageID/digest resolves to the expected image index or platform digest above. |
| Service ports | Service exposes `http` `3001->3001/TCP` and `control-ui` `18789->18789/TCP`; EndpointSlice points to the new pod and is ready. |
| Runtime HTTP | `18789` returns HTTP `200` from loopback, PodIP, and ServiceIP where feasible. |
| Instance freshness | new instance ID/name, pod name, namespace, service name, and memory size are recorded; token/password/access URL values are not recorded. |

## Running-container readback

The later gate must read back from the running container, not only from build artifacts.

Runtime patch proof:

- `/usr/local/share/gtclaw/trusted-proxy-auth-contract` exists and is directory-executable;
- packaged verifier exists and exits `0` against `/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js`;
- gateway bundle contains `isGtManagerMediatedControlUiAuth`;
- gateway bundle reads `x-forwarded-prefix` from the WebSocket upgrade request;
- original direct-client protections remain visible, including `device signature invalid`, `verifyDeviceSignature`, `resolveConnectAuthDecision`, `bootstrapTokenCandidate`, `verifyDeviceToken`, and `shouldSkipControlUiPairing`.

Startup config proof:

- `/defaults/openclaw-agent/config.yaml` uses `/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract`;
- `/usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract` exists and is executable;
- startup still launches `openclaw gateway run --bind lan --auth token`;
- sanitized config readback prints only keys, modes, booleans, and counts, never token/password/header/cookie values.

zh-CN Control UI hash proof:

| File | Expected sha256 |
| --- | --- |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/index.html` | `b26c425c6fdb6295779765ca8f3c90b661d5953b54a63a40ebe8e8ddeb1abcec` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js` | `d9dbbba83a930be1c0f60ed04b6247eb006a90f3dbb90e46676ad62c82d95648` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js` | `3c025ee187a558f73375360bd67c7ffac7e6a9a403847d983aeb46065d282b63` |
| `/usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js` | `cdb41aeb1ddecb6767abe41b4d00742cb53157e2be2cade7714bea3cf1ebbe2f` |

## Direct-spoof and runtime-contract evidence

Collect without browser where feasible:

- run the packaged verifier from inside the running container and record exit `0`;
- record that the verifier rejects a direct client JSON trusted-proxy marker without the backend route prefix;
- record that the verifier rejects shared auth without the route prefix and with the wrong route prefix;
- record that the verifier rejects non-operator and non-Control UI clients even when the route prefix exists;
- record that the patch does not read `connectParams.trustedProxy`, `connectParams.auth.trustedProxy`, or any browser-controlled JSON marker as standalone proof;
- if a dynamic direct WebSocket spoof probe can be run without printing credentials, run it with output redacted and record only accept/reject status and close/error class;
- if a dynamic direct spoof probe cannot be run without exposing credential material, do not run it; record `dynamic_direct_spoof_probe=not_feasible_without_secret_exposure` and rely on packaged verifier plus running-bundle readback for this gate.

No browser, DevTools, Playwright, token-bearing URL, auth header value, cookie value, or password value is allowed for this evidence.

## Capacity preflight and cleanup boundary

Before creating the one fresh instance, the later gate must run read-only capacity preflight:

- node allocatable memory and current requested memory;
- current OpenClaw runtime pods, memory requests, status, age, image, and readiness;
- existing Services/PVCs only as inventory, not cleanup targets;
- available request headroom sufficient for one standard `2Gi` instance.

If capacity is sufficient:

- create exactly one new standard `2Gi` instance and continue readiness checks.

If capacity is insufficient before instance creation:

- do not create the instance;
- do not delete or modify any existing instance, pod, Service, PVC, database row, session, asset, image tag, or registry content under this approval token;
- return `CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_BLOCKED: insufficient capacity before creation`;
- request a separate cleanup/capacity approval packet that names the exact cleanup candidate and allowed object scope.

If the one fresh instance is created and then fails readiness:

- record the failure with sanitized evidence;
- do not create a replacement instance under this token;
- cleanup may affect only the new instance and its directly owned pod/Service/PVC if Commander/user explicitly approves cleanup in the fresh-instance execution gate notes;
- no historical disposable workload, active user workload, old Service, old PVC, database row, session, asset, image tag, or registry content may be deleted under this packet.

## Why browser rerun waits

browser rerun must wait until after this fresh-instance readiness gate because:

- the backend delivery gate is healthy but was blocked on runtime readiness;
- the localization browser rerun already proved route and zh-CN shell visibility, but also recorded the known `device signature invalid` blocker;
- the current old instance `16 / oc2gi-loc-221427` is not running the trusted-proxy runtime image;
- browser evidence before fresh/current runtime readback would not prove the target image/config is actually in use;
- a browser gate should test only after backend delivery, target runtime image readiness, running-container contract proof, and stale-route ambiguity are eliminated.

The later browser gate must be separate from this fresh-instance gate.

## Future gate verification commands

These commands are for the later approved fresh-instance gate. They were not executed by this packet.

Set sanitized variables after the one instance is created:

```bash
export NS='clawmanager-user-1'
export NEW_INSTANCE_ID='<new-instance-id>'
export NEW_INSTANCE_NAME='<new-instance-name>'
export POD='<new-pod-name>'
export SVC='<new-service-name>'
export TARGET_IMAGE='k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-trusted-proxy-20260508130130'
export TARGET_INDEX_DIGEST='sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010'
export TARGET_ARM64_DIGEST='sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb'
```

Read-only capacity preflight:

```bash
kubectl get nodes -o custom-columns=NAME:.metadata.name,ALLOCATABLE_MEMORY:.status.allocatable.memory,CAPACITY_MEMORY:.status.capacity.memory
kubectl -n "$NS" get pods -l app=clawreef -o custom-columns=NAME:.metadata.name,PHASE:.status.phase,READY:.status.containerStatuses[0].ready,RESTARTS:.status.containerStatuses[0].restartCount,IMAGE:.spec.containers[0].image,MEMORY:.spec.containers[0].resources.requests.memory
kubectl -n "$NS" get svc,pvc --sort-by=.metadata.creationTimestamp
```

Pod Running/Ready, restart, OOM, and image:

```bash
kubectl -n "$NS" wait --for=condition=Ready "pod/$POD" --timeout=240s
kubectl -n "$NS" get pod "$POD" -o jsonpath='{.status.phase}{"\n"}{range .status.conditions[*]}{.type}={.status}{"\n"}{end}{range .status.containerStatuses[*]}{.name} ready={.ready} restarts={.restartCount} imageID={.imageID}{"\n"}{end}'
kubectl -n "$NS" describe pod "$POD" | rg -n 'OOMKilled|Back-off|Failed|Killing|Started|Created|Pulled|Container image already present|Successfully assigned' || true
kubectl -n "$NS" get pod "$POD" -o jsonpath='{.spec.containers[?(@.name=="desktop")].image}{"\n"}'
```

Service ports and endpoints:

```bash
kubectl -n "$NS" get svc "$SVC" -o jsonpath='{range .spec.ports[*]}{.name}{" "}{.port}{"->"}{.targetPort}{"/"}{.protocol}{"\n"}{end}'
kubectl -n "$NS" get endpoints "$SVC" -o wide
kubectl -n "$NS" get endpointslice -l "kubernetes.io/service-name=$SVC" -o jsonpath='{range .items[*].endpoints[*]}{.addresses} ready={.conditions.ready} serving={.conditions.serving}{"\n"}{end}'
```

Runtime `18789` HTTP 200:

```bash
POD_IP="$(kubectl -n "$NS" get pod "$POD" -o jsonpath='{.status.podIP}')"
SVC_IP="$(kubectl -n "$NS" get svc "$SVC" -o jsonpath='{.spec.clusterIP}')"
kubectl -n "$NS" exec "$POD" -c desktop -- curl --noproxy '*' -sS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:18789/
kubectl -n "$NS" exec "$POD" -c desktop -- curl --noproxy '*' -sS -o /dev/null -w '%{http_code}\n' "http://$POD_IP:18789/"
kubectl -n "$NS" exec "$POD" -c desktop -- curl --noproxy '*' -sS -o /dev/null -w '%{http_code}\n' "http://$SVC_IP:18789/"
```

Running-container contract and startup readback:

```bash
kubectl -n "$NS" exec "$POD" -c desktop -- sh -lc '
set -eu
test -d /usr/local/share/gtclaw/trusted-proxy-auth-contract
test -x /usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract
node /usr/local/share/gtclaw/trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs \
  /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js \
  /usr/local/share/gtclaw/trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
grep -q "isGtManagerMediatedControlUiAuth" /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js
grep -q "x-forwarded-prefix" /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js
grep -q "openclaw-gateway-with-gtmanager-auth-contract" /defaults/openclaw-agent/config.yaml
grep -q "openclaw gateway run --bind lan --auth token" /usr/local/bin/openclaw-gateway-with-gtmanager-auth-contract
'
```

zh-CN Control UI hash readback:

```bash
kubectl -n "$NS" exec "$POD" -c desktop -- sha256sum \
  /usr/local/lib/node_modules/openclaw/dist/control-ui/index.html \
  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/index-M4TNVXB3.js \
  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/i18n-B06L7jQN.js \
  /usr/local/lib/node_modules/openclaw/dist/control-ui/assets/zh-CN-B26mMdbY.js
```

Sanitized direct-spoof/runtime-contract evidence:

```bash
kubectl -n "$NS" exec "$POD" -c desktop -- sh -lc '
set -eu
node /usr/local/share/gtclaw/trusted-proxy-auth-contract/verify-trusted-proxy-contract.mjs \
  /usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js \
  /usr/local/share/gtclaw/trusted-proxy-auth-contract/patch-openclaw-trusted-proxy-contract.mjs
node -e "
const fs=require(\"fs\");
const p=\"/usr/local/lib/node_modules/openclaw/dist/server.impl-BbJvXoPb.js\";
const s=fs.readFileSync(p,\"utf8\");
const required=[\"isGtManagerMediatedControlUiAuth\",\"x-forwarded-prefix\",\"device signature invalid\",\"verifyDeviceSignature\",\"resolveConnectAuthDecision\",\"bootstrapTokenCandidate\",\"verifyDeviceToken\",\"shouldSkipControlUiPairing\"];
const forbidden=[\"connectParams.trustedProxy\",\"connectParams.auth.trustedProxy\"];
for (const m of required) if (!s.includes(m)) throw new Error(\"missing \"+m);
for (const m of forbidden) if (s.includes(m)) throw new Error(\"forbidden \"+m);
console.log(\"static_runtime_contract_markers_ok\");
"
'
```

## Verification commands for this approval packet

These checks apply to this packet-writing gate:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance-approval-packet.md
git diff --no-index --check -- /dev/null specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance-approval-packet.md
rg -n "CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_APPROVAL_PACKET_DONE|APPROVE_CONTROLUI_RUNTIME_TRUSTED_PROXY_AUTH_CONTRACT_FRESH_INSTANCE_GATE|sha256:92ffef1bc993ac17def09b0d05ef37d1ff2d34bb3f9fc24ac72ad185d395d010|sha256:474dab6f0ac469090dc02eeb10b474f80a9480f76ebd6631ce3ab6ab62dc25fb|exactly one new standard.*2Gi|Pod.*Running|Ready|OOM|Service ports|18789.*HTTP.*200|running-container|direct-spoof|capacity preflight|browser rerun must wait|no browser E2E|no image build/tag/push/pull|no passes:true|no Close|no git stage/commit/push" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance-approval-packet.md
rg -n -i -o "(api[_-]?key|access[_-]?token|gateway[_-]?token|password|secret|authorization|bearer|jwt|private[_-]?key)[[:space:]]*[:=][[:space:]<>]*[^[:space:]<>]{8,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|https?://[^[:space:]]*[?&](token|access|auth|password|key)=" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance-approval-packet.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-runtime-trusted-proxy-auth-contract-fresh-instance-approval-packet.md
```

## Verification results

| Command | Exit | Result |
| --- | ---: | --- |
| `git diff --check -- ...fresh-instance-approval-packet.md` | `0` | No whitespace errors reported. |
| `git diff --no-index --check -- /dev/null ...fresh-instance-approval-packet.md` | `1` | No output and no whitespace diagnostics; exit `1` is the expected no-index difference status for a new file. |
| required marker `rg` scan | `0` | Required markers found, including verdict, approval token, target digests, exactly one standard 2Gi instance, Pod Running/Ready, OOM, Service ports, 18789 HTTP 200, running-container readback, direct-spoof evidence, capacity preflight, browser rerun boundary, and forbidden actions. |
| sensitive/access URL shape scan piped to `wc -l` | `0` | Output was `0`. |
| `git status --short -- ...fresh-instance-approval-packet.md` | `0` | Shows only this new approval packet as untracked in the requested path scope. |

## Forbidden actions confirmation

This packet-writing gate performed only read-only evidence review and wrote this one file.

It did not perform:

- no instance creation, deletion, or modification
- no pod creation, deletion, or modification
- no Service creation, deletion, or modification
- no PVC creation, deletion, or modification
- no kubectl mutation
- no k3d mutation
- no Helm mutation
- no browser E2E
- no DevTools
- no Playwright
- no image build/tag/push/pull
- no backend source or artifact edits
- no frontend source or artifact edits
- no runtime source or artifact edits
- no database mutation
- no Mem0 write
- no `passes:true`
- no Close
- no longterm write-back
- no git stage/commit/push
- no token/password/key/cookie/bearer/auth header/access URL plaintext recorded
