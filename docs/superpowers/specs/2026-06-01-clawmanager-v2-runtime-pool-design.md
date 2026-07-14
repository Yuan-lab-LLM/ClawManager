# ClawManager V2 Runtime Pool Design

## Overview

ClawManager V2 changes the runtime model from one Kubernetes Pod per user instance to shared runtime pools. A user instance becomes metadata plus a workspace directory plus a gateway subprocess. A runtime Pod hosts one `clawmanager-agent` and many gateway subprocesses.

Runtime pools are split by agent type:

- `openclaw-runtime` Deployment, initially 1 replica.
- `hermes-runtime` Deployment, initially 1 replica.
- Each runtime Pod hosts up to 100 gateways by default.
- The 101st instance of the same type causes ClawManager to scale the matching Deployment.

The user-facing proxy path remains stable:

```text
/api/v1/instances/{id}/proxy/
```

Backend proxying uses the active binding:

```text
instance_id -> runtime_pod.pod_ip + gateway_port
```

Workspaces use a shared root:

```text
/workspaces/{runtime}/user-{user_id}/instance-{instance_id}
```

ClawManager owns scheduling, persistence, proxying, permissions, file APIs, UI, rollout control, and deployment manifests. `clawmanager-agent` owns gateway subprocess lifecycle, port allocation, cgroups, Linux user isolation, health checks, metrics, and gateway logs.

## Deployment Model

V2 keeps the one-command install goal:

```bash
kubectl apply -f clawmanager-v2.yaml
```

The YAML includes:

- Namespace, RBAC, ConfigMaps, Secrets, Services, PVCs.
- MySQL.
- Redis.
- ClawManager backend, default 3 replicas.
- ClawManager frontend/nginx.
- AI Gateway / control plane.
- `workspace-store`.
- `openclaw-runtime` Deployment.
- `hermes-runtime` Deployment.

The default storage mode is an embedded lightweight shared store, implemented as an NFS/Ganesha-style service:

- `workspace-store` persists data on its own RWO PVC.
- Backend and runtime Pods mount `/workspaces`.
- This satisfies one-YAML deployment and runtime Pod failover.
- `workspace-store` is a lightweight single point. Storage-layer HA is outside the default one-YAML mode and belongs to a future external RWX mode.

Default runtime parameters:

```text
backend replicas: 3
openclaw runtime replicas: 1
hermes runtime replicas: 1
gateways per runtime pod: 100
gateway port range per pod: 20000-20299
scheduler lease renew: 2s
scheduler failover: 6-8s
agent heartbeat interval: 2s
agent unhealthy threshold: 8s
gateway health interval: 3s
migration batch size: 10
redis lock ttl: 30s
```

All defaults should be configurable through environment variables or ConfigMap entries.

## Persistence And Hot State

MySQL is the only source of truth. Redis is a hot-path accelerator.

MySQL stores:

- Instances.
- Runtime Pods.
- Runtime bindings.
- Runtime rollouts.
- Workspace audit records.
- Users, quotas, and permissions.

Redis stores:

- Runtime Pod heartbeat TTLs.
- Runtime capacity hot cache.
- Gateway status snapshots.
- Scheduler event streams.
- WebSocket fanout.
- Short locks.

Redis may be lost and rebuilt. On ClawManager restart, the scheduler reconciles MySQL records with agent-reported `/gateways` state.

## Data Model

The existing `instances` table remains the user instance fact table. V2 adds fields or equivalent migration support for:

```text
workspace_path
last_error
```

In V2, resource fields change meaning:

- `cpu_cores`: cgroup CPU quota for one gateway.
- `memory_gb`: cgroup memory limit for one gateway.
- `disk_gb`: logical workspace quota.

New `runtime_pods` table:

```text
id
runtime_type: openclaw | hermes
pod_name
pod_namespace
pod_ip
agent_id
status: ready | draining | unhealthy | offline | retired
version
capacity
used
port_start
port_end
management_url
management_token_encrypted
last_heartbeat_at
created_at
updated_at
```

New `instance_runtime_bindings` table:

```text
id
instance_id
runtime_pod_id
runtime_type
gateway_port
workspace_path
gateway_status: starting | running | stopped | error
status: active | released | migrating | error
reason: create | start | stop | failover | upgrade | delete | recovery
bound_at
released_at
last_health_check_at
last_error
```

