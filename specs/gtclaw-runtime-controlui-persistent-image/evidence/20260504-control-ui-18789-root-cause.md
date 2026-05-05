# Control-UI 18789 Root Cause Evidence - 2026-05-04

## Verdict

`ROOT_CAUSE_DONE`

Root cause identified with read-only evidence: `openclaw-gateway` is running and serves `127.0.0.1:18789`, but it does not listen on the pod/service-facing address. Kubernetes Service and Endpoint metadata correctly expose `18789 -> 18789` and point to the current PodIP, so GTManager proxy requests to the Service ClusterIP are refused by the runtime listener boundary.

Root-cause classification: `wrong listen address`.

This evidence is diagnostic only: no fix, no browser E2E rerun, no passes:true, no Close.

## Gate Statement

Allowed output written:

`specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md`

No runtime instance, pod, container, image/resource setting, Kubernetes resource, database, registry, image, tag, backend, frontend, deployments, docs, longterm, AgentTeam, spec, plan, tasks, existing evidence, Mem0, passes:true, or Close state was mutated. No runtime files were written. No `kubectl cp` was used. No browser E2E was rerun.

Secret hygiene: command output summaries below are redacted and do not record token values, cookie values, credentials, secrets, `.env`, `.codex/auth.json`, `.codex/config.toml`, or token-bearing URLs. One pod `describe` command printed environment values in raw tool output; this evidence intentionally does not reproduce those values.

## Dependency Review

| Evidence | Result used |
| --- | --- |
| `20260504-browser-e2e-approval-packet.md` | Future E2E target was instance `5` / `gtclaw-fresh-20260504095843`; target control-ui port `18789`; no manual pod patch allowed. |
| `20260504-fresh-instance-mutation-and-pod-hash.md` | Fresh pod hash matched all four allowlist files under `/usr/local/lib/node_modules/openclaw/dist/control-ui`; pod image digest matched the approved persistent artifact; restart count was `0` at that gate. |
| `20260504-browser-e2e.md` | Browser E2E gate was `BLOCKED`; `/control-ui/`, `/control-ui/chat?session=main`, and history fallback returned `502`; `/proxy/` desktop regression passed. |
| `20260504-browser-e2e-chrome-devtools-mcp-rerun.md` | Strict Chrome DevTools MCP rerun confirmed all three `/control-ui/` routes returned `502`; redacted error was `dial tcp 10.43.47.127:18789: connect: connection refused`; `/proxy/` desktop regression passed. |

## Commands Run

### Preflight / Review

| Command | Secret-safe output summary |
| --- | --- |
| `wc -l AGENTS.md .specify/memory/constitution.md specs/gtclaw-runtime-controlui-persistent-image/spec.md specs/gtclaw-runtime-controlui-persistent-image/plan.md specs/gtclaw-runtime-controlui-persistent-image/tasks.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md` | Confirmed allowed documents and evidence files exist. |
| `sed -n '1,220p' AGENTS.md` | Confirmed project rules, forbidden actions, and memory/search expectations. |
| `sed -n '1,260p' .specify/memory/constitution.md` | Confirmed E2E gates, no passes:true without evidence, and secret hygiene. |
| `sed -n '1,220p' specs/gtclaw-runtime-controlui-persistent-image/spec.md` | Confirmed persistent image feature scope, fresh instance requirement, and no manual pod patch rule. |
| `sed -n '1,280p' specs/gtclaw-runtime-controlui-persistent-image/plan.md` | Confirmed path/source-of-truth and evidence expectations. |
| `sed -n '1,140p' specs/gtclaw-runtime-controlui-persistent-image/tasks.md` | Confirmed no implementation, no mutation, and Browser E2E gate boundaries. |
| `sed -n '1,230p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-approval-packet.md` | Confirmed target instance/pod/image/digests and future E2E criteria. |
| `sed -n '1,190p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-fresh-instance-mutation-and-pod-hash.md` | Confirmed fresh instance `5`, imageID digest, restart count `0`, no manual pod patch, and hash match. |
| `sed -n '1,130p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e.md` | Confirmed prior Browser E2E `BLOCKED` and route set. |
| `sed -n '1,210p' specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md` | Confirmed strict MCP rerun `BLOCKED`, `502`, and `connection refused` target. |
| `test -e specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md; printf '%s\n' $?` | File did not exist before this write. |
| `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md` | Before this evidence write, only the existing strict MCP rerun evidence showed as untracked in this path-limited check. |

### Pod / Service / Endpoint Metadata

