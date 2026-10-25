# clawmanager-agent V2 Contract

## Purpose

The agent runs inside each shared OpenClaw or Hermes runtime Pod. It manages gateway subprocesses, ports, cgroups, Linux users, workspace quota, health checks, skill/status reporting, and metrics.

## Agent-To-Backend Reports

All report requests use header `X-ClawManager-Agent-Token`.

### POST /api/v1/runtime-agent/register

Request body:

```json
{
  "runtime_type": "openclaw",
  "namespace": "clawmanager-system",
  "pod_name": "openclaw-runtime-6f77f8b8c7-abcde",
  "pod_uid": "pod-uid",
  "pod_ip": "10.42.0.31",
  "node_name": "node-a",
  "deployment_name": "openclaw-runtime",
  "image_ref": "ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:latest",
  "agent_endpoint": "http://10.42.0.31:19090",
  "capacity": 100
}
```

### POST /api/v1/runtime-agent/heartbeat

Request body:

```json
{
  "pod_name": "openclaw-runtime-6f77f8b8c7-abcde",
  "runtime_type": "openclaw",
  "state": "ready",
  "used_slots": 37,
  "draining": false
}
```

### POST /api/v1/runtime-agent/metrics/report

Request body:

```json
{
  "pod_name": "openclaw-runtime-6f77f8b8c7-abcde",
  "cpu_millis_used": 13600,
  "memory_bytes_used": 42949672960,
  "disk_bytes_used": 214748364800,
  "network_rx_bytes": 9223372,
  "network_tx_bytes": 19223372,
  "gateways": [
    {
      "instance_id": 123,
      "gateway_id": "gw-123-7",
      "port": 20017,
      "state": "running",
      "last_health_at": "2026-06-01T10:00:00Z"
    }
  ]
}
```

## Backend-To-Agent Commands

All command requests use header `X-ClawManager-Control-Token`.

ClawManager treats redirects from agent command endpoints as errors. Agents should return direct 2xx, 4xx, or 5xx responses instead of 3xx redirects.

### GET /v1/health

The agent reports whether it can accept control-plane commands.

Success status: HTTP 200.

Success response body:

```json
{
  "status": "ready"
}
```

ClawManager currently treats any HTTP 2xx response as success and does not require a response body for this endpoint. Any non-2xx status is treated as an error.

#### Optional Hermes Desktop Web capability (contract versions 1 and 2)

Starting with the Hermes Desktop Web integration, ClawManager separately reads
this optional object to decide whether the experimental UI can be enabled. The
existing generic health check remains compatible with an empty 2xx response.
Requests carry `X-ClawManager-Control-Token`; agents must protect this endpoint.

```json
{
  "status": "ready",
  "capabilities": {
    "hermes_desktop_web": {
      "contract_version": 2,
      "enabled": true,
      "hermes_ref": "v2026.8.31",
      "hermes_commit": "29112bef099274229cadff79cdff7bf7b99c4b77",
      "rpc_protocol": "hermes-jsonrpc-v1",
      "backend_mode": "dashboard",
      "auth_mode": "password-cookie",
      "artifacts_verified": true,
      "release_accepted": false,
      "payload_sha256": "<64 lowercase hex characters from the verified release>"
    }
  }
}
```

All fields in `hermes_desktop_web` are required when `enabled` is true. Unknown
contract versions, unsupported commits, an absent capability, or malformed
JSON disable Desktop Web only; they must not break existing Lite/Dashboard
operations. Version 2 separates deployed protocol support from signed release
acceptance: `enabled` requires the managed launch configuration, authentication,
fixed upstream protocol and actual image artifact hashes to be verified. The
three added fields are mandatory; `release_accepted` must accurately retain
false until signed release evidence is verified. An unsigned release must not
contain acceptance evidence. An accepted release with invalid evidence is rejected.
ClawManager requires `artifacts_verified=true` and a valid payload digest, then
performs the same user, instance, Redis and upstream authentication checks.
This permits direct deployment verification without a circular requirement for
prior browser acceptance. It does not generate an acceptance report or signature.
Version 1 remains supported for older accepted runtimes. Both versions admit only
the pinned commit above, Dashboard mode and password-cookie authentication;
neither infers compatibility from a version string alone.