New `runtime_rollouts` table:

```text
id
runtime_type
from_image
to_image
status: idle | creating_new_pods | draining_old_pods | migrating_gateways | paused | failed | completed | rolling_back
strategy
batch_size
started_by
started_at
completed_at
paused_at
error_message
```

## State Machines

Instance statuses:

```text
creating -> running   gateway scheduled and healthy
creating -> error     workspace, scheduler, or agent failure
running  -> stopped   user stops instance; slot and port are released
running  -> running   failover or upgrade migration succeeds
running  -> error     repeated recovery failure
running  -> deleting
stopped  -> creating  user starts instance; scheduler chooses a new Pod
stopped  -> deleting
error    -> creating  user retries start
error    -> deleting
```

Runtime Pod statuses:

```text
ready      accepts new gateways
draining   accepts no new gateways; existing gateways are migrated away
unhealthy  agent or Pod health failure; active bindings migrate away
offline    Pod is unreachable
retired    Pod has no active gateways and can be removed or ignored
```

## Scheduler

The backend runs as multiple replicas. All replicas serve API traffic, but only the backend holding a Kubernetes `Lease` runs `RuntimeScheduler`.

The scheduler handles:

- Newly created instances in `creating`.
- Start requests.
- Stop/Delete cleanup.
- Runtime Pod capacity changes.
- Runtime Pod heartbeat timeout.
- Gateway health failure.
- Draining Pod migration.
- Rollout batches.
- ClawManager restart recovery.
- Orphan gateway cleanup.

Concurrency protection uses:

- Kubernetes Lease for scheduler leadership.
- Redis short locks:
  - `lock:instance:{id}`
  - `lock:runtime:scale:{type}`
  - `lock:runtime:pod:{pod_id}`
  - `lock:workspace:{instance_id}:{path_hash}`
- Conditional DB updates as the final consistency guard.

## Instance Lifecycle

Create is asynchronous:

1. API creates the instance record with `creating`.
2. API creates the workspace directory.
3. API writes a scheduler event to Redis.
4. Scheduler chooses a `ready` runtime Pod with capacity.
5. If no Pod has capacity, scheduler scales the matching runtime Deployment.
6. Scheduler waits for the new agent to register.
7. Scheduler calls agent `POST /gateways`.
8. Scheduler writes an active binding.
9. Scheduler updates the instance to `running`.

Stop:

- Calls agent to delete the gateway.
- Releases slot and port.
- Marks active binding `released`.
- Updates the instance to `stopped`.
- Leaves workspace data intact.

Start:

- Moves instance to `creating`.
- Schedules a fresh gateway.
- May use a different Pod and port.

Restart:

- Prefer restart on the current healthy Pod.
- If the current Pod is unhealthy, reschedule to a healthy Pod.

Delete:

- Stops the gateway if active.
- Deletes workspace contents.
- Deletes or marks instance according to existing repository behavior.
- Writes audit events.

## Failover

Failover triggers:

- Agent heartbeat exceeds 8 seconds.
- Pod is NotReady, deleted, or restarted.
- Gateway health fails repeatedly.
- Proxy detects active binding is unreachable.

Failover flow:

1. Mark runtime Pod `unhealthy` or `offline`.
2. Select a healthy same-runtime Pod.
3. If capacity exists, call target agent `recover`.
4. If capacity is full, scale the Deployment first.
5. Write a new active binding.
6. Release or retire the old binding.
7. Clean orphan gateway if the old Pod later returns.

Target interruption is 5-10 seconds.

## Gray Upgrade / Rollout

OpenClaw and Hermes upgrades use the same drain-and-migrate machinery as failover.

Admin starts rollout from System Settings / System Images:

1. Create a `runtime_rollouts` row.
2. Update the runtime Deployment image/template.
3. New version Pods register as ready.
4. Old version Pods are marked `draining`.
5. Draining Pods receive no new gateways.
6. Scheduler migrates gateways in batches of 10.
7. Each batch is health-checked.
8. Failure pauses rollout.
9. Admin can continue, pause, or roll back.
10. Empty old Pods become `retired`.

Deployment strategy:

```text
maxUnavailable: 0
maxSurge: 1
```

ClawManager controls gateway migration instead of allowing Kubernetes rolling update to kill Pods that still host gateways.

## Agent Contract

Each runtime Pod runs one `clawmanager-agent`.

Agent keeps existing active reporting abilities:

```http
POST /api/v1/agent/register
POST /api/v1/agent/heartbeat
POST /api/v1/agent/state/report
POST /api/v1/agent/skills/inventory
```

Reporting expands from one instance to Pod plus many gateways:

```json
{
  "agent_id": "openclaw-runtime-abc",
  "runtime_type": "openclaw",
  "pod_name": "openclaw-runtime-abc",
  "pod_ip": "10.42.1.83",
  "capacity": 100,
  "used": 37,
  "port_range": { "start": 20000, "end": 20299 },
  "version": "openclaw:2.1.0",
  "gateways": [
    {
      "instance_id": 42,
      "port": 20017,
      "pid": 812,
      "status": "running",
      "workspace_path": "/workspaces/openclaw/user-7/instance-42"
    }
  ]
}
```

ClawManager actively calls agent on:

```text
http://<pod_ip>:19090
```

All management calls use:

```http
Authorization: Bearer <management_token>
```

Required agent management APIs:

```http
GET    /healthz
GET    /capacity
GET    /gateways
GET    /gateways/{instance_id}
POST   /gateways
POST   /gateways/{instance_id}/stop
POST   /gateways/{instance_id}/restart
POST   /gateways/{instance_id}/recover
DELETE /gateways/{instance_id}
GET    /gateways/{instance_id}/logs?tail=200
```

Create gateway request:

```json
{
  "instance_id": 42,
  "user_id": 7,
  "agent_type": "openclaw",
  "workspace_path": "/workspaces/openclaw/user-7/instance-42",
  "port_range": { "start": 20000, "end": 20299 },
  "env": {
    "CLAWMANAGER_INSTANCE_ID": "42",
    "CLAWMANAGER_LLM_BASE_URL": "http://clawmanager-gateway.clawmanager-system.svc.cluster.local:9001/api/v1/gateway/llm"
  },
  "resources": {
    "cpu_cores": 0.5,
    "memory_mb": 512,
    "pids_limit": 256
  }
}
```

Create gateway response:

```json
{
  "instance_id": 42,
  "port": 20017,
  "status": "running",
  "pid": 812,
  "url": "http://127.0.0.1:20017"
}
```

Agent port requirements:

- Pod-local gateway range is `20000-20299`.
- Agent assigns the actual port.
- Agent skips conflicts or returns structured errors.
- Agent persists enough state to recover the port occupation map after restart.
- Repeated `create` for the same instance is idempotent.

Agent security requirements:

- Each gateway runs as its own Linux user.
- Deterministic identity:

```text
uid = 200000 + instance_id
gid = 200000 + instance_id
```

- Agent creates uid/gid as needed.
- Agent `chown`s workspace to that uid/gid.
- Workspace permission should be `0700` or `0750`.
- Gateway subprocess must not run as root.
- Agent may run as root to manage users, ownership, and cgroups.
- Agent must defend against `../`, absolute paths, and symlink escape.

Agent resource requirements:

- Each gateway uses a dedicated cgroup or systemd scope.
- Enforce CPU quota, memory limit, and pids limit.
- IO weight is outside the first release and can be added in a separate resource-control enhancement.
- Agent reports cgroup OOM/limit failures.
- Agent may restart failed gateways, then mark error after retry exhaustion.

Agent health and recovery:

- Check gateway health every 3 seconds.
- Restart locally before reporting hard failure.
- Support `recover` on a different Pod using the same workspace path.

Agent metrics:

- Pod-level CPU, memory, disk I/O, network in/out, used/capacity, workspace total.
- Gateway-level instance_id, port, pid, status, CPU, memory, pids, network in/out, workspace_used_bytes, restart_count, last_error.

## Proxying

Frontend and iframe use:

```text
/api/v1/instances/{id}/proxy/
```

Backend checks user permission and active binding, then proxies to:

```text
http://{pod_ip}:{gateway_port}
```

HTTP and WebSocket traffic both use this path. The frontend path does not change after migration; only the binding changes.