| Command | Secret-safe output summary |
| --- | --- |
| `kubectl get pod clawreef-5-gtclaw-fresh-20260504095843 -n clawmanager-user-1 -o wide --show-labels` | Pod was `1/1 Running`, `READY`, `RESTARTS=0`, PodIP `10.42.0.61`, labels include `app=clawreef`, `instance-id=5`, `instance-name=gtclaw-fresh-20260504095843`. |
| `kubectl get pod clawreef-5-gtclaw-fresh-20260504095843 -n clawmanager-user-1 -o jsonpath='{.status.phase}{"\n"}{.status.podIP}{"\n"}{range .status.containerStatuses[*]}{.name}{" ready="}{.ready}{" restartCount="}{.restartCount}{" state="}{.state}{" image="}{.image}{" imageID="}{.imageID}{"\n"}{end}'` | Phase `Running`; PodIP `10.42.0.61`; container `desktop ready=true restartCount=0`; image and imageID matched the approved persistent artifact digest. |
| `kubectl get pod clawreef-5-gtclaw-fresh-20260504095843 -n clawmanager-user-1 -o jsonpath='{.metadata.labels}'` | Labels match Service selector keys for `app=clawreef` and `instance-id=5`. |
| `kubectl describe pod clawreef-5-gtclaw-fresh-20260504095843 -n clawmanager-user-1` | Pod was Running/Ready with no Events; container only declared port `3001/TCP`; restart count `0`. Environment values are not recorded here. |
| `kubectl get svc -n clawmanager-user-1 -l instance-id=5 -o wide --show-labels` | Two instance-5 Services exist. Fresh service `clawreef-5-gtclaw-fresh-20260504095843-svc` has ClusterIP `10.43.47.127`, ports `3001/TCP,18789/TCP`, selector `app=clawreef,instance-id=5`. Duplicate `clawreef-5-instance-5-svc` has ClusterIP `10.43.30.23`, same selector and same target pod. |
| `kubectl get endpoints -n clawmanager-user-1 -l instance-id=5 -o wide` | Both Services resolve to `10.42.0.61:3001` and `10.42.0.61:18789`. |
| `kubectl get endpointslice -n clawmanager-user-1 -l instance-id=5 -o wide` | EndpointSlices list endpoint `10.42.0.61` with ports `3001,18789`. |
| `kubectl get svc -n clawmanager-user-1 -l instance-id=5 -o jsonpath='{range .items[*]}{.metadata.name}{" clusterIP="}{.spec.clusterIP}{" selector="}{.spec.selector}{" ports="}{range .spec.ports[*]}{.name}{":"}{.port}{"->"}{.targetPort}{"/"}{.protocol}{" "}{end}{"\n"}{end}'` | Fresh service: `clusterIP=10.43.47.127`, `http:3001->3001/TCP`, `control-ui:18789->18789/TCP`. Duplicate service: `clusterIP=10.43.30.23`, `control-ui:18789->18789/TCP`, `tcp-3001:3001->3001/TCP`. |
| `kubectl get endpointslice clawreef-5-gtclaw-fresh-20260504095843-svc-w2fbd -n clawmanager-user-1 -o jsonpath='{.metadata.name}{"\n"}{range .ports[*]}{.name}{":"}{.port}{"/"}{.protocol}{"\n"}{end}{range .endpoints[*]}addr={.addresses[*]} ready={.conditions.ready} serving={.conditions.serving} terminating={.conditions.terminating} targetRef={.targetRef.name}{"\n"}{end}'` | Fresh service EndpointSlice has `http:3001/TCP`, `control-ui:18789/TCP`, endpoint `10.42.0.61`, `ready=true`, `serving=true`, `terminating=false`, targetRef current pod. |
| `kubectl get svc clawreef-5-gtclaw-fresh-20260504095843-svc -n clawmanager-user-1 -o yaml` | Fresh service selector and targetPort are correct; ClusterIP is `10.43.47.127`; `control-ui` maps `18789 -> 18789`. |
| `kubectl get svc clawreef-5-instance-5-svc -n clawmanager-user-1 -o yaml` | Duplicate service also maps `18789 -> 18789` and points to the same pod selector; not the root cause. |

### Runtime Process / Listener / Probe Metadata

