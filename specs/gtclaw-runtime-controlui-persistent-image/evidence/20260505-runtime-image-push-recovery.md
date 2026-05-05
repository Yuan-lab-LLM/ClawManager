# Runtime Image Push Recovery Gate

## Verdict

RUNTIME_IMAGE_PUSH_RECOVERY_BLOCKED

The already-built local image was inspected and the approved `localhost:5001` push was retried. Publication remains blocked because the retry again failed on a registry blob `HEAD` request to `localhost:5001`, while read-only diagnostics also showed no reachable registry endpoint on either `localhost:5001` or `127.0.0.1:5001`. The alias path was not used because diagnostics did not support `localhost`-specific IPv6/HTTPS behavior as the only blocker.

## Scope and Approvals

- Dependency approval packet: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-push-recovery-approval-packet.md`
- Prior blocked build/tag/push evidence: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-image-build-tag-push.md`
- Approved existing timestamp: `20260505162033`
- Approved host tag: `localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033`
- Approved in-cluster tag: `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033`
- Approved local image digest: `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`
- Approved linux/arm64 manifest digest from build output: `sha256:48db346b8865e39ececea662ac230cea2618bde7d0b1ed7370b6e736d85949f7`

## Local Image Inspect Before Retry

Command:

```sh
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Sanitized output:

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Command:

```sh
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Sanitized output:

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Result: both existing local tags pointed to the approved `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9` image before retry.

## Read-Only Registry Diagnostics

Command:

```sh
docker ps --filter name=registry --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
<no rows>
```

Command:

```sh
docker ps --filter name=clawmanager-registry --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
<no rows>
```

Command:

```sh
docker ps --filter publish=5001 --format 'ID={{.ID}} Name={{.Names}} Image={{.Image}} Status={{.Status}} Ports={{.Ports}}'
```

Sanitized output:

```text
<no rows>
```

No registry container was found by the approved read-only filters, so `docker logs --tail` was not run.

Command:

```sh
host='localhost:5001'; out=$(curl --head --silent --show-error --max-time 5 --output /dev/null --write-out 'target=localhost:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=7 curl: (7) Failed to connect to localhost port 5001 after 0 ms: Could not connect to server
target=localhost:5001 scheme=http method=HEAD status=000 remote_ip= err=Failed to connect to localhost port 5001 after 0 ms: Could not connect to server
```

Command:

```sh
host='127.0.0.1:5001'; out=$(curl --head --silent --show-error --max-time 5 --output /dev/null --write-out 'target=127.0.0.1:5001 scheme=http method=HEAD status=%{http_code} remote_ip=%{remote_ip} err=%{errormsg}' "http://$host/v2/" 2>&1); rc=$?; printf 'curl_exit=%s %s\n' "$rc" "$out"
```

Sanitized output:

```text
curl_exit=7 curl: (7) Failed to connect to 127.0.0.1 port 5001 after 0 ms: Could not connect to server
target=127.0.0.1:5001 scheme=http method=HEAD status=000 remote_ip= err=Failed to connect to 127.0.0.1 port 5001 after 0 ms: Could not connect to server
```

Diagnostic conclusion: the approved diagnostics did not show a reachable registry endpoint on `localhost:5001` or `127.0.0.1:5001`. Because `127.0.0.1:5001` was also unavailable, the alias path was not justified.

## Retry Push Result

Command:

```sh
docker push localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033
```

Sanitized result:

```text
exit=1
retry push failed while checking an existing registry blob with HEAD against localhost:5001.
final error: dial tcp [::1]:5001: i/o timeout
blob digest in failed HEAD: sha256:89c36d8ca235e87f8a1c057dd3ba9d341671171bf4fe2fca3b6c413acbe0f897
```

The retry was the only push attempted against the approved `localhost:5001` tag.

## Alias Path

No `127.0.0.1:5001` alias tag or push was executed. Diagnostics showed both `localhost:5001` and `127.0.0.1:5001` were not reachable over the approved `/v2/` HEAD checks, so the evidence did not support treating the failure as a localhost-only IPv6/HTTPS blocker.

## Local Image Inspect After Retry

Command:

```sh
docker image inspect localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Sanitized output:

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Command:

```sh
docker image inspect k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033 --format 'RepoTags={{json .RepoTags}}
RepoDigests={{json .RepoDigests}}
Id={{.Id}}
Os={{.Os}}
Architecture={{.Architecture}}'
```

Sanitized output:

```text
RepoTags=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033","localhost:5001/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033"]
RepoDigests=["k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9","localhost:5001/clawmanager-openclaw/openclaw@sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9"]
Id=sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9
Os=linux
Architecture=arm64
```

Result: the local image digest after retry remained `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`. The local tag state did not drift.

## Registry Publication Status

- Publication status: blocked / not accepted for the next fresh instance gate.
- Reason: the approved retry push failed with `dial tcp [::1]:5001: i/o timeout`, and read-only diagnostics showed no reachable registry endpoint on `localhost:5001` or `127.0.0.1:5001`.
- Final image acceptability: not acceptable for direct fresh instance use because successful registry publication was not proven.
- Recommended next gate: `Registry Connectivity Diagnostic Approval Packet`, or external expert escalation if the registry root cause remains unclear under the current mutation limits.

## Negative Proofs

- no docker pull
- no docker build
- no docker rebuild
- no docker run
- no docker create
- no docker cp
- no docker export
- no docker save
- no registry config mutation or restart
- no runtime/K8S/database/browser mutation
- no fresh instance creation, deletion, or modification
- no browser E2E, Chrome DevTools MCP, or Playwright
- no backend/frontend/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modifications
- no reviewed startup artifact modification
- no `/tmp/gtclaw-runtime-patch/**` or `/tmp/gtclaw-startup-source-inspect-y5sCboQ2/**` modification
- no secret/token/cookie/access URL values printed
- no Mem0 write or longterm write
- no passes:true
- no Close