V2 does not create per-instance Kubernetes Services. It uses direct Pod IP plus gateway port.

## Workspace File Management

ClawManager backend directly reads and writes the shared workspace. File management is independent of the OpenClaw/Hermes iframe and works even when the instance is stopped.

Supported operations:

- List files and directories.
- Text preview.
- Image preview.
- PDF preview.
- Download.
- Upload.
- Create folder.
- Rename file or directory.
- Delete file or directory.

API shape:

```http
GET    /api/v1/instances/{id}/workspace/files?path=/
GET    /api/v1/instances/{id}/workspace/preview?path=/README.md
GET    /api/v1/instances/{id}/workspace/download?path=/README.md
POST   /api/v1/instances/{id}/workspace/upload
POST   /api/v1/instances/{id}/workspace/folders
PATCH  /api/v1/instances/{id}/workspace/rename
DELETE /api/v1/instances/{id}/workspace/files
```

Preview support:

- Text, maximum 1 MiB:
  - `.txt`, `.md`, `.json`, `.yaml`, `.yml`, `.log`, `.py`, `.js`, `.ts`, `.go`, `.sh`
- Images, maximum 10 MiB:
  - `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.svg`
- PDF through browser iframe.
- Other binary files are download-only.

Upload defaults:

- Maximum upload size: 500 MiB.
- Configurable through environment variables.

Security rules:

1. API accepts relative paths only.
2. Clean and join the user path with the instance workspace root.
3. Resolve realpath.
4. Final real path must remain inside the instance workspace root.
5. Reject `../`.
6. Reject absolute paths.
7. Reject symlink escape.
8. Upload filenames cannot contain path separators.
9. Preview, download, delete, rename, and upload all use the same path guard.

Workspace write operations use Redis path locks:

```text
lock:workspace:{instance_id}:{path_hash}
```

Logical disk quota:

- `disk_gb` is the workspace quota.
- Upload checks current size plus upload size before writing.
- Agent periodically reports workspace usage.
- If gateway writes beyond quota, agent reports `quota_exceeded`.
- No filesystem project quota in the first version.

Deletion:

- No trash/recycle bin in V2 first version.
- Deletes are direct and require confirmation.
- Directory deletion displays item count before confirmation.

Audit:

- Audit all write operations: upload, delete, rename, create folder.
- Audit download.
- Do not audit preview.

## Frontend Information Architecture

Ordinary users only see usable product state.

V2 also includes a UI simplification pass. The target style is quiet, direct, and work-focused:

- Use a simple visual system with neutral backgrounds, restrained borders, clear spacing, and minimal decorative effects.
- Avoid marketing-style hero areas, oversized titles, nested cards, ornamental gradients, and visual noise.
- Prefer full-width sections and compact panels over many floating cards.
- Use small, stable controls with clear labels and icons for common actions.
- Keep ordinary user pages focused on the next useful action, not operational detail.
- Keep admin pages denser and more table-oriented, with metrics designed for scanning and comparison.
- Use consistent status language across pages: `Available`, `Starting`, `Unavailable` for users; `Ready`, `Draining`, `Unhealthy`, `Offline`, `Retired` for administrators.
- Make responsive layouts predictable: iframe, file manager, tables, and toolbars must not overflow or overlap on mobile or desktop.

User instance detail page shows:

- Instance name.
- Runtime type: OpenClaw or Hermes.
- Availability: `Available`, `Starting`, `Unavailable`.
- Start, Stop, Restart, Delete, Refresh, Fullscreen.
- OpenClaw/Hermes iframe.
- Workspace file manager.
- Workspace usage.

User page layout:

```text
[Compact title row: instance name, availability, primary actions]
[OpenClaw/Hermes iframe with Refresh and Fullscreen]
[Workspace file manager]
```

No separate service/resource/recent-status blocks appear on the ordinary user page. If an instance is unavailable, the page shows only actionable messages such as starting progress, retry start, or contact administrator.

User instance detail page does not show:

- Pod name or IP.
- Gateway port.
- Runtime capacity.
- Runtime version.
- Agent heartbeat.
- Scheduler or migration details.
- CPU, memory, network, disk runtime internals.
- Service/resource/recent status operational cards.