| Command | Secret-safe output summary |
| --- | --- |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'command -v ss; ss -ltnp 2>/dev/null'` | `ss` unavailable in the runtime image. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'command -v netstat; netstat -ltnp 2>/dev/null'` | `netstat` unavailable in the runtime image. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- ps -eo pid,ppid,stat,comm,args` | `openclaw-agent`, `openclaw`, and `openclaw-gateway` processes were present; no evidence of crash or restart loop. Chromium inside the desktop was launched against `http://localhost:18789`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'command -v curl; curl -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://127.0.0.1:18789/'` | `curl` exists; `127.0.0.1:18789` returned HTTP `200`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'curl -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://10.42.0.61:18789/'` | Discarded as reachability evidence because proxy environment routed the request to the egress proxy and returned HTTP `400`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'curl -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://10.43.47.127:18789/'` | Discarded as reachability evidence because proxy environment routed the request to the egress proxy and returned HTTP `400`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'curl -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://10.43.30.23:18789/'` | Discarded as reachability evidence because proxy environment routed the request to the egress proxy and returned HTTP `400`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'grep -H -i ":4965" /proc/net/tcp /proc/net/tcp6 2>/dev/null'` | `18789` (`0x4965`) LISTEN sockets were only loopback: `0100007F:4965` (`127.0.0.1:18789`) and loopback tcp6 form. No `0.0.0.0:18789` and no PodIP `10.42.0.61:18789` listener were present. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'curl --noproxy "*" -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://10.42.0.61:18789/'` | PodIP `10.42.0.61:18789` failed immediately with curl `(7)` could not connect; HTTP `000`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'curl --noproxy "*" -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://10.43.47.127:18789/'` | Fresh Service ClusterIP `10.43.47.127:18789` failed immediately with curl `(7)` could not connect; HTTP `000`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'curl --noproxy "*" -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://10.43.30.23:18789/'` | Duplicate Service ClusterIP `10.43.30.23:18789` failed immediately with curl `(7)` could not connect; HTTP `000`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'for inode in 6408779 6408780; do for fd in /proc/[0-9]*/fd/*; do target=$(readlink "$fd" 2>/dev/null || true); case "$target" in socket:\[$inode\]) pid=${fd#/proc/}; pid=${pid%%/*}; printf "%s " "$inode"; tr "\000" " " < /proc/$pid/cmdline; printf "\n";; esac; done; done'` | Listener socket inodes for `18789` map to `openclaw-gateway`. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'curl --noproxy "*" -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://10.42.0.61:3001/'` | PodIP `10.42.0.61:3001` connected immediately and returned HTTP `400`, proving PodIP connectivity to the desktop listener path. |
| `kubectl exec -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop -- sh -lc 'curl --noproxy "*" -sS -o /dev/null -w "%{http_code} %{remote_ip}:%{remote_port} %{time_connect}\n" --max-time 3 http://10.43.47.127:3001/'` | Fresh Service ClusterIP `10.43.47.127:3001` connected immediately and returned HTTP `400`, proving Service routing reaches the pod for an exposed listener. |
| `kubectl get networkpolicy -n clawmanager-user-1 -o wide` | Instance `5` has one NetworkPolicy: `clawreef-5-gtclaw-fresh-20260504095843-netpol`. |
| `kubectl get networkpolicy -n clawmanager-system -o wide` | No NetworkPolicy resources in `clawmanager-system`. |
| `kubectl get networkpolicy clawreef-5-gtclaw-fresh-20260504095843-netpol -n clawmanager-user-1 -o yaml` | Policy is egress-only for the runtime pod selector. It does not define ingress denial for GTManager to pod/service; desktop `3001` Service reachability confirms this is not the 18789 blocker. |
| `kubectl logs -n clawmanager-user-1 clawreef-5-gtclaw-fresh-20260504095843 -c desktop --tail=600 \| rg -n "openclaw\|gateway\|18789\|listen\|listening\|bind\|control-ui\|Control UI\|error\|panic\|crash\|failed\|Started\|starting"` | Runtime logs show gateway startup succeeded. Key safe lines: `host mounted at http://127.0.0.1:18789/__openclaw__/canvas/`, `ready`, and browser control on `http://127.0.0.1:18791/`. No crash/panic evidence for `openclaw-gateway`. |

## Listener / Probe Matrix For 18789