Both versions expose a non-native Web Core surface: the BFF forces `source: "web"`
on session creation and resume. Runtime tool policy must exclude `desktop_ui`
and tools that require an Electron UI response, including warm resumes that
reuse an existing agent/toolset. A source string alone is not proof of this
guarantee; fail the compatibility gate if it cannot be enforced and tested.

ClawManager resolves the current running binding and generation before probing
the agent. Team instances and Hermes Pro are excluded from this initial
integration. No credentials, session cookies, connection URLs or LLM secrets
belong in the capability report.

The upstream authentication exchange used by the BFF is:

1. `POST /auth/password-login` with JSON `provider=basic`, username
   `clawmanager`, the platform-managed password and `next=/chat`; the BFF
   retains the Hermes session cookie on the server.
2. `POST /api/auth/ws-ticket` with that cookie to mint an upstream ticket.
3. Connect to `/api/ws` inside the cluster with subprotocols
   `hermes-gateway-v1` and `hermes-gateway-ticket.<upstream-ticket>`.
   Never put an upstream ticket/token in the query. Only the public protocol
   `hermes-gateway-v1` may be echoed in the handshake response.

All three exchanges use the configured trusted internal
`CLAWMANAGER_CONTROL_UI_ORIGIN`, after the BFF validates the browser-facing
Origin and authorization. This supersedes the upstream query-ticket example.

Neither the Hermes cookie nor its WebSocket ticket is returned to the browser.
The browser uses a separate ClawManager HttpOnly session and a 30-second,
single-use ClawManager WebSocket ticket. Redis enforces redemption across
ClawManager replicas. See [AgentsRuntime development requirements](agentsruntime-hermes-web.md)
for process isolation, deployment and acceptance requirements.

### POST /v1/gateways

The agent creates a gateway subprocess and returns the bound port. If no port is free in the requested range, return HTTP 409.

Request body:

```json
{
  "instance_id": 123,
  "user_id": 45,
  "agent_type": "openclaw",
  "workspace_path": "/workspaces/openclaw/user-45/instance-123",
  "port_range": {
    "start": 20000,
    "end": 20299
  },
  "uid": 200123,
  "gid": 200123,
  "cpu_cores": 2,
  "memory_mb": 4096,
  "disk_quota_mb": 20480,
  "generation": 7
}
```

Success status: HTTP 200 or HTTP 201.

Success response body:

```json
{
  "gateway_id": "gw-123-7",
  "port": 20017,
  "pid": 8842,
  "status": "running"
}
```

### DELETE /v1/gateways/{gateway_id}

The agent stops the gateway and releases cgroup, process, and port resources.

`gateway_id` is URL path-escaped by ClawManager. Agents must decode the path segment before lookup.

Success status: HTTP 200, HTTP 202, or HTTP 204. ClawManager does not require a response body.

### POST /v1/drain

The agent marks the Pod as draining and rejects new gateway creation. Existing gateways continue until ClawManager migrates them.

Request body:

```json
{
  "draining": true
}
```

Success status: HTTP 200 or HTTP 202. ClawManager does not require a response body.

## Backend-To-Agent Error Responses

For now, agents should return a plain text response body for command errors.

When no port is free for `POST /v1/gateways`, return:

```http
HTTP/1.1 409 Conflict
Content-Type: text/plain

no free port
```

ClawManager maps this to `runtime agent conflict: no free port`.

For general failures, return an appropriate non-2xx status with a short plain text body:

```http
HTTP/1.1 500 Internal Server Error
Content-Type: text/plain

failed to start gateway process
```

ClawManager includes both the status code and response body in the returned error.

## Isolation Requirements

Every gateway runs as UID/GID `200000 + instance_id`. The agent rejects workspace paths outside `/workspaces/{runtime}/user-{user_id}/instance-{instance_id}` after resolving symlinks. CPU and memory limits are enforced with cgroups. Workspace quota is enforced before writes.