Admin adds a Runtime Pods page:

- Runtime selector: OpenClaw / Hermes.
- Pool summary: Pod count, gateway usage, CPU, memory, disk I/O, network.
- Pod table: status, used/capacity, CPU, memory, disk I/O, network, version.
- Selected Pod detail: Pod IP, agent heartbeat, port range, workspace mount, metrics chart.
- Gateway distribution: instance, user, port, status.
- Operations: Drain Pod, Resume Pod, Migrate Gateways, View Agent Logs, Scale Runtime Pool.

Admin page style:

- Use table-first layouts for Pods and gateways.
- Use compact metric cells and sparklines rather than large decorative charts.
- Keep dangerous operations grouped and confirm destructive or disruptive actions.
- Do not hide operational detail from administrators, but keep it visually organized by pool, Pod, and gateway.

Admin runtime data is pushed over WebSocket. Initial page load uses HTTP snapshot; updates use WebSocket from Redis-backed backend state. Agent reports metrics every 2 seconds.

System Settings / System Images adds rollout controls:

- OpenClaw runtime image.
- Hermes runtime image.
- Current version.
- Target version.
- Rollout status.
- Old and new Pod counts.
- Migrated and failed gateway counts.
- Publish, pause, continue, rollback.

## Legacy Compatibility

V2 allows new instance creation only for:

- `openclaw`
- `hermes`

Legacy types are not offered on the create page:

- `ubuntu`
- `debian`
- `centos`
- `custom`
- `webtop`

Existing legacy instances remain visible and are labeled `Legacy`. Their old detail and runtime behavior should remain available where practical. No automatic migration is performed. Admins can delete legacy instances. A legacy migration tool is out of scope for this V2 runtime-pool design.

## Acceptance Criteria

Instance creation:

- Creating OpenClaw/Hermes does not create per-instance Pod/PVC/Service.
- API returns `creating`.
- Scheduler creates a gateway through agent.
- Instance becomes `running`.
- The 101st same-runtime instance scales the matching runtime Deployment.

Port handling:

- Gateway ports do not conflict within one Pod.
- Agent restart restores port occupation state.
- Binding records the actual assigned port.

Access:

- `/api/v1/instances/{id}/proxy/` opens OpenClaw/Hermes.
- Frontend path remains unchanged after migration.
- Ordinary users cannot see Pod/IP/port details.

UI style:

- Ordinary user pages use the simplified V2 style: compact title row, iframe, workspace file manager, and no runtime operational cards.
- Admin pages use a simple operations-console style: dense tables, compact metrics, clear grouping, and no decorative dashboard chrome.
- Text, buttons, toolbars, iframe, file manager, and runtime tables remain readable without overlap on mobile and desktop.
- Status language is consistent with the V2 design vocabulary for user and admin contexts.

Failover:

- Runtime Pod deletion or restart migrates running instances within 5-10 seconds.
- Workspace data remains intact.
- Old orphan gateways are cleaned when old Pods return.

Gray upgrade:

- Publishing a new image starts new runtime Pods.
- Old Pods enter draining.
- Gateways migrate in batches.
- Rollout can pause on failure.
- Admin can continue or roll back.

Workspace files:

- Users can list, preview, download, upload, create folder, rename, and delete within their workspace.
- Stopped instances still allow file operations.
- Path escape and symlink escape are rejected.
- Deletes require confirmation and do not use trash.
- Write operations and downloads are audited.

Admin runtime page:

- Shows each runtime Pod status, gateway usage, CPU, memory, disk, and network.
- Shows gateway distribution per Pod.
- Supports drain, resume, migrate, log view, and scale operations.
- Uses WebSocket for real-time updates.

Deployment:

- One YAML installs all required components.
- Backend defaults to 3 replicas.
- OpenClaw and Hermes runtime pools default to 1 replica each.
- Redis, MySQL, and workspace-store are included and usable.

## Open Risks

- Default workspace-store is intentionally lightweight and not storage-layer HA.
- Agent changes are on the critical path and must follow this contract.
- Pod-level multi-tenancy requires careful Linux user, cgroup, and path isolation.
- Rollout migration must be idempotent and pause safely on partial failures.
- Metrics volume must be controlled through Redis aggregation and WebSocket subscriptions.