| Probe | Result | Interpretation |
| --- | --- | --- |
| Pod process list | `openclaw-agent`, `openclaw`, and `openclaw-gateway` running | Runtime process did not crash. |
| `/proc/net/tcp*` listener scan for `:4965` | `127.0.0.1:18789` loopback listeners only | Wrong listen address; no pod-facing listener. |
| Socket inode to process mapping | `18789` listener sockets owned by `openclaw-gateway` | The gateway is the process serving loopback 18789. |
| `curl http://127.0.0.1:18789/` inside pod | HTTP `200` | Control-ui upstream is healthy on loopback. |
| `curl --noproxy "*" http://10.42.0.61:18789/` inside pod | curl `(7)`, HTTP `000`, could not connect | PodIP path refused because no PodIP listener. |
| `curl --noproxy "*" http://10.43.47.127:18789/` inside pod | curl `(7)`, HTTP `000`, could not connect | Fresh Service ClusterIP path refused because endpoint forwards to a non-listening PodIP port. |
| `curl --noproxy "*" http://10.43.30.23:18789/` inside pod | curl `(7)`, HTTP `000`, could not connect | Duplicate Service also fails for the same listener reason. |
| `curl --noproxy "*" http://10.42.0.61:3001/` inside pod | Connects immediately, HTTP `400` | PodIP networking is not generally broken. |
| `curl --noproxy "*" http://10.43.47.127:3001/` inside pod | Connects immediately, HTTP `400` | Service routing is not generally broken. |

## Pod / Service / App Metadata Summary

| Item | Current evidence |
| --- | --- |
| Pod state | `clawmanager-user-1/clawreef-5-gtclaw-fresh-20260504095843` is `Running`, Ready `1/1`, restart count `0`. |
| PodIP | Current PodIP is `10.42.0.61`. This does not match the browser `502` IP because the browser error IP is the Service ClusterIP, not the PodIP. |
| Fresh Service | `clawreef-5-gtclaw-fresh-20260504095843-svc` has ClusterIP `10.43.47.127`, selector `app=clawreef,instance-id=5`, and ports `http:3001->3001/TCP`, `control-ui:18789->18789/TCP`. |
| Endpoint / EndpointSlice | Endpoint address is current PodIP `10.42.0.61`, `ready=true`, `serving=true`, `terminating=false`; ports include `18789`. |
| Duplicate Service | `clawreef-5-instance-5-svc` has ClusterIP `10.43.30.23`, same selector and same endpoint. It is not the observed browser target for the strict rerun, and it also fails 18789 because the pod listener is loopback-only. |
| App pod | `clawmanager-system/clawmanager-app-6c985497f5-2kdq8` is `Running`, Ready `1/1`, restart count `0`. |
| App Service | No `app=clawmanager-app` Service resource was found in `clawmanager-system` by that label selector. App logs were still available from the pod. |

## Proxy Route / Error Confirmation

Command:

```bash
kubectl logs -n clawmanager-system clawmanager-app-6c985497f5-2kdq8 --since=6h | rg -n "10\\.43\\.47\\.127:18789|18789|control-ui|connection refused|Failed to proxy|failed to proxy|dial tcp|clawreef-5|instance 5|Instance 5"
```

Secret-safe summary:

- Instance `5` service creation was logged with ClusterIP `10.43.47.127`.
- GTManager access generation for `mode=control-ui` returned HTTP `200`.
- Proxy route target was `http://10.43.47.127:18789/...`.
- The three required route classes logged `502` with `dial tcp 10.43.47.127:18789: connect: connection refused`:
  - `/api/v1/instances/5/control-ui/`
  - `/api/v1/instances/5/control-ui/chat?session=main`
  - `/api/v1/instances/5/control-ui/history-fallback-check`
- Later strict browser lines at `07:39:06` to `07:39:07 UTC` repeat the same `connection refused` class.
- No token value, cookie value, or token-bearing URL is recorded here.

This confirms GTManager proxy target resolution selected the Service ClusterIP `10.43.47.127` and port `18789`. That target is consistent with Kubernetes Service metadata. The proxy target is not the wrong PodIP/port/path; the refused connection is explained by the runtime listener binding only to loopback.

## Historical Instance 3 T8I Contrast

Historical comparison command:

```bash
rg -n "Instance|instance|clawreef-3|control-ui|18789|/proxy/|GTClaw 控制台|zh-CN|Browser|route|desktop|T8I|Verdict|pass|502|localhost:18789|127\\.0\\.0\\.1:18789" specs/gtmanager-gtclaw-m1-runtime-localization/evidence/20260502-t8i-e2e-rerun-after-runtime-body-fix.md
```

Historical behavior is baseline only, not current pass evidence.

Relevant contrast:

| Surface | Historical instance `3` T8I | Fresh instance `5` current evidence |
| --- | --- | --- |
| GTManager `/control-ui/` routes | Root, chat, and history fallback returned HTTP `200` and rendered `GTClaw 控制台`, `zh-CN`, and Chinese markers. | Root, chat, and history fallback return HTTP `502` with `connection refused`. |
| Runtime 18789 loopback | `127.0.0.1:18789` returned HTTP `200`. | `127.0.0.1:18789` returns HTTP `200`. |
| Runtime 18789 PodIP | Historical `10.42.0.57:18789` returned HTTP `200`. | Current `10.42.0.61:18789` is refused. |
| Runtime 18789 ClusterIP | Historical `10.43.115.105:18789` returned HTTP `200`. | Current `10.43.47.127:18789` is refused. |
| `/proc/net/tcp` listener | Historical evidence included both `127.0.0.1:18789` and PodIP `10.42.0.57:18789`. | Current evidence only shows loopback listeners for `18789`; no PodIP listener. |
| Desktop regression | Historical `/proxy/` passed. | Current `/proxy/` passes. |

This isolates the behavior difference to the 18789 listen address, not to the patched static files, route cookies, desktop proxy route, or general service routing.

## Required Questions

1. Pod still Running/Ready and restart count still `0`: yes. Current pod is Running, Ready `1/1`, restart count `0`.
2. PodIP vs `10.43.47.127`: current PodIP is `10.42.0.61`; `10.43.47.127` is the fresh Service ClusterIP. The Service endpoint points to PodIP `10.42.0.61`.
3. Kubernetes Service exposes control-ui `18789`: yes. Fresh Service maps `control-ui:18789 -> 18789/TCP`; selector and EndpointSlice are correct.
4. Runtime container has a process listening on `18789`: yes, `openclaw-gateway` listens on `127.0.0.1:18789`, but not on PodIP or `0.0.0.0`.
5. If no pod-facing listener, process state: `openclaw-gateway` is started and ready; no crash evidence; it is listening on loopback only. It is not listening on another service-facing port for control-ui evidence; `18789` is loopback-only.
6. If pod-local `127.0.0.1:18789` reachable but Service/PodIP not: confirmed wrong listen address. Service targetPort is correct. NetworkPolicy is egress-only and desktop `3001` PodIP/ServiceIP connectivity works, so NetworkPolicy/iptables is not the identified blocker.
7. If pod-local and PodIP both reachable but GTManager proxy still `502`: not this case. PodIP `18789` is refused. Backend target resolution selected the correct Service ClusterIP/port/path according to app logs.
8. Historical instance `3` T8I comparison: instance `3` had 18789 reachable on loopback, PodIP, and ClusterIP, and `/proc/net/tcp` showed both loopback and PodIP listeners. Instance `5` only has loopback listeners.
9. Root-cause classification: `wrong listen address`.

## Classification Matrix

| Candidate | Classification |
| --- | --- |
| runtime process not listening | Not exact. `openclaw-gateway` listens on `127.0.0.1:18789`; it is not listening on the pod/service-facing address. |
| runtime process crashed | Rejected. Process list and logs show `openclaw-gateway` running and ready. |
| wrong listen address | Identified root cause. Listener is loopback-only; Service forwards to PodIP/ClusterIP. |
| wrong Service/endpoint | Rejected. Service selector, targetPort, EndpointSlice address, ready/serving state, and 3001 Service reachability are correct. |
| wrong proxy target resolution | Rejected. GTManager selected `10.43.47.127:18789`, matching fresh Service ClusterIP and target port. |
| transient pod state | Rejected by current Running/Ready status, restart count `0`, stable loopback success, stable PodIP/ServiceIP refusal, and repeated browser evidence. |
| unknown / needs external expert | Not needed for root-cause identification. External review may still be useful for deciding the safest fix design. |

## Next Recommended Gate Prompt Type

Recommended next gate prompt type: `RuntimeControlUI18789FixApprovalPacket`.

The next prompt should authorize a separate fix investigation/implementation packet only after Commander review. It should require the fixer to choose one narrow approach, such as making `openclaw-gateway` bind a pod/service-facing address or changing the control-ui proxy topology to target a reachable loopback bridge, and then collect fresh route evidence. This root-cause task implemented no fix.

Required next-gate exclusions unless explicitly approved: no browser E2E until after fix deployment/selection is authorized, no runtime pod manual patch as acceptance evidence, no passes:true, no Close.

## Verification Commands

Required verification for this evidence file:

```bash
git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md
rg -n "ROOT_CAUSE_DONE|BLOCKED|clawreef-5-gtclaw-fresh-20260504095843|10\\.43\\.47\\.127|18789|control-ui|connection refused|listener|Service|Endpoint|PodIP|proxy target|no fix|no browser E2E|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md
git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-control-ui-18789-root-cause.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260504-browser-e2e-chrome-devtools-mcp-rerun.md
```
