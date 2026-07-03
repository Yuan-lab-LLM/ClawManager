# ClawManager V2 Runtime Pool Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build ClawManager V2 so OpenClaw and Hermes instances run as gateway processes inside shared runtime Pods, with workspace isolation, fast failover, admin runtime observability, and a simpler user UI.

**Architecture:** The backend remains the source of truth in MySQL, uses Redis for hot locks/events/cache, and runs an embedded leader-elected runtime scheduler across multiple backend replicas. Runtime Pods are managed by Kubernetes Deployments and `clawmanager-agent`; each runtime Pod hosts up to 100 gateways and exposes gateway traffic by Pod IP plus assigned port. Workspaces live under a shared `/workspaces` mount and are accessed through a dedicated backend file manager API, not through the frame.

**Tech Stack:** Go 1.26, Gin, MySQL via `upper/db`, Redis RESP client, Kubernetes client-go, React 19, Vite, TypeScript, Tailwind CSS, Kubernetes YAML.

---

## Scope Check

This release touches several subsystems, but they are tightly ordered: schema first, scheduler second, lifecycle/proxy/files third, UI and manifests last. Keep the work in this single master plan, but commit after each task so any subsystem can be reviewed independently.

## File Structure

Backend data and migrations:
- Modify: `backend/internal/db/migrations.go` - existing embedded migration runner remains unchanged unless ordering assumptions fail in tests.
- Create: `backend/internal/db/migrations/023_add_runtime_pool_v2.sql` - runtime Pod, gateway binding, rollout, and workspace audit schema.
- Modify: `backend/internal/db/migrations_test.go` - verify migration file ordering and SQL splitting still handles migration 023.
- Modify: `backend/internal/models/instance.go` - add workspace fields and V2 runtime metadata.
- Create: `backend/internal/models/runtime_pod.go` - runtime Pod state and metrics model.
- Create: `backend/internal/models/instance_runtime_binding.go` - active gateway binding model.
- Create: `backend/internal/models/runtime_rollout.go` - rollout state model for gray upgrades.
- Create: `backend/internal/models/workspace_file_audit.go` - upload/download/write audit model.
- Modify: `backend/internal/repository/instance_repository.go` - V2 lifecycle selectors and status update helpers.
- Create: `backend/internal/repository/runtime_pod_repository.go` - runtime Pod CRUD and capacity claims.
- Create: `backend/internal/repository/instance_runtime_binding_repository.go` - gateway binding CRUD and generation-safe updates.
- Create: `backend/internal/repository/runtime_rollout_repository.go` - rollout CRUD and selectors.
- Create: `backend/internal/repository/workspace_file_audit_repository.go` - append-only file audit writes.

Backend runtime scheduling and agent integration:
- Create: `docs/clawmanager-agent-v2-contract.md` - exact HTTP dual-channel contract required from `clawmanager-agent`.
- Create: `backend/internal/services/redis_client.go` - platform Redis client reusing the existing RESP approach.
- Create: `backend/internal/services/runtime_agent_client.go` - backend-to-agent command client.
- Create: `backend/internal/services/runtime_scheduler.go` - leader loop, assignment, failover, and gray-drain orchestration.
- Create: `backend/internal/services/runtime_capacity.go` - capacity calculations and runtime type normalization.
- Create: `backend/internal/services/runtime_events.go` - Redis-backed runtime event fanout for all backend replicas.
- Create: `backend/internal/services/runtime_leader.go` - Kubernetes Lease leader election wrapper.
- Create: `backend/internal/services/k8s/runtime_deployment_service.go` - create, scale, and inspect OpenClaw/Hermes runtime Deployments.
- Modify: `backend/internal/services/instance_service.go` - route V2 create/start/stop/delete through scheduler state; keep legacy behavior for old instance types.
- Modify: `backend/internal/services/instance_proxy_service.go` - proxy V2 instances to binding Pod IP plus gateway port; keep legacy service proxy fallback.
- Modify: `backend/internal/services/sync_service.go` - stop assuming every running instance owns a Pod.
- Modify: `backend/cmd/server/main.go` - wire repositories, services, scheduler start, routes, and websocket runtime event bridge.

Backend workspace file APIs:
- Create: `backend/internal/services/workspace_path_guard.go` - path clean, realpath, and symlink escape prevention.
- Create: `backend/internal/services/workspace_file_service.go` - list, preview, download, upload, mkdir, rename, delete.
- Create: `backend/internal/handlers/workspace_file_handler.go` - user-facing workspace file endpoints.
- Modify: `backend/internal/handlers/instance_handler.go` - attach workspace routes and simplify status response for V2.
- Modify: `backend/internal/utils/response.go` - add V2 file validation errors to client-safe bad request mapping.

Backend admin APIs:
- Create: `backend/internal/handlers/runtime_pool_handler.go` - admin runtime Pod status, metrics, drain, and rollout endpoints.
- Modify: `backend/internal/services/websocket_service.go` - add admin runtime event subscriptions with user-role checks.
- Modify: `backend/internal/handlers/websocket_handler.go` if it exists in this repository; otherwise wire websocket changes in the existing handler file discovered during implementation.

Frontend user and admin experience:
- Modify: `frontend/package.json` and lock file - add `lucide-react` if icons are not already available.
- Modify: `frontend/src/index.css` - neutral, simple design tokens; remove heavy gradients and oversized radii.
- Modify: `frontend/src/components/UserLayout.tsx` - simpler user shell.
- Modify: `frontend/src/components/AdminLayout.tsx` - simpler admin shell and add Runtime Pods navigation.
- Modify: `frontend/src/types/instance.ts` - V2 instance availability and workspace types.
- Create: `frontend/src/types/runtimePool.ts` - admin runtime Pod, gateway, metrics, rollout types.
- Create: `frontend/src/services/workspaceService.ts` - workspace API client.
- Create: `frontend/src/services/runtimePoolService.ts` - admin runtime API client.
- Modify: `frontend/src/hooks/useWebSocket.ts` - add admin runtime event hook.
- Modify: `frontend/src/services/instanceService.ts` - V2 status fields and workspace entry points.
- Modify: `frontend/src/pages/instances/CreateInstancePage.tsx` - only allow creating OpenClaw and Hermes for V2.
- Modify: `frontend/src/pages/instances/InstanceListPage.tsx` - hide Pod/service/resource details from ordinary users.
- Modify: `frontend/src/pages/instances/InstanceDetailPage.tsx` - show availability, service frame, workspace file manager, and simple controls.
- Create: `frontend/src/components/WorkspaceFileManager.tsx` - independent file list, preview, upload, download, rename, delete.
- Create: `frontend/src/components/InstanceServiceFrame.tsx` - OpenClaw/Hermes iframe wrapper with availability state.
- Create: `frontend/src/pages/admin/RuntimePodsPage.tsx` - admin-only runtime Pod metrics and gateway usage.
- Modify: `frontend/src/pages/admin/SystemSettingsPage.tsx` - add OpenClaw/Hermes runtime image and gray rollout controls.
- Modify: `frontend/src/router/index.tsx` - route `/admin/runtime-pods`.

Deployment:
- Modify: `deployments/k8s/clawmanager.yaml` - one-YAML install for Redis, workspace store, backend replicas, runtime Deployments, RBAC, env.
- Modify: `deployments/k3s/clawmanager.yaml` - same defaults adapted to k3s/local path storage.
- Modify: `backend/deployments/k8s/clawreef-incluster.yaml` if this file is still shipped as an install path; otherwise leave it unchanged and document that `deployments/k8s/clawmanager.yaml` is canonical.

## Agent Contract Summary

The `clawmanager-agent` changes are documented but implemented outside this repository. The backend implementation must assume these endpoints and payloads:

Agent-to-backend channel:

```http
POST /api/v1/runtime-agent/register
POST /api/v1/runtime-agent/heartbeat
POST /api/v1/runtime-agent/gateways/report
POST /api/v1/runtime-agent/skills/report
POST /api/v1/runtime-agent/metrics/report
```

Backend-to-agent channel:

```http
GET  http://{pod_ip}:19090/v1/health
GET  http://{pod_ip}:19090/v1/metrics
POST http://{pod_ip}:19090/v1/gateways
DELETE http://{pod_ip}:19090/v1/gateways/{gateway_id}
POST http://{pod_ip}:19090/v1/gateways/{gateway_id}/health
POST http://{pod_ip}:19090/v1/drain
```

Gateway create request:

```json
{
  "instance_id": 123,
  "user_id": 45,
  "agent_type": "openclaw",
  "workspace_path": "/workspaces/openclaw/user-45/instance-123",
  "port_range": { "start": 20000, "end": 20299 },
  "uid": 200123,
  "gid": 200123,
  "cpu_cores": 2,
  "memory_mb": 4096,
  "disk_quota_mb": 20480,
  "generation": 7
}
```

Gateway create response:

```json
{
  "gateway_id": "gw-123-7",
  "port": 20017,
  "pid": 8842,
  "status": "running"
}
```

The agent must allocate a free port inside `20000-20299`, enforce one Linux UID/GID per instance using `200000 + instance_id`, apply cgroup CPU/memory limits, enforce workspace quota, reject symlink escapes inside workspaces, report gateway health, and survive backend replica changes. During gray upgrade, `POST /v1/drain` must reject new gateways and continue reporting existing gateway health until ClawManager migrates them away.

## Task 1: Schema, Models, And Repository Interfaces

**Files:**
- Create: `backend/internal/db/migrations/023_add_runtime_pool_v2.sql`
- Modify: `backend/internal/db/migrations_test.go`
- Modify: `backend/internal/models/instance.go`
- Create: `backend/internal/models/runtime_pod.go`
- Create: `backend/internal/models/instance_runtime_binding.go`
- Create: `backend/internal/models/runtime_rollout.go`
- Create: `backend/internal/models/workspace_file_audit.go`
- Modify: `backend/internal/repository/instance_repository.go`
- Create: `backend/internal/repository/runtime_pod_repository.go`
- Create: `backend/internal/repository/instance_runtime_binding_repository.go`
- Create: `backend/internal/repository/runtime_rollout_repository.go`
- Create: `backend/internal/repository/workspace_file_audit_repository.go`

- [ ] **Step 1: Add failing migration order test**

Append this test to `backend/internal/db/migrations_test.go`:

```go
func TestMigration023IsEmbedded(t *testing.T) {
	files, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		t.Fatalf("read embedded migrations: %v", err)
	}

	found := false
	for _, file := range files {
		if file.Name() == "023_add_runtime_pool_v2.sql" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("migration 023_add_runtime_pool_v2.sql is not embedded")
	}
}
```

- [ ] **Step 2: Run migration test and verify it fails**

Run:

```powershell
go test ./backend/internal/db -run TestMigration023IsEmbedded -count=1
```

Expected: fail with `migration 023_add_runtime_pool_v2.sql is not embedded`.

- [ ] **Step 3: Create migration 023**

Create `backend/internal/db/migrations/023_add_runtime_pool_v2.sql` with:

```sql
ALTER TABLE instances
  ADD COLUMN workspace_path VARCHAR(1024) NULL AFTER mount_path,
  ADD COLUMN workspace_usage_bytes BIGINT NOT NULL DEFAULT 0 AFTER workspace_path,
  ADD COLUMN runtime_generation INT NOT NULL DEFAULT 1 AFTER workspace_usage_bytes,
  ADD COLUMN runtime_error_message TEXT NULL AFTER runtime_generation;

CREATE TABLE runtime_pods (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  runtime_type VARCHAR(32) NOT NULL,
  namespace VARCHAR(128) NOT NULL,
  pod_name VARCHAR(255) NOT NULL,
  pod_uid VARCHAR(128) NULL,
  pod_ip VARCHAR(64) NULL,
  node_name VARCHAR(255) NULL,
  deployment_name VARCHAR(255) NOT NULL,
  image_ref VARCHAR(512) NOT NULL,
  agent_endpoint VARCHAR(255) NULL,
  state VARCHAR(32) NOT NULL DEFAULT 'pending',
  capacity INT NOT NULL DEFAULT 100,
  used_slots INT NOT NULL DEFAULT 0,
  draining TINYINT(1) NOT NULL DEFAULT 0,
  cpu_millis_used BIGINT NOT NULL DEFAULT 0,
  memory_bytes_used BIGINT NOT NULL DEFAULT 0,
  disk_bytes_used BIGINT NOT NULL DEFAULT 0,
  network_rx_bytes BIGINT NOT NULL DEFAULT 0,
  network_tx_bytes BIGINT NOT NULL DEFAULT 0,
  metrics_json JSON NULL,
  last_seen_at DATETIME NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_runtime_pod_namespace_name (namespace, pod_name),
  KEY idx_runtime_pods_schedulable (runtime_type, state, draining, used_slots, capacity),
  KEY idx_runtime_pods_last_seen (last_seen_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE instance_runtime_bindings (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  instance_id INT NOT NULL,
  runtime_pod_id BIGINT NOT NULL,
  runtime_type VARCHAR(32) NOT NULL,
  gateway_id VARCHAR(128) NOT NULL,
  gateway_port INT NOT NULL,
  gateway_pid INT NULL,
  workspace_path VARCHAR(1024) NOT NULL,
  state VARCHAR(32) NOT NULL DEFAULT 'creating',
  generation INT NOT NULL DEFAULT 1,
  last_health_at DATETIME NULL,
  error_message TEXT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  UNIQUE KEY uk_instance_runtime_binding_instance (instance_id),
  UNIQUE KEY uk_instance_runtime_binding_gateway (runtime_pod_id, gateway_port),
  KEY idx_instance_runtime_binding_pod_state (runtime_pod_id, state),
  CONSTRAINT fk_instance_runtime_binding_instance
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE,
  CONSTRAINT fk_instance_runtime_binding_pod
    FOREIGN KEY (runtime_pod_id) REFERENCES runtime_pods(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE runtime_rollouts (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  runtime_type VARCHAR(32) NOT NULL,
  target_image_ref VARCHAR(512) NOT NULL,
  status VARCHAR(32) NOT NULL DEFAULT 'pending',
  batch_size INT NOT NULL DEFAULT 1,
  max_unavailable INT NOT NULL DEFAULT 1,
  started_by INT NULL,
  started_at DATETIME NULL,
  finished_at DATETIME NULL,
  error_message TEXT NULL,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  KEY idx_runtime_rollouts_type_status (runtime_type, status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE workspace_file_audits (
  id BIGINT PRIMARY KEY AUTO_INCREMENT,
  instance_id INT NOT NULL,
  user_id INT NOT NULL,
  action VARCHAR(32) NOT NULL,
  relative_path VARCHAR(1024) NOT NULL,
  bytes BIGINT NOT NULL DEFAULT 0,
  created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  KEY idx_workspace_file_audits_instance_time (instance_id, created_at),
  KEY idx_workspace_file_audits_user_time (user_id, created_at),
  CONSTRAINT fk_workspace_file_audits_instance
    FOREIGN KEY (instance_id) REFERENCES instances(id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
```

- [ ] **Step 4: Extend instance model**

Add fields to `backend/internal/models/instance.go` after `MountPath` and after `StoppedAt`:

```go
	WorkspacePath       *string `db:"workspace_path" json:"workspace_path,omitempty"`
	WorkspaceUsageBytes int64   `db:"workspace_usage_bytes" json:"workspace_usage_bytes"`
	RuntimeGeneration   int     `db:"runtime_generation" json:"runtime_generation"`
	RuntimeErrorMessage *string `db:"runtime_error_message" json:"runtime_error_message,omitempty"`
```

- [ ] **Step 5: Add runtime models**

Create `backend/internal/models/runtime_pod.go`:

```go
package models

import "time"

type RuntimePod struct {
	ID              int64      `db:"id,primarykey,autoincrement" json:"id"`
	RuntimeType     string     `db:"runtime_type" json:"runtime_type"`
	Namespace       string     `db:"namespace" json:"namespace"`
	PodName         string     `db:"pod_name" json:"pod_name"`
	PodUID          *string    `db:"pod_uid" json:"pod_uid,omitempty"`
	PodIP           *string    `db:"pod_ip" json:"pod_ip,omitempty"`
	NodeName        *string    `db:"node_name" json:"node_name,omitempty"`
	DeploymentName  string     `db:"deployment_name" json:"deployment_name"`
	ImageRef        string     `db:"image_ref" json:"image_ref"`
	AgentEndpoint   *string    `db:"agent_endpoint" json:"agent_endpoint,omitempty"`
	State           string     `db:"state" json:"state"`
	Capacity        int        `db:"capacity" json:"capacity"`
	UsedSlots       int        `db:"used_slots" json:"used_slots"`
	Draining        bool       `db:"draining" json:"draining"`
	CPUMillisUsed   int64      `db:"cpu_millis_used" json:"cpu_millis_used"`
	MemoryBytesUsed int64      `db:"memory_bytes_used" json:"memory_bytes_used"`
	DiskBytesUsed   int64      `db:"disk_bytes_used" json:"disk_bytes_used"`
	NetworkRXBytes  int64      `db:"network_rx_bytes" json:"network_rx_bytes"`
	NetworkTXBytes  int64      `db:"network_tx_bytes" json:"network_tx_bytes"`
	MetricsJSON      *string    `db:"metrics_json" json:"metrics_json,omitempty"`
	LastSeenAt       *time.Time `db:"last_seen_at" json:"last_seen_at,omitempty"`
	CreatedAt       time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt       time.Time  `db:"updated_at" json:"updated_at"`
}

func (RuntimePod) TableName() string {
	return "runtime_pods"
}
```

Create `backend/internal/models/instance_runtime_binding.go`:

```go
package models

import "time"

type InstanceRuntimeBinding struct {
	ID            int64      `db:"id,primarykey,autoincrement" json:"id"`
	InstanceID    int        `db:"instance_id" json:"instance_id"`
	RuntimePodID  int64      `db:"runtime_pod_id" json:"runtime_pod_id"`
	RuntimeType   string     `db:"runtime_type" json:"runtime_type"`
	GatewayID     string     `db:"gateway_id" json:"gateway_id"`
	GatewayPort   int        `db:"gateway_port" json:"gateway_port"`
	GatewayPID    *int       `db:"gateway_pid" json:"gateway_pid,omitempty"`
	WorkspacePath string     `db:"workspace_path" json:"workspace_path"`
	State         string     `db:"state" json:"state"`
	Generation    int        `db:"generation" json:"generation"`
	LastHealthAt  *time.Time `db:"last_health_at" json:"last_health_at,omitempty"`
	ErrorMessage  *string    `db:"error_message" json:"error_message,omitempty"`
	CreatedAt     time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt     time.Time  `db:"updated_at" json:"updated_at"`
}

func (InstanceRuntimeBinding) TableName() string {
	return "instance_runtime_bindings"
}
```

Create `backend/internal/models/runtime_rollout.go`:

```go
package models

import "time"

type RuntimeRollout struct {
	ID             int64      `db:"id,primarykey,autoincrement" json:"id"`
	RuntimeType    string     `db:"runtime_type" json:"runtime_type"`
	TargetImageRef string     `db:"target_image_ref" json:"target_image_ref"`
	Status         string     `db:"status" json:"status"`
	BatchSize      int        `db:"batch_size" json:"batch_size"`
	MaxUnavailable int        `db:"max_unavailable" json:"max_unavailable"`
	StartedBy      *int       `db:"started_by" json:"started_by,omitempty"`
	StartedAt      *time.Time `db:"started_at" json:"started_at,omitempty"`
	FinishedAt     *time.Time `db:"finished_at" json:"finished_at,omitempty"`
	ErrorMessage   *string    `db:"error_message" json:"error_message,omitempty"`
	CreatedAt      time.Time  `db:"created_at" json:"created_at"`
	UpdatedAt      time.Time  `db:"updated_at" json:"updated_at"`
}

func (RuntimeRollout) TableName() string {
	return "runtime_rollouts"
}
```

Create `backend/internal/models/workspace_file_audit.go`:

```go
package models

import "time"

type WorkspaceFileAudit struct {
	ID           int64     `db:"id,primarykey,autoincrement" json:"id"`
	InstanceID   int       `db:"instance_id" json:"instance_id"`
	UserID       int       `db:"user_id" json:"user_id"`
	Action       string    `db:"action" json:"action"`
	RelativePath string    `db:"relative_path" json:"relative_path"`
	Bytes        int64     `db:"bytes" json:"bytes"`
	CreatedAt    time.Time `db:"created_at" json:"created_at"`
}

func (WorkspaceFileAudit) TableName() string {
	return "workspace_file_audits"
}
```

- [ ] **Step 6: Add repository interfaces and implementations**

Implement interfaces with these method signatures. Use `upper/db` collections for normal CRUD and raw SQL for capacity claims requiring atomic increments.

`backend/internal/repository/runtime_pod_repository.go`:

```go
package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type RuntimePodRepository interface {
	UpsertFromAgent(ctx context.Context, pod *models.RuntimePod) error
	GetByID(ctx context.Context, id int64) (*models.RuntimePod, error)
	GetByNamespaceName(ctx context.Context, namespace, podName string) (*models.RuntimePod, error)
	List(ctx context.Context, runtimeType string) ([]models.RuntimePod, error)
	ListSchedulable(ctx context.Context, runtimeType string) ([]models.RuntimePod, error)
	TryClaimSlot(ctx context.Context, podID int64) (bool, error)
	ReleaseSlot(ctx context.Context, podID int64) error
	MarkState(ctx context.Context, podID int64, state string, draining bool) error
	MarkUnseenUnhealthy(ctx context.Context, cutoff time.Time) error
	UpdateMetrics(ctx context.Context, podID int64, metrics RuntimePodMetricsUpdate) error
}

type RuntimePodMetricsUpdate struct {
	CPUMillisUsed   int64
	MemoryBytesUsed int64
	DiskBytesUsed   int64
	NetworkRXBytes  int64
	NetworkTXBytes  int64
	MetricsJSON      string
	LastSeenAt       time.Time
}

type runtimePodRepository struct {
	sess db.Session
}

func NewRuntimePodRepository(sess db.Session) RuntimePodRepository {
	return &runtimePodRepository{sess: sess}
}

func (r *runtimePodRepository) collection() db.Collection {
	return r.sess.Collection("runtime_pods")
}
```

Then add each method in the same file. The `TryClaimSlot` implementation must use this SQL shape:

```go
func (r *runtimePodRepository) TryClaimSlot(ctx context.Context, podID int64) (bool, error) {
	res, err := r.sess.SQL().
		ExecContext(ctx, `UPDATE runtime_pods
			SET used_slots = used_slots + 1
			WHERE id = ? AND state = 'ready' AND draining = 0 AND used_slots < capacity`, podID)
	if err != nil {
		return false, err
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return affected == 1, nil
}
```

`backend/internal/repository/instance_runtime_binding_repository.go` must include:

```go
type InstanceRuntimeBindingRepository interface {
	Create(ctx context.Context, binding *models.InstanceRuntimeBinding) error
	GetByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error)
	GetRunningByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error)
	ListByRuntimePodID(ctx context.Context, runtimePodID int64) ([]models.InstanceRuntimeBinding, error)
	ListByRuntimePodIDs(ctx context.Context, runtimePodIDs []int64) ([]models.InstanceRuntimeBinding, error)
	UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error
	UpdateState(ctx context.Context, instanceID int, generation int, state string, message *string) error
	DeleteByInstanceID(ctx context.Context, instanceID int) error
}
```

Extend `backend/internal/repository/instance_repository.go` with:

```go
	GetV2DesiredRunning(ctx context.Context, limit int) ([]models.Instance, error)
	GetV2Creating(ctx context.Context, limit int) ([]models.Instance, error)
	UpdateRuntimeState(ctx context.Context, id int, status string, generation int, message *string) error
	SetWorkspacePath(ctx context.Context, id int, workspacePath string) error
	UpdateWorkspaceUsage(ctx context.Context, id int, usageBytes int64) error
```

- [ ] **Step 7: Run backend tests for schema compilation**

Run:

```powershell
go test ./backend/internal/db ./backend/internal/models ./backend/internal/repository -count=1
```

Expected: pass, or report packages with no test files and no compile errors.

- [ ] **Step 8: Commit schema and repository layer**

Run:

```powershell
git add backend/internal/db backend/internal/models backend/internal/repository
git commit -m "feat: add runtime pool schema"
```

## Task 2: Agent Contract Documentation And HTTP Client

**Files:**
- Create: `docs/clawmanager-agent-v2-contract.md`
- Create: `backend/internal/services/runtime_agent_client.go`
- Create: `backend/internal/services/runtime_agent_client_test.go`

- [ ] **Step 1: Write the agent contract document**

Create `docs/clawmanager-agent-v2-contract.md` with these sections:

```markdown
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

### POST /v1/gateways

The agent creates a gateway subprocess and returns the bound port. If no port is free in the requested range, return HTTP 409.

### DELETE /v1/gateways/{gateway_id}

The agent stops the gateway and releases cgroup, process, and port resources.

### POST /v1/drain

The agent marks the Pod as draining and rejects new gateway creation. Existing gateways continue until ClawManager migrates them.

## Isolation Requirements

Every gateway runs as UID/GID `200000 + instance_id`. The agent rejects workspace paths outside `/workspaces/{runtime}/user-{user_id}/instance-{instance_id}` after resolving symlinks. CPU and memory limits are enforced with cgroups. Workspace quota is enforced before writes.
```

- [ ] **Step 2: Write failing client tests**

Create `backend/internal/services/runtime_agent_client_test.go`:

```go
package services

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRuntimeAgentClientCreateGateway(t *testing.T) {
	var gotToken string
	var gotReq RuntimeAgentCreateGatewayRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/gateways" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		gotToken = r.Header.Get("X-ClawManager-Control-Token")
		if err := json.NewDecoder(r.Body).Decode(&gotReq); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_ = json.NewEncoder(w).Encode(RuntimeAgentCreateGatewayResponse{
			GatewayID: "gw-7-3",
			Port:      20017,
			PID:       intPtr(8842),
			Status:    "running",
		})
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	resp, err := client.CreateGateway(context.Background(), server.URL, RuntimeAgentCreateGatewayRequest{
		InstanceID:    7,
		UserID:        8,
		AgentType:     "openclaw",
		WorkspacePath: "/workspaces/openclaw/user-8/instance-7",
		PortRange:    RuntimeAgentPortRange{Start: 20000, End: 20299},
		UID:           200007,
		GID:           200007,
		CPUCores:      2,
		MemoryMB:      4096,
		DiskQuotaMB:   20480,
		Generation:    3,
	})
	if err != nil {
		t.Fatalf("CreateGateway returned error: %v", err)
	}
	if gotToken != "secret" {
		t.Fatalf("unexpected token %q", gotToken)
	}
	if gotReq.InstanceID != 7 || gotReq.PortRange.Start != 20000 {
		t.Fatalf("unexpected request %#v", gotReq)
	}
	if resp.GatewayID != "gw-7-3" || resp.Port != 20017 {
		t.Fatalf("unexpected response %#v", resp)
	}
}

func TestRuntimeAgentClientConflict(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no free port", http.StatusConflict)
	}))
	defer server.Close()

	client := NewRuntimeAgentClient("secret")
	_, err := client.CreateGateway(context.Background(), server.URL, RuntimeAgentCreateGatewayRequest{})
	if err == nil || err.Error() != "runtime agent conflict: no free port\n" {
		t.Fatalf("unexpected error %v", err)
	}
}

func intPtr(v int) *int {
	return &v
}
```

- [ ] **Step 3: Run client tests and verify they fail**

Run:

```powershell
go test ./backend/internal/services -run RuntimeAgentClient -count=1
```

Expected: fail because `RuntimeAgentCreateGatewayRequest` and `NewRuntimeAgentClient` do not exist.

- [ ] **Step 4: Implement runtime agent client**

Create `backend/internal/services/runtime_agent_client.go`:

```go
package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type RuntimeAgentClient interface {
	Health(ctx context.Context, endpoint string) error
	CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error)
	DeleteGateway(ctx context.Context, endpoint, gatewayID string) error
	Drain(ctx context.Context, endpoint string) error
}

type RuntimeAgentPortRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type RuntimeAgentCreateGatewayRequest struct {
	InstanceID    int                   `json:"instance_id"`
	UserID        int                   `json:"user_id"`
	AgentType     string                `json:"agent_type"`
	WorkspacePath string                `json:"workspace_path"`
	PortRange     RuntimeAgentPortRange `json:"port_range"`
	UID           int                   `json:"uid"`
	GID           int                   `json:"gid"`
	CPUCores      float64               `json:"cpu_cores"`
	MemoryMB      int                   `json:"memory_mb"`
	DiskQuotaMB   int                   `json:"disk_quota_mb"`
	Generation    int                   `json:"generation"`
}

type RuntimeAgentCreateGatewayResponse struct {
	GatewayID string `json:"gateway_id"`
	Port      int    `json:"port"`
	PID       *int   `json:"pid,omitempty"`
	Status    string `json:"status"`
}

type runtimeAgentHTTPClient struct {
	controlToken string
	httpClient   *http.Client
}

func NewRuntimeAgentClient(controlToken string) RuntimeAgentClient {
	return &runtimeAgentHTTPClient{
		controlToken: controlToken,
		httpClient: &http.Client{
			Timeout: 5 * time.Second,
		},
	}
}

func (c *runtimeAgentHTTPClient) Health(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodGet, endpoint, "/v1/health", nil, nil)
}

func (c *runtimeAgentHTTPClient) CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	var resp RuntimeAgentCreateGatewayResponse
	if err := c.do(ctx, http.MethodPost, endpoint, "/v1/gateways", req, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

func (c *runtimeAgentHTTPClient) DeleteGateway(ctx context.Context, endpoint, gatewayID string) error {
	return c.do(ctx, http.MethodDelete, endpoint, "/v1/gateways/"+gatewayID, nil, nil)
}

func (c *runtimeAgentHTTPClient) Drain(ctx context.Context, endpoint string) error {
	return c.do(ctx, http.MethodPost, endpoint, "/v1/drain", map[string]bool{"draining": true}, nil)
}

func (c *runtimeAgentHTTPClient) do(ctx context.Context, method, endpoint, path string, body any, out any) error {
	endpoint = strings.TrimRight(endpoint, "/")
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-ClawManager-Control-Token", c.controlToken)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		if resp.StatusCode == http.StatusConflict {
			return fmt.Errorf("runtime agent conflict: %s", string(msg))
		}
		return fmt.Errorf("runtime agent status %d: %s", resp.StatusCode, string(msg))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
```

- [ ] **Step 5: Run client tests**

Run:

```powershell
go test ./backend/internal/services -run RuntimeAgentClient -count=1
```

Expected: pass.

- [ ] **Step 6: Commit agent contract and client**

Run:

```powershell
git add docs/clawmanager-agent-v2-contract.md backend/internal/services/runtime_agent_client.go backend/internal/services/runtime_agent_client_test.go
git commit -m "feat: add runtime agent contract"
```

## Task 3: Runtime Capacity, Kubernetes Runtime Deployments, And Leader Election

**Files:**
- Create: `backend/internal/services/runtime_capacity.go`
- Create: `backend/internal/services/runtime_capacity_test.go`
- Create: `backend/internal/services/runtime_leader.go`
- Create: `backend/internal/services/k8s/runtime_deployment_service.go`
- Create: `backend/internal/services/k8s/runtime_deployment_service_test.go`
- Modify: `backend/internal/config/config.go`

- [ ] **Step 1: Write runtime capacity tests**

Create `backend/internal/services/runtime_capacity_test.go`:

```go
package services

import "testing"

func TestNormalizeV2RuntimeType(t *testing.T) {
	tests := map[string]string{
		"openclaw": "openclaw",
		"hermes":   "hermes",
		"webtop":   "",
		"ubuntu":   "",
	}
	for input, want := range tests {
		got, ok := NormalizeV2RuntimeType(input)
		if want == "" {
			if ok {
				t.Fatalf("expected %s to be rejected", input)
			}
			continue
		}
		if !ok || got != want {
			t.Fatalf("NormalizeV2RuntimeType(%q) = %q, %v", input, got, ok)
		}
	}
}

func TestRuntimeWorkspacePath(t *testing.T) {
	got := RuntimeWorkspacePath("openclaw", 45, 123)
	want := "/workspaces/openclaw/user-45/instance-123"
	if got != want {
		t.Fatalf("workspace path mismatch: want %s got %s", want, got)
	}
}

func TestRuntimeLinuxID(t *testing.T) {
	got := RuntimeLinuxID(123)
	if got != 200123 {
		t.Fatalf("runtime linux id mismatch: %d", got)
	}
}
```

- [ ] **Step 2: Implement capacity helpers**

Create `backend/internal/services/runtime_capacity.go`:

```go
package services

import "fmt"

const (
	RuntimeTypeOpenClaw = "openclaw"
	RuntimeTypeHermes   = "hermes"

	RuntimeGatewayPortStart = 20000
	RuntimeGatewayPortEnd   = 20299
	RuntimePodCapacity      = 100
	RuntimeLinuxIDBase      = 200000
)

func NormalizeV2RuntimeType(instanceType string) (string, bool) {
	switch instanceType {
	case RuntimeTypeOpenClaw:
		return RuntimeTypeOpenClaw, true
	case RuntimeTypeHermes:
		return RuntimeTypeHermes, true
	default:
		return "", false
	}
}

func RuntimeWorkspacePath(runtimeType string, userID int, instanceID int) string {
	return fmt.Sprintf("/workspaces/%s/user-%d/instance-%d", runtimeType, userID, instanceID)
}

func RuntimeLinuxID(instanceID int) int {
	return RuntimeLinuxIDBase + instanceID
}
```

- [ ] **Step 3: Run capacity tests**

Run:

```powershell
go test ./backend/internal/services -run 'NormalizeV2RuntimeType|RuntimeWorkspacePath|RuntimeLinuxID' -count=1
```

Expected: pass.

- [ ] **Step 4: Add runtime config fields**

Modify `backend/internal/config/config.go` by adding fields to the main config struct:

```go
	Runtime RuntimeConfig
```

Add this struct and env parsing:

```go
type RuntimeConfig struct {
	Namespace          string
	WorkspaceRoot      string
	AgentControlToken  string
	AgentReportToken   string
	BackendReplicaID   string
	RedisURL           string
	SchedulerEnabled   bool
	HeartbeatTimeout   time.Duration
	SchedulerTick       time.Duration
	OpenClawImage      string
	HermesImage         string
	MaxGatewaysPerPod   int
	GatewayPortStart    int
	GatewayPortEnd      int
}
```

Parse with defaults:

```go
Runtime: RuntimeConfig{
	Namespace:        getEnv("RUNTIME_NAMESPACE", getEnv("K8S_NAMESPACE", "clawmanager-system")),
	WorkspaceRoot:    getEnv("RUNTIME_WORKSPACE_ROOT", "/workspaces"),
	AgentControlToken: getEnv("RUNTIME_AGENT_CONTROL_TOKEN", ""),
	AgentReportToken:  getEnv("RUNTIME_AGENT_REPORT_TOKEN", ""),
	BackendReplicaID:  getEnv("HOSTNAME", "clawmanager-backend-local"),
	RedisURL:          getEnv("PLATFORM_REDIS_URL", getEnv("TEAM_REDIS_URL", "")),
	SchedulerEnabled:  getEnvBool("RUNTIME_SCHEDULER_ENABLED", true),
	HeartbeatTimeout:  getEnvDuration("RUNTIME_HEARTBEAT_TIMEOUT", 10*time.Second),
	SchedulerTick:     getEnvDuration("RUNTIME_SCHEDULER_TICK", 2*time.Second),
	OpenClawImage:     getEnv("OPENCLAW_RUNTIME_IMAGE", "ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:latest"),
	HermesImage:        getEnv("HERMES_RUNTIME_IMAGE", "ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:latest"),
	MaxGatewaysPerPod:  getEnvInt("RUNTIME_MAX_GATEWAYS_PER_POD", RuntimePodCapacity),
	GatewayPortStart:  getEnvInt("RUNTIME_GATEWAY_PORT_START", RuntimeGatewayPortStart),
	GatewayPortEnd:    getEnvInt("RUNTIME_GATEWAY_PORT_END", RuntimeGatewayPortEnd),
},
```

If `getEnvDuration`, `getEnvBool`, or `getEnvInt` is missing, add them in `config.go` using `strconv` and `time.ParseDuration`.

- [ ] **Step 5: Add Kubernetes runtime deployment service test**

Create `backend/internal/services/k8s/runtime_deployment_service_test.go` with a unit test that constructs the Deployment and checks the shared workspace mount:

```go
package k8s

import "testing"

func TestBuildRuntimeDeploymentUsesSharedWorkspaceAndAgentPort(t *testing.T) {
	dep := BuildRuntimeDeployment(RuntimeDeploymentSpec{
		Name:              "openclaw-runtime",
		Namespace:         "clawmanager-system",
		RuntimeType:       "openclaw",
		Image:             "example/openclaw:latest",
		Replicas:          2,
		WorkspaceNFSServer: "workspace-store.clawmanager-system.svc.cluster.local",
		WorkspaceNFSPath:   "/exports/workspaces",
		AgentControlToken:  "control",
		AgentReportToken:   "report",
	})

	if dep.Name != "openclaw-runtime" || *dep.Spec.Replicas != 2 {
		t.Fatalf("unexpected deployment identity %#v", dep)
	}
	container := dep.Spec.Template.Spec.Containers[0]
	if container.Ports[0].ContainerPort != 19090 {
		t.Fatalf("agent port missing: %#v", container.Ports)
	}
	foundWorkspace := false
	for _, mount := range container.VolumeMounts {
		if mount.Name == "workspaces" && mount.MountPath == "/workspaces" {
			foundWorkspace = true
		}
	}
	if !foundWorkspace {
		t.Fatalf("workspace mount missing: %#v", container.VolumeMounts)
	}
}
```

- [ ] **Step 6: Implement Kubernetes runtime deployment service**

Create `backend/internal/services/k8s/runtime_deployment_service.go` with:

```go
package k8s

import (
	"context"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/client-go/kubernetes"
)

type RuntimeDeploymentSpec struct {
	Name               string
	Namespace          string
	RuntimeType        string
	Image              string
	Replicas           int32
	WorkspaceNFSServer string
	WorkspaceNFSPath   string
	AgentControlToken  string
	AgentReportToken   string
}

type RuntimeDeploymentService interface {
	Ensure(ctx context.Context, spec RuntimeDeploymentSpec) error
	Scale(ctx context.Context, namespace, name string, replicas int32) error
}

type runtimeDeploymentService struct {
	client kubernetes.Interface
}

func NewRuntimeDeploymentService(client kubernetes.Interface) RuntimeDeploymentService {
	return &runtimeDeploymentService{client: client}
}

func int32Ptr(v int32) *int32 {
	return &v
}

func BuildRuntimeDeployment(spec RuntimeDeploymentSpec) *appsv1.Deployment {
	labels := map[string]string{
		"app":                         spec.Name,
		"clawmanager.io/runtime-type": spec.RuntimeType,
	}
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      spec.Name,
			Namespace: spec.Namespace,
			Labels:    labels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: int32Ptr(spec.Replicas),
			Selector: &metav1.LabelSelector{MatchLabels: labels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{Labels: labels},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Name:  "runtime",
						Image: spec.Image,
						Ports: []corev1.ContainerPort{{
							Name:          "agent",
							ContainerPort: 19090,
						}},
						Env: []corev1.EnvVar{
							{Name: "CLAWMANAGER_RUNTIME_TYPE", Value: spec.RuntimeType},
							{Name: "CLAWMANAGER_AGENT_PORT", Value: "19090"},
							{Name: "CLAWMANAGER_GATEWAY_PORT_START", Value: "20000"},
							{Name: "CLAWMANAGER_GATEWAY_PORT_END", Value: "20299"},
							{Name: "CLAWMANAGER_AGENT_CONTROL_TOKEN", Value: spec.AgentControlToken},
							{Name: "CLAWMANAGER_AGENT_REPORT_TOKEN", Value: spec.AgentReportToken},
						},
						Resources: corev1.ResourceRequirements{
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("500m"),
								corev1.ResourceMemory: resource.MustParse("1Gi"),
							},
						},
						VolumeMounts: []corev1.VolumeMount{{
							Name:      "workspaces",
							MountPath: "/workspaces",
						}},
					}},
					Volumes: []corev1.Volume{{
						Name: "workspaces",
						VolumeSource: corev1.VolumeSource{
							NFS: &corev1.NFSVolumeSource{
								Server: spec.WorkspaceNFSServer,
								Path:   spec.WorkspaceNFSPath,
							},
						},
					}},
				},
			},
		},
	}
}
```

Add `Ensure` and `Scale` using AppsV1 Deployments `Get`, `Create`, `Update`, and `UpdateScale`.

- [ ] **Step 7: Implement Lease leader service**

Create `backend/internal/services/runtime_leader.go`:

```go
package services

import (
	"context"
	"time"

	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type RuntimeLeaderService interface {
	IsLeader(ctx context.Context) bool
}

type runtimeLeaderService struct {
	client    kubernetes.Interface
	namespace string
	name      string
	holder    string
	ttl       time.Duration
}

func NewRuntimeLeaderService(client kubernetes.Interface, namespace, holder string) RuntimeLeaderService {
	return &runtimeLeaderService{
		client:    client,
		namespace: namespace,
		name:      "clawmanager-runtime-scheduler",
		holder:    holder,
		ttl:       15 * time.Second,
	}
}

func (s *runtimeLeaderService) IsLeader(ctx context.Context) bool {
	now := metav1.Now()
	leaseClient := s.client.CoordinationV1().Leases(s.namespace)
	lease, err := leaseClient.Get(ctx, s.name, metav1.GetOptions{})
	if err != nil {
		lease = &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{Name: s.name, Namespace: s.namespace},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       &s.holder,
				RenewTime:            &now,
				LeaseDurationSeconds: int32Ptr32(int32(s.ttl.Seconds())),
			},
		}
		_, createErr := leaseClient.Create(ctx, lease, metav1.CreateOptions{})
		return createErr == nil
	}
	if lease.Spec.HolderIdentity != nil && *lease.Spec.HolderIdentity != s.holder && lease.Spec.RenewTime != nil {
		expires := lease.Spec.RenewTime.Time.Add(s.ttl)
		if time.Now().Before(expires) {
			return false
		}
	}
	lease.Spec.HolderIdentity = &s.holder
	lease.Spec.RenewTime = &now
	_, err = leaseClient.Update(ctx, lease, metav1.UpdateOptions{})
	return err == nil
}

func int32Ptr32(v int32) *int32 {
	return &v
}
```

- [ ] **Step 8: Run runtime deployment and leader tests**

Run:

```powershell
go test ./backend/internal/services ./backend/internal/services/k8s -run 'Runtime|Leader' -count=1
```

Expected: pass.

- [ ] **Step 9: Commit runtime capacity and Kubernetes services**

Run:

```powershell
git add backend/internal/config backend/internal/services backend/internal/services/k8s
git commit -m "feat: add runtime pool infrastructure services"
```

## Task 4: Runtime Scheduler And Redis Event Fanout

**Files:**
- Create: `backend/internal/services/redis_client.go`
- Create: `backend/internal/services/runtime_events.go`
- Create: `backend/internal/services/runtime_scheduler.go`
- Create: `backend/internal/services/runtime_scheduler_test.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Extract a platform Redis client**

Create `backend/internal/services/redis_client.go` by reusing the existing RESP parsing pattern from `team_redis.go`. The exported interface must be:

```go
type PlatformRedisClient interface {
	SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error)
	Del(ctx context.Context, key string) error
	XAdd(ctx context.Context, key string, fields map[string]string) (string, error)
	XRead(ctx context.Context, key, lastID string, block time.Duration) ([]redisStreamMessage, error)
}
```

Use the same URL parsing behavior as `newRedisBus`, and add:

```go
func NewPlatformRedisClient(rawURL string) (PlatformRedisClient, error) {
	return newRedisBus(rawURL)
}
```

Extend `redisBus` in `team_redis.go` only if needed so the same type satisfies `SetNX` and `Del`:

```go
func (b *redisBus) SetNX(ctx context.Context, key, value string, ttl time.Duration) (bool, error) {
	reply, err := b.do(ctx, "SET", key, value, "NX", "PX", fmt.Sprintf("%d", ttl.Milliseconds()))
	if err != nil {
		return false, err
	}
	return reply == "OK", nil
}

func (b *redisBus) Del(ctx context.Context, key string) error {
	_, err := b.do(ctx, "DEL", key)
	return err
}
```

- [ ] **Step 2: Add runtime event service**

Create `backend/internal/services/runtime_events.go`:

```go
package services

import (
	"context"
	"encoding/json"
	"time"
)

const runtimeEventStreamKey = "clawmanager:runtime-events"

type RuntimeEvent struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type RuntimeEventService interface {
	Publish(ctx context.Context, eventType string, payload any) error
	Read(ctx context.Context, lastID string, block time.Duration) ([]redisStreamMessage, error)
}

type runtimeEventService struct {
	redis PlatformRedisClient
}

func NewRuntimeEventService(redis PlatformRedisClient) RuntimeEventService {
	return &runtimeEventService{redis: redis}
}

func (s *runtimeEventService) Publish(ctx context.Context, eventType string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	event := RuntimeEvent{Type: eventType, Payload: raw, CreatedAt: time.Now().UTC()}
	eventRaw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	_, err = s.redis.XAdd(ctx, runtimeEventStreamKey, map[string]string{"event": string(eventRaw)})
	return err
}

func (s *runtimeEventService) Read(ctx context.Context, lastID string, block time.Duration) ([]redisStreamMessage, error) {
	return s.redis.XRead(ctx, runtimeEventStreamKey, lastID, block)
}
```

- [ ] **Step 3: Write scheduler assignment test with fakes**

Create `backend/internal/services/runtime_scheduler_test.go` with fake repositories and this test:

```go
func TestRuntimeSchedulerAssignsCreatingInstanceToReadyPod(t *testing.T) {
	instance := models.Instance{ID: 123, UserID: 45, Type: "openclaw", CPUCores: 2, MemoryGB: 4, DiskGB: 20, RuntimeGeneration: 1}
	pod := models.RuntimePod{ID: 9, RuntimeType: "openclaw", PodIP: stringPtr("10.42.0.31"), AgentEndpoint: stringPtr("http://10.42.0.31:19090"), State: "ready", Capacity: 100, UsedSlots: 0}
	s := newRuntimeSchedulerForTest(instance, pod)

	if err := s.assignInstance(context.Background(), instance); err != nil {
		t.Fatalf("assignInstance returned error: %v", err)
	}
	if s.fakeAgent.createGatewayCalls != 1 {
		t.Fatalf("expected one create gateway call, got %d", s.fakeAgent.createGatewayCalls)
	}
	binding := s.fakeBindings.byInstance[123]
	if binding.GatewayPort != 20017 || binding.RuntimePodID != 9 {
		t.Fatalf("unexpected binding %#v", binding)
	}
}
```

The fake agent must return:

```go
&RuntimeAgentCreateGatewayResponse{
	GatewayID: "gw-123-1",
	Port:      20017,
	PID:       intPtr(8842),
	Status:    "running",
}
```

- [ ] **Step 4: Implement scheduler**

Create `backend/internal/services/runtime_scheduler.go` with:

```go
package services

import (
	"context"
	"fmt"
	"log"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	k8ssvc "clawreef/internal/services/k8s"
)

type RuntimeScheduler struct {
	instanceRepo repository.InstanceRepository
	podRepo      repository.RuntimePodRepository
	bindingRepo  repository.InstanceRuntimeBindingRepository
	rolloutRepo  repository.RuntimeRolloutRepository
	agentClient  RuntimeAgentClient
	events       RuntimeEventService
	leader       RuntimeLeaderService
	deployments  k8ssvc.RuntimeDeploymentService
	tick         time.Duration
}

func NewRuntimeScheduler(
	instanceRepo repository.InstanceRepository,
	podRepo repository.RuntimePodRepository,
	bindingRepo repository.InstanceRuntimeBindingRepository,
	rolloutRepo repository.RuntimeRolloutRepository,
	agentClient RuntimeAgentClient,
	events RuntimeEventService,
	leader RuntimeLeaderService,
	deployments k8ssvc.RuntimeDeploymentService,
	tick time.Duration,
) *RuntimeScheduler {
	return &RuntimeScheduler{
		instanceRepo: instanceRepo,
		podRepo: podRepo,
		bindingRepo: bindingRepo,
		rolloutRepo: rolloutRepo,
		agentClient: agentClient,
		events: events,
		leader: leader,
		deployments: deployments,
		tick: tick,
	}
}

func (s *RuntimeScheduler) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.tick)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !s.leader.IsLeader(ctx) {
					continue
				}
				if err := s.reconcile(ctx); err != nil {
					log.Printf("runtime scheduler reconcile failed: %v", err)
				}
			}
		}
	}()
}

func (s *RuntimeScheduler) reconcile(ctx context.Context) error {
	creating, err := s.instanceRepo.GetV2Creating(ctx, 100)
	if err != nil {
		return err
	}
	for _, instance := range creating {
		if err := s.assignInstance(ctx, instance); err != nil {
			msg := err.Error()
			_ = s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "error", instance.RuntimeGeneration, &msg)
		}
	}
	running, err := s.instanceRepo.GetV2DesiredRunning(ctx, 200)
	if err != nil {
		return err
	}
	for _, instance := range running {
		if _, err := s.bindingRepo.GetRunningByInstanceID(ctx, instance.ID); err != nil {
			if assignErr := s.assignInstance(ctx, instance); assignErr != nil {
				msg := assignErr.Error()
				_ = s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "error", instance.RuntimeGeneration, &msg)
			}
		}
	}
	return nil
}

func (s *RuntimeScheduler) assignInstance(ctx context.Context, instance models.Instance) error {
	runtimeType, ok := NormalizeV2RuntimeType(instance.Type)
	if !ok {
		return fmt.Errorf("unsupported V2 runtime type %s", instance.Type)
	}
	pods, err := s.podRepo.ListSchedulable(ctx, runtimeType)
	if err != nil {
		return err
	}
	for _, pod := range pods {
		if pod.AgentEndpoint == nil {
			continue
		}
		claimed, err := s.podRepo.TryClaimSlot(ctx, pod.ID)
		if err != nil {
			return err
		}
		if !claimed {
			continue
		}
		if err := s.createGatewayOnPod(ctx, instance, pod); err != nil {
			_ = s.podRepo.ReleaseSlot(ctx, pod.ID)
			continue
		}
		return nil
	}
	return fmt.Errorf("no schedulable runtime pod for %s", runtimeType)
}

func (s *RuntimeScheduler) createGatewayOnPod(ctx context.Context, instance models.Instance, pod models.RuntimePod) error {
	workspacePath := RuntimeWorkspacePath(pod.RuntimeType, instance.UserID, instance.ID)
	linuxID := RuntimeLinuxID(instance.ID)
	resp, err := s.agentClient.CreateGateway(ctx, *pod.AgentEndpoint, RuntimeAgentCreateGatewayRequest{
		InstanceID:    instance.ID,
		UserID:        instance.UserID,
		AgentType:     pod.RuntimeType,
		WorkspacePath: workspacePath,
		PortRange:     RuntimeAgentPortRange{Start: RuntimeGatewayPortStart, End: RuntimeGatewayPortEnd},
		UID:           linuxID,
		GID:           linuxID,
		CPUCores:      instance.CPUCores,
		MemoryMB:      instance.MemoryGB * 1024,
		DiskQuotaMB:   instance.DiskGB * 1024,
		Generation:    instance.RuntimeGeneration,
	})
	if err != nil {
		return err
	}
	binding := &models.InstanceRuntimeBinding{
		InstanceID:    instance.ID,
		RuntimePodID:  pod.ID,
		RuntimeType:   pod.RuntimeType,
		GatewayID:     resp.GatewayID,
		GatewayPort:   resp.Port,
		GatewayPID:    resp.PID,
		WorkspacePath: workspacePath,
		State:         "running",
		Generation:    instance.RuntimeGeneration,
	}
	if err := s.bindingRepo.Create(ctx, binding); err != nil {
		return err
	}
	if err := s.instanceRepo.SetWorkspacePath(ctx, instance.ID, workspacePath); err != nil {
		return err
	}
	return s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "running", instance.RuntimeGeneration, nil)
}
```

Add failover and rollout in the same file with these public methods:

```go
func (s *RuntimeScheduler) DrainPod(ctx context.Context, podID int64) error
func (s *RuntimeScheduler) FailoverPod(ctx context.Context, podID int64, reason string) error
func (s *RuntimeScheduler) StartRollout(ctx context.Context, rolloutID int64) error
```

`FailoverPod` must mark the Pod `unhealthy`, list its bindings, delete each binding, release the old slot, increment instance generation, set instance status `creating`, and let the next scheduler tick recreate the gateway on another Pod.

- [ ] **Step 5: Wire scheduler in `main.go`**

In `backend/cmd/server/main.go`, create the new repositories after the existing repositories:

```go
runtimePodRepo := repository.NewRuntimePodRepository(sess)
bindingRepo := repository.NewInstanceRuntimeBindingRepository(sess)
rolloutRepo := repository.NewRuntimeRolloutRepository(sess)
```

Create Redis and runtime services:

```go
platformRedis, err := services.NewPlatformRedisClient(cfg.Runtime.RedisURL)
if err != nil {
	log.Printf("platform redis disabled: %v", err)
}
runtimeEvents := services.NewRuntimeEventService(platformRedis)
agentClient := services.NewRuntimeAgentClient(cfg.Runtime.AgentControlToken)
leader := services.NewRuntimeLeaderService(k8sClient, cfg.Runtime.Namespace, cfg.Runtime.BackendReplicaID)
runtimeDeployments := k8s.NewRuntimeDeploymentService(k8sClient)
runtimeScheduler := services.NewRuntimeScheduler(instanceRepo, runtimePodRepo, bindingRepo, rolloutRepo, agentClient, runtimeEvents, leader, runtimeDeployments, cfg.Runtime.SchedulerTick)
if cfg.Runtime.SchedulerEnabled {
	runtimeScheduler.Start(ctx)
}
```

If `platformRedis` can be nil in local development, make `NewRuntimeEventService` accept nil and return a no-op service that never panics.

- [ ] **Step 6: Run scheduler tests**

Run:

```powershell
go test ./backend/internal/services -run RuntimeScheduler -count=1
```

Expected: pass.

- [ ] **Step 7: Commit scheduler**

Run:

```powershell
git add backend/internal/services backend/cmd/server/main.go
git commit -m "feat: add runtime scheduler"
```

## Task 5: V2 Instance Lifecycle And Pod-IP Proxy

**Files:**
- Modify: `backend/internal/services/instance_service.go`
- Modify: `backend/internal/services/instance_proxy_service.go`
- Modify: `backend/internal/services/sync_service.go`
- Modify: `backend/internal/handlers/instance_handler.go`
- Modify: `backend/cmd/server/main.go`
- Create: `backend/internal/services/instance_proxy_service_v2_test.go`

- [ ] **Step 1: Add proxy test for binding target**

Create `backend/internal/services/instance_proxy_service_v2_test.go` with a test that builds an instance, a binding, and a runtime Pod:

```go
func TestInstanceProxyServiceUsesRuntimeBindingForV2(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/apps/openclaw" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer target.Close()

	podIP, port := splitHostPortForTest(t, target.URL)
	instance := models.Instance{ID: 123, UserID: 45, Type: "openclaw", Status: "running"}
	binding := models.InstanceRuntimeBinding{InstanceID: 123, RuntimePodID: 9, GatewayPort: port, State: "running"}
	pod := models.RuntimePod{ID: 9, PodIP: &podIP, State: "ready"}

	service := newProxyServiceForV2Test(instance, binding, pod)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/instances/123/proxy/apps/openclaw", nil)
	rec := httptest.NewRecorder()

	service.Proxy(rec, req, 123, "/apps/openclaw")
	if rec.Code != http.StatusOK || rec.Body.String() != "ok" {
		t.Fatalf("unexpected proxy response %d %q", rec.Code, rec.Body.String())
	}
}
```

- [ ] **Step 2: Modify instance creation for V2**

In `backend/internal/services/instance_service.go`, keep legacy behavior for non-V2 types and add this branch near the start of create:

```go
runtimeType, isV2 := NormalizeV2RuntimeType(req.Type)
if isV2 {
	instance := &models.Instance{
		UserID:            userID,
		Name:              strings.TrimSpace(req.Name),
		Description:       normalizeStringPtr(req.Description),
		Type:              runtimeType,
		RuntimeType:       "gateway",
		Status:            "creating",
		CPUCores:          req.CPUCores,
		MemoryGB:          req.MemoryGB,
		DiskGB:            req.DiskGB,
		StorageClass:      strings.TrimSpace(req.StorageClass),
		MountPath:         "/workspaces",
		RuntimeGeneration: 1,
	}
	if err := s.instanceRepo.Create(ctx, instance); err != nil {
		return nil, err
	}
	workspacePath := RuntimeWorkspacePath(runtimeType, userID, instance.ID)
	if err := os.MkdirAll(workspacePath, 0750); err != nil {
		return nil, err
	}
	if err := s.instanceRepo.SetWorkspacePath(ctx, instance.ID, workspacePath); err != nil {
		return nil, err
	}
	instance.WorkspacePath = &workspacePath
	return instance, nil
}
```

Keep validation so new creation accepts only `openclaw` and `hermes`; old records with `ubuntu`, `webtop`, `debian`, `centos`, or `custom` remain readable and manageable through legacy paths.

- [ ] **Step 3: Modify start, stop, restart, delete**

For V2 instances in `instance_service.go`:

```go
func (s *instanceService) Start(ctx context.Context, userID int, instanceID int) error {
	instance, err := s.mustOwnInstance(ctx, userID, instanceID)
	if err != nil {
		return err
	}
	if _, ok := NormalizeV2RuntimeType(instance.Type); ok {
		return s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "creating", instance.RuntimeGeneration+1, nil)
	}
	return s.startLegacy(ctx, instance)
}

func (s *instanceService) Stop(ctx context.Context, userID int, instanceID int) error {
	instance, err := s.mustOwnInstance(ctx, userID, instanceID)
	if err != nil {
		return err
	}
	if _, ok := NormalizeV2RuntimeType(instance.Type); ok {
		binding, err := s.bindingRepo.GetByInstanceID(ctx, instance.ID)
		if err == nil {
			pod, podErr := s.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
			if podErr == nil && pod.AgentEndpoint != nil {
				_ = s.agentClient.DeleteGateway(ctx, *pod.AgentEndpoint, binding.GatewayID)
			}
			_ = s.bindingRepo.DeleteByInstanceID(ctx, instance.ID)
			_ = s.runtimePodRepo.ReleaseSlot(ctx, binding.RuntimePodID)
		}
		return s.instanceRepo.UpdateRuntimeState(ctx, instance.ID, "stopped", instance.RuntimeGeneration, nil)
	}
	return s.stopLegacy(ctx, instance)
}
```

Use the existing legacy code by moving old logic into private helpers `startLegacy`, `stopLegacy`, and `deleteLegacy`.

- [ ] **Step 4: Modify status for ordinary users**

In `backend/internal/handlers/instance_handler.go`, make `/instances/:id/status` return user-safe availability for V2:

```json
{
  "instance_status": {
    "instance_id": 123,
    "status": "running",
    "availability": "available",
    "agent_type": "openclaw",
    "workspace_usage_bytes": 123456
  }
}
```

Do not include Pod name, namespace, Pod IP, service name, port, capacity, node, or runtime scheduling fields for normal users.

- [ ] **Step 5: Modify proxy for V2**

In `backend/internal/services/instance_proxy_service.go`, resolve target for V2:

```go
func (s *instanceProxyService) resolveV2Target(ctx context.Context, instanceID int) (*url.URL, error) {
	binding, err := s.bindingRepo.GetRunningByInstanceID(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("instance gateway is not available")
	}
	pod, err := s.runtimePodRepo.GetByID(ctx, binding.RuntimePodID)
	if err != nil {
		return nil, err
	}
	if pod.PodIP == nil || *pod.PodIP == "" {
		return nil, fmt.Errorf("runtime pod ip is not available")
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(*pod.PodIP, strconv.Itoa(binding.GatewayPort))}, nil
}
```

Use this target when `NormalizeV2RuntimeType(instance.Type)` succeeds, otherwise execute the existing Service-based proxy path.

- [ ] **Step 6: Run lifecycle and proxy tests**

Run:

```powershell
go test ./backend/internal/services -run 'Instance|Proxy|RuntimeScheduler' -count=1
```

Expected: pass.

- [ ] **Step 7: Commit lifecycle and proxy**

Run:

```powershell
git add backend/internal/services backend/internal/handlers backend/cmd/server/main.go
git commit -m "feat: route V2 instances through runtime gateways"
```

## Task 6: Workspace File Manager Backend

**Files:**
- Create: `backend/internal/services/workspace_path_guard.go`
- Create: `backend/internal/services/workspace_path_guard_test.go`
- Create: `backend/internal/services/workspace_file_service.go`
- Create: `backend/internal/services/workspace_file_service_test.go`
- Create: `backend/internal/handlers/workspace_file_handler.go`
- Modify: `backend/internal/utils/response.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Write path guard tests**

Create `backend/internal/services/workspace_path_guard_test.go`:

```go
package services

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveWorkspacePathRejectsTraversal(t *testing.T) {
	root := t.TempDir()
	_, err := ResolveWorkspacePath(root, "../outside.txt")
	if err == nil || err.Error() != "workspace path escapes instance workspace" {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestResolveWorkspacePathRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	_, err := ResolveWorkspacePath(root, "escape/file.txt")
	if err == nil || err.Error() != "workspace path escapes instance workspace" {
		t.Fatalf("unexpected error %v", err)
	}
}

func TestResolveWorkspacePathAllowsNestedFile(t *testing.T) {
	root := t.TempDir()
	got, err := ResolveWorkspacePath(root, "a/b.txt")
	if err != nil {
		t.Fatalf("ResolveWorkspacePath returned error: %v", err)
	}
	want := filepath.Join(root, "a", "b.txt")
	if got != want {
		t.Fatalf("want %s got %s", want, got)
	}
}
```

- [ ] **Step 2: Implement path guard**

Create `backend/internal/services/workspace_path_guard.go`:

```go
package services

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ResolveWorkspacePath(workspaceRoot, relativePath string) (string, error) {
	workspaceRoot = filepath.Clean(workspaceRoot)
	cleanRelative := filepath.Clean("/" + strings.TrimSpace(relativePath))
	cleanRelative = strings.TrimPrefix(cleanRelative, string(filepath.Separator))
	fullPath := filepath.Join(workspaceRoot, cleanRelative)

	rootReal, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil {
		return "", err
	}
	parent := fullPath
	if info, statErr := os.Lstat(fullPath); statErr == nil && !info.IsDir() {
		parent = filepath.Dir(fullPath)
	}
	if _, statErr := os.Lstat(parent); statErr == nil {
		realParent, evalErr := filepath.EvalSymlinks(parent)
		if evalErr != nil {
			return "", evalErr
		}
		if !isPathInside(rootReal, realParent) {
			return "", fmt.Errorf("workspace path escapes instance workspace")
		}
	}
	if !isPathInside(workspaceRoot, fullPath) {
		return "", fmt.Errorf("workspace path escapes instance workspace")
	}
	return fullPath, nil
}

func isPathInside(root, candidate string) bool {
	rel, err := filepath.Rel(root, candidate)
	if err != nil {
		return false
	}
	return rel == "." || (!strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != "..")
}
```

- [ ] **Step 3: Write workspace service tests**

Create `backend/internal/services/workspace_file_service_test.go`:

```go
func TestWorkspaceFileServicePreviewTextLimit(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("hello"), 0640); err != nil {
		t.Fatal(err)
	}
	service := NewWorkspaceFileService(fakeAuditRepo{})
	preview, err := service.Preview(context.Background(), WorkspaceFileScope{InstanceID: 1, UserID: 2, WorkspacePath: root}, "small.txt")
	if err != nil {
		t.Fatalf("Preview returned error: %v", err)
	}
	if preview.Kind != "text" || preview.Text != "hello" {
		t.Fatalf("unexpected preview %#v", preview)
	}
}

func TestWorkspaceFileServiceRejectsLargeTextPreview(t *testing.T) {
	root := t.TempDir()
	large := bytes.Repeat([]byte("a"), 1024*1024+1)
	if err := os.WriteFile(filepath.Join(root, "large.log"), large, 0640); err != nil {
		t.Fatal(err)
	}
	service := NewWorkspaceFileService(fakeAuditRepo{})
	_, err := service.Preview(context.Background(), WorkspaceFileScope{InstanceID: 1, UserID: 2, WorkspacePath: root}, "large.log")
	if err == nil || err.Error() != "text preview exceeds 1 MiB" {
		t.Fatalf("unexpected error %v", err)
	}
}
```

- [ ] **Step 4: Implement workspace file service**

Create `backend/internal/services/workspace_file_service.go` with these exported types:

```go
type WorkspaceFileScope struct {
	InstanceID    int
	UserID        int
	WorkspacePath string
}

type WorkspaceEntry struct {
	Name         string    `json:"name"`
	Path         string    `json:"path"`
	IsDir        bool      `json:"is_dir"`
	Size         int64     `json:"size"`
	ModifiedAt   time.Time `json:"modified_at"`
	Previewable  bool      `json:"previewable"`
	Downloadable bool      `json:"downloadable"`
}

type WorkspacePreview struct {
	Kind        string `json:"kind"`
	ContentType string `json:"content_type"`
	Text        string `json:"text,omitempty"`
	URL         string `json:"url,omitempty"`
}

type WorkspaceFileService interface {
	List(ctx context.Context, scope WorkspaceFileScope, relativePath string) ([]WorkspaceEntry, error)
	Preview(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*WorkspacePreview, error)
	OpenDownload(ctx context.Context, scope WorkspaceFileScope, relativePath string) (*os.File, string, int64, error)
	Upload(ctx context.Context, scope WorkspaceFileScope, relativeDir string, filename string, reader io.Reader, size int64) error
	Mkdir(ctx context.Context, scope WorkspaceFileScope, relativePath string) error
	Rename(ctx context.Context, scope WorkspaceFileScope, oldPath, newPath string) error
	Delete(ctx context.Context, scope WorkspaceFileScope, relativePath string) error
}
```

Rules to implement:
- Text preview extensions: `.txt`, `.md`, `.json`, `.yaml`, `.yml`, `.log`, `.py`, `.js`, `.ts`, `.go`, `.sh`; max 1 MiB.
- Image preview extensions: `.png`, `.jpg`, `.jpeg`, `.gif`, `.webp`, `.svg`; max 10 MiB.
- PDF preview uses iframe URL and content type `application/pdf`.
- Unknown binary files are download-only.
- Upload max is read from env `WORKSPACE_UPLOAD_MAX_BYTES`, default `524288000`.
- Audit upload, mkdir, rename, delete, and download. Do not audit preview.

- [ ] **Step 5: Implement handler routes**

Create `backend/internal/handlers/workspace_file_handler.go` with endpoints:

```go
GET    /api/v1/instances/:id/workspace/files
GET    /api/v1/instances/:id/workspace/preview
GET    /api/v1/instances/:id/workspace/download
POST   /api/v1/instances/:id/workspace/upload
POST   /api/v1/instances/:id/workspace/folders
PATCH  /api/v1/instances/:id/workspace/entries
DELETE /api/v1/instances/:id/workspace/entries
```

Each handler must load the instance, confirm ownership from auth context, require `WorkspacePath != nil`, and create:

```go
scope := services.WorkspaceFileScope{
	InstanceID: instance.ID,
	UserID: userID,
	WorkspacePath: *instance.WorkspacePath,
}
```

- [ ] **Step 6: Wire workspace routes**

In `backend/cmd/server/main.go`:

```go
workspaceAuditRepo := repository.NewWorkspaceFileAuditRepository(sess)
workspaceFileService := services.NewWorkspaceFileService(workspaceAuditRepo)
workspaceFileHandler := handlers.NewWorkspaceFileHandler(instanceService, workspaceFileService)

instances := api.Group("/instances")
instances.GET("/:id/workspace/files", workspaceFileHandler.List)
instances.GET("/:id/workspace/preview", workspaceFileHandler.Preview)
instances.GET("/:id/workspace/download", workspaceFileHandler.Download)
instances.POST("/:id/workspace/upload", workspaceFileHandler.Upload)
instances.POST("/:id/workspace/folders", workspaceFileHandler.Mkdir)
instances.PATCH("/:id/workspace/entries", workspaceFileHandler.Rename)
instances.DELETE("/:id/workspace/entries", workspaceFileHandler.Delete)
```

- [ ] **Step 7: Run workspace backend tests**

Run:

```powershell
go test ./backend/internal/services ./backend/internal/handlers -run 'Workspace|ResolveWorkspacePath' -count=1
```

Expected: pass.

- [ ] **Step 8: Commit workspace backend**

Run:

```powershell
git add backend/internal/services backend/internal/handlers backend/internal/repository backend/internal/utils backend/cmd/server/main.go
git commit -m "feat: add workspace file APIs"
```

## Task 7: Runtime Agent Report And Admin APIs

**Files:**
- Create: `backend/internal/handlers/runtime_agent_handler.go`
- Create: `backend/internal/handlers/runtime_pool_handler.go`
- Create: `backend/internal/handlers/runtime_pool_handler_test.go`
- Modify: `backend/internal/services/websocket_service.go`
- Modify: `backend/cmd/server/main.go`

- [ ] **Step 1: Implement runtime agent report handler**

Create `backend/internal/handlers/runtime_agent_handler.go` with:

```go
type RuntimeAgentHandler struct {
	cfg          config.RuntimeConfig
	podRepo      repository.RuntimePodRepository
	bindingRepo  repository.InstanceRuntimeBindingRepository
	events       services.RuntimeEventService
}

func (h *RuntimeAgentHandler) requireAgentToken(c *gin.Context) bool {
	if h.cfg.AgentReportToken == "" || c.GetHeader("X-ClawManager-Agent-Token") != h.cfg.AgentReportToken {
		utils.Unauthorized(c, "invalid runtime agent token")
		return false
	}
	return true
}
```

Add handlers:
- `Register` upserts `runtime_pods`.
- `Heartbeat` updates state, used slots, draining, and last seen.
- `ReportMetrics` updates metrics and binding health, then publishes `runtime_pod_metrics` to Redis events.
- `ReportGateways` reconciles binding state from agent.
- `ReportSkills` stores skill reports using existing skill service hooks if available; if no hook exists, accept the request and publish a `runtime_agent_skills_reported` event.

- [ ] **Step 2: Implement admin runtime handler**

Create `backend/internal/handlers/runtime_pool_handler.go`:

```go
type RuntimePoolHandler struct {
	podRepo     repository.RuntimePodRepository
	bindingRepo repository.InstanceRuntimeBindingRepository
	rolloutRepo repository.RuntimeRolloutRepository
	scheduler   *services.RuntimeScheduler
}

func (h *RuntimePoolHandler) ListPods(c *gin.Context)
func (h *RuntimePoolHandler) GetPodGateways(c *gin.Context)
func (h *RuntimePoolHandler) DrainPod(c *gin.Context)
func (h *RuntimePoolHandler) StartRollout(c *gin.Context)
```

`ListPods` returns:

```json
{
  "pods": [
    {
      "id": 9,
      "runtime_type": "openclaw",
      "pod_name": "openclaw-runtime-abcde",
      "pod_ip": "10.42.0.31",
      "node_name": "node-a",
      "state": "ready",
      "used_slots": 37,
      "capacity": 100,
      "draining": false,
      "cpu_millis_used": 13600,
      "memory_bytes_used": 42949672960,
      "disk_bytes_used": 214748364800,
      "network_rx_bytes": 9223372,
      "network_tx_bytes": 19223372,
      "last_seen_at": "2026-06-01T10:00:00Z"
    }
  ]
}
```

- [ ] **Step 3: Add admin handler tests**

Create `backend/internal/handlers/runtime_pool_handler_test.go` and verify non-admin access is rejected and admin access returns metrics. Use existing auth middleware test helpers if present; otherwise call the handler directly with `gin.CreateTestContext`.

- [ ] **Step 4: Extend websocket service**

Modify `backend/internal/services/websocket_service.go`:

```go
type WebSocketTopic string

const (
	WebSocketTopicUser         WebSocketTopic = "user"
	WebSocketTopicRuntimeAdmin WebSocketTopic = "runtime_admin"
)
```

Add subscription filtering:

```go
type Client struct {
	UserID  int
	Role    string
	Topic   WebSocketTopic
	Send    chan []byte
}

func (h *Hub) BroadcastRuntimeAdmin(message []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for client := range h.clients {
		if client.Role == "admin" && client.Topic == WebSocketTopicRuntimeAdmin {
			select {
			case client.Send <- message:
			default:
			}
		}
	}
}
```

Start one goroutine per backend replica to `XREAD` `clawmanager:runtime-events` and broadcast runtime admin messages to local websocket clients.

- [ ] **Step 5: Wire routes**

In `backend/cmd/server/main.go`:

```go
runtimeAgentHandler := handlers.NewRuntimeAgentHandler(cfg.Runtime, runtimePodRepo, bindingRepo, runtimeEvents)
api.POST("/runtime-agent/register", runtimeAgentHandler.Register)
api.POST("/runtime-agent/heartbeat", runtimeAgentHandler.Heartbeat)
api.POST("/runtime-agent/gateways/report", runtimeAgentHandler.ReportGateways)
api.POST("/runtime-agent/skills/report", runtimeAgentHandler.ReportSkills)
api.POST("/runtime-agent/metrics/report", runtimeAgentHandler.ReportMetrics)

runtimePoolHandler := handlers.NewRuntimePoolHandler(runtimePodRepo, bindingRepo, rolloutRepo, runtimeScheduler)
admin.GET("/runtime-pods", runtimePoolHandler.ListPods)
admin.GET("/runtime-pods/:id/gateways", runtimePoolHandler.GetPodGateways)
admin.POST("/runtime-pods/:id/drain", runtimePoolHandler.DrainPod)
admin.POST("/runtime-rollouts", runtimePoolHandler.StartRollout)
```

- [ ] **Step 6: Run admin API tests**

Run:

```powershell
go test ./backend/internal/handlers ./backend/internal/services -run 'RuntimePool|RuntimeAgent|WebSocket' -count=1
```

Expected: pass.

- [ ] **Step 7: Commit admin runtime APIs**

Run:

```powershell
git add backend/internal/handlers backend/internal/services backend/cmd/server/main.go
git commit -m "feat: add runtime admin APIs"
```

## Task 8: Frontend Simple UI Foundation

**Files:**
- Modify: `frontend/package.json`
- Modify: package lock file if present after install.
- Modify: `frontend/src/index.css`
- Modify: `frontend/src/components/UserLayout.tsx`
- Modify: `frontend/src/components/AdminLayout.tsx`
- Modify: `frontend/src/App.css` if it still injects decorative gradients.

- [ ] **Step 1: Add icon dependency**

Run:

```powershell
cd frontend
npm install lucide-react
cd ..
```

Expected: `frontend/package.json` and the lock file include `lucide-react`.

- [ ] **Step 2: Replace global visual tokens**

Modify `frontend/src/index.css` so the design uses a neutral, simple base:

```css
:root {
  font-family:
    Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI",
    sans-serif;
  color: #111827;
  background: #f7f8fa;
  font-synthesis: none;
  text-rendering: optimizeLegibility;
  -webkit-font-smoothing: antialiased;
  -moz-osx-font-smoothing: grayscale;
}

body {
  margin: 0;
  min-width: 320px;
  min-height: 100vh;
  background: #f7f8fa;
}

button,
input,
select,
textarea {
  font: inherit;
}

.cm-surface {
  background: #ffffff;
  border: 1px solid #e5e7eb;
  border-radius: 8px;
}

.cm-button {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  min-height: 36px;
  border-radius: 6px;
  border: 1px solid #d1d5db;
  background: #ffffff;
  color: #111827;
}

.cm-button-primary {
  border-color: #2563eb;
  background: #2563eb;
  color: #ffffff;
}
```

Remove decorative gradient or orb classes from `App.css`.

- [ ] **Step 3: Simplify user layout**

Modify `frontend/src/components/UserLayout.tsx`:
- Use a plain top bar and left nav.
- Keep border radius at `rounded-lg` or smaller.
- Use `lucide-react` icons for nav and command buttons.
- Remove runtime details from ordinary user navigation labels.
- Keep language switcher and account actions.

Use this layout shape:

```tsx
<div className="min-h-screen bg-[#f7f8fa] text-gray-900">
  <aside className="fixed inset-y-0 left-0 hidden w-60 border-r border-gray-200 bg-white lg:block">
    ...
  </aside>
  <div className="lg:pl-60">
    <header className="sticky top-0 z-20 border-b border-gray-200 bg-white">
      ...
    </header>
    <main className="mx-auto w-full max-w-7xl px-4 py-5 sm:px-6 lg:px-8">
      <Outlet />
    </main>
  </div>
</div>
```

- [ ] **Step 4: Simplify admin layout**

Modify `frontend/src/components/AdminLayout.tsx` with the same visual system and add a nav entry:

```tsx
{
  to: "/admin/runtime-pods",
  label: "Runtime Pods",
  icon: Server,
}
```

Do not expose admin-only runtime metrics in `UserLayout`.

- [ ] **Step 5: Run frontend lint**

Run:

```powershell
cd frontend
npm run lint
cd ..
```

Expected: pass.

- [ ] **Step 6: Commit UI foundation**

Run:

```powershell
git add frontend/package.json frontend/package-lock.json frontend/src/index.css frontend/src/App.css frontend/src/components/UserLayout.tsx frontend/src/components/AdminLayout.tsx
git commit -m "style: simplify ClawManager UI shell"
```

## Task 9: Frontend Workspace Manager And V2 User Pages

**Files:**
- Modify: `frontend/src/types/instance.ts`
- Create: `frontend/src/types/workspace.ts`
- Create: `frontend/src/services/workspaceService.ts`
- Create: `frontend/src/components/WorkspaceFileManager.tsx`
- Create: `frontend/src/components/InstanceServiceFrame.tsx`
- Modify: `frontend/src/pages/instances/CreateInstancePage.tsx`
- Modify: `frontend/src/pages/instances/InstanceListPage.tsx`
- Modify: `frontend/src/pages/instances/InstanceDetailPage.tsx`
- Modify: `frontend/src/services/instanceService.ts`

- [ ] **Step 1: Add TypeScript types**

Create `frontend/src/types/workspace.ts`:

```ts
export interface WorkspaceEntry {
  name: string;
  path: string;
  is_dir: boolean;
  size: number;
  modified_at: string;
  previewable: boolean;
  downloadable: boolean;
}

export interface WorkspacePreview {
  kind: "text" | "image" | "pdf" | "download";
  content_type: string;
  text?: string;
  url?: string;
}
```

Modify `frontend/src/types/instance.ts`:

```ts
export type V2InstanceType = "openclaw" | "hermes";
export type InstanceAvailability = "available" | "starting" | "unavailable";

export interface InstanceStatus {
  instance_id: number;
  status: string;
  availability?: InstanceAvailability;
  agent_type?: V2InstanceType;
  workspace_usage_bytes?: number;
  created_at?: string;
  started_at?: string;
}
```

Keep legacy optional Pod fields only if existing pages still need them for admin legacy screens.

- [ ] **Step 2: Add workspace service**

Create `frontend/src/services/workspaceService.ts`:

```ts
import api from "./api";
import type { WorkspaceEntry, WorkspacePreview } from "../types/workspace";

export const workspaceService = {
  async list(instanceId: number, path = ""): Promise<WorkspaceEntry[]> {
    const response = await api.get(`/instances/${instanceId}/workspace/files`, {
      params: { path },
    });
    return response.data.data.entries;
  },

  async preview(instanceId: number, path: string): Promise<WorkspacePreview> {
    const response = await api.get(`/instances/${instanceId}/workspace/preview`, {
      params: { path },
    });
    return response.data.data.preview;
  },

  downloadUrl(instanceId: number, path: string): string {
    return `/api/v1/instances/${instanceId}/workspace/download?path=${encodeURIComponent(path)}`;
  },

  async upload(instanceId: number, path: string, file: File): Promise<void> {
    const formData = new FormData();
    formData.append("file", file);
    await api.post(`/instances/${instanceId}/workspace/upload`, formData, {
      params: { path },
      headers: { "Content-Type": "multipart/form-data" },
    });
  },

  async mkdir(instanceId: number, path: string): Promise<void> {
    await api.post(`/instances/${instanceId}/workspace/folders`, { path });
  },

  async rename(instanceId: number, oldPath: string, newPath: string): Promise<void> {
    await api.patch(`/instances/${instanceId}/workspace/entries`, {
      old_path: oldPath,
      new_path: newPath,
    });
  },

  async remove(instanceId: number, path: string): Promise<void> {
    await api.delete(`/instances/${instanceId}/workspace/entries`, {
      params: { path },
    });
  },
};
```

- [ ] **Step 3: Build independent file manager**

Create `frontend/src/components/WorkspaceFileManager.tsx`. It must:
- Fetch entries with React Query.
- Show breadcrumb path.
- Upload via hidden file input.
- Provide icon buttons for preview, download, rename, delete, and new folder.
- Show text preview in a compact preformatted pane.
- Show image/PDF preview in a modal or right panel.
- Never render an arbitrary path input that can bypass the breadcrumb.

Core state:

```tsx
const [currentPath, setCurrentPath] = useState("");
const [previewPath, setPreviewPath] = useState<string | null>(null);
const entriesQuery = useQuery({
  queryKey: ["workspace", instanceId, currentPath],
  queryFn: () => workspaceService.list(instanceId, currentPath),
});
```

- [ ] **Step 4: Build service frame**

Create `frontend/src/components/InstanceServiceFrame.tsx`:

```tsx
interface InstanceServiceFrameProps {
  instanceId: number;
  availability: "available" | "starting" | "unavailable";
}

export function InstanceServiceFrame({ instanceId, availability }: InstanceServiceFrameProps) {
  if (availability === "starting") {
    return <div className="cm-surface flex min-h-[420px] items-center justify-center text-sm text-gray-600">Starting</div>;
  }
  if (availability === "unavailable") {
    return <div className="cm-surface flex min-h-[420px] items-center justify-center text-sm text-gray-600">Unavailable</div>;
  }
  return (
    <iframe
      title="Instance"
      src={`/api/v1/instances/${instanceId}/proxy/`}
      className="h-[min(720px,calc(100vh-220px))] min-h-[520px] w-full rounded-lg border border-gray-200 bg-white"
    />
  );
}
```

- [ ] **Step 5: Restrict create page to V2 types**

Modify `frontend/src/pages/instances/CreateInstancePage.tsx` so the visible type choices are only:

```ts
const CREATE_INSTANCE_TYPES = INSTANCE_TYPES.filter((type) =>
  ["openclaw", "hermes"].includes(type.id),
);
```

Mark existing non-V2 types as legacy only in admin/history contexts, not in creation.

- [ ] **Step 6: Simplify ordinary user list and detail pages**

Modify `InstanceListPage.tsx` and `InstanceDetailPage.tsx`:
- Remove service resource, recent status, Pod name, Pod IP, namespace, service, gateway port, scheduler details from ordinary user pages.
- Show status as `Available`, `Starting`, or `Unavailable`.
- Show instance type, name, updated time, and workspace usage.
- Put `InstanceServiceFrame` and `WorkspaceFileManager` in tabs or a split layout.
- Keep start, stop, restart, delete actions.

Detail page core:

```tsx
const availability =
  status?.availability ??
  (instance.status === "running" ? "available" : instance.status === "creating" ? "starting" : "unavailable");

return (
  <div className="space-y-4">
    <header className="flex flex-wrap items-center justify-between gap-3">
      <div>
        <h1 className="text-xl font-semibold text-gray-950">{instance.name}</h1>
        <p className="text-sm text-gray-500">{instance.type === "hermes" ? "Hermes" : "OpenClaw"}</p>
      </div>
      <InstanceActionButtons instance={instance} />
    </header>
    <section className="grid gap-4 xl:grid-cols-[minmax(0,1fr)_420px]">
      <InstanceServiceFrame instanceId={instance.id} availability={availability} />
      <WorkspaceFileManager instanceId={instance.id} />
    </section>
  </div>
);
```

- [ ] **Step 7: Run frontend checks**

Run:

```powershell
cd frontend
npm run lint
npm run build
cd ..
```

Expected: both commands pass.

- [ ] **Step 8: Commit user V2 UI**

Run:

```powershell
git add frontend/src/types frontend/src/services frontend/src/components frontend/src/pages/instances
git commit -m "feat: add V2 user workspace UI"
```

## Task 10: Admin Runtime Pods And Rollout UI

**Files:**
- Create: `frontend/src/types/runtimePool.ts`
- Create: `frontend/src/services/runtimePoolService.ts`
- Modify: `frontend/src/hooks/useWebSocket.ts`
- Create: `frontend/src/pages/admin/RuntimePodsPage.tsx`
- Modify: `frontend/src/pages/admin/SystemSettingsPage.tsx`
- Modify: `frontend/src/router/index.tsx`
- Modify: `frontend/src/components/AdminLayout.tsx`

- [ ] **Step 1: Add runtime pool types**

Create `frontend/src/types/runtimePool.ts`:

```ts
export interface RuntimePod {
  id: number;
  runtime_type: "openclaw" | "hermes";
  pod_name: string;
  pod_ip?: string;
  node_name?: string;
  state: "pending" | "ready" | "draining" | "unhealthy" | "deleted";
  used_slots: number;
  capacity: number;
  draining: boolean;
  cpu_millis_used: number;
  memory_bytes_used: number;
  disk_bytes_used: number;
  network_rx_bytes: number;
  network_tx_bytes: number;
  last_seen_at?: string;
}

export interface RuntimeGateway {
  instance_id: number;
  gateway_id: string;
  gateway_port: number;
  state: string;
  workspace_path: string;
  last_health_at?: string;
}

export interface StartRuntimeRolloutRequest {
  runtime_type: "openclaw" | "hermes";
  target_image_ref: string;
  batch_size: number;
  max_unavailable: number;
}
```

- [ ] **Step 2: Add admin service client**

Create `frontend/src/services/runtimePoolService.ts`:

```ts
import api from "./api";
import type { RuntimeGateway, RuntimePod, StartRuntimeRolloutRequest } from "../types/runtimePool";

export const runtimePoolService = {
  async listPods(runtimeType?: "openclaw" | "hermes"): Promise<RuntimePod[]> {
    const response = await api.get("/admin/runtime-pods", {
      params: runtimeType ? { runtime_type: runtimeType } : undefined,
    });
    return response.data.data.pods;
  },

  async listGateways(podId: number): Promise<RuntimeGateway[]> {
    const response = await api.get(`/admin/runtime-pods/${podId}/gateways`);
    return response.data.data.gateways;
  },

  async drainPod(podId: number): Promise<void> {
    await api.post(`/admin/runtime-pods/${podId}/drain`);
  },

  async startRollout(data: StartRuntimeRolloutRequest): Promise<void> {
    await api.post("/admin/runtime-rollouts", data);
  },
};
```

- [ ] **Step 3: Create Runtime Pods page**

Modify `frontend/src/hooks/useWebSocket.ts` first:

```ts
export interface RuntimeAdminMessage {
  type: "runtime_pod_metrics" | "runtime_pod_state" | "runtime_rollout";
  data: unknown;
  timestamp: string;
}

export function useRuntimeAdminWebSocket(
  onRuntimeEvent?: (message: RuntimeAdminMessage) => void,
) {
  const { addMessageHandler, isConnected } = useWebSocket();

  useEffect(() => {
    if (!onRuntimeEvent) return;

    const handler = (message: WebSocketMessage) => {
      if (
        message.type === "runtime_pod_metrics" ||
        message.type === "runtime_pod_state" ||
        message.type === "runtime_rollout"
      ) {
        onRuntimeEvent(message as RuntimeAdminMessage);
      }
    };

    return addMessageHandler(handler);
  }, [onRuntimeEvent, addMessageHandler]);

  return { isConnected };
}
```

Create `frontend/src/pages/admin/RuntimePodsPage.tsx`. It must:
- Show segmented filter: All, OpenClaw, Hermes.
- Show a dense table with Pod, type, state, slots, CPU, memory, disk, network, last seen.
- Refresh with React Query every 3 seconds.
- Subscribe to admin runtime websocket through `useRuntimeAdminWebSocket`; invalidate the `["runtime-pods"]` query when a runtime event arrives.
- Provide a Drain button for each Pod.
- Show a gateway drawer/table for a selected Pod.

Use simple table layout:

```tsx
<table className="min-w-full divide-y divide-gray-200 text-sm">
  <thead className="bg-gray-50 text-left text-xs font-medium uppercase tracking-normal text-gray-500">
    ...
  </thead>
  <tbody className="divide-y divide-gray-100 bg-white">
    ...
  </tbody>
</table>
```

- [ ] **Step 4: Add route**

Modify `frontend/src/router/index.tsx`:

```tsx
{
  path: "runtime-pods",
  element: <RuntimePodsPage />,
}
```

under the admin route group.

- [ ] **Step 5: Add gray rollout controls in system settings**

Modify `frontend/src/pages/admin/SystemSettingsPage.tsx`:
- Add a section named `Runtime Images`.
- Include only OpenClaw and Hermes runtime image fields for V2.
- Add `Start gray rollout` action using `runtimePoolService.startRollout`.
- Inputs: runtime type, target image, batch size default 1, max unavailable default 1.
- Keep existing image settings if needed for legacy visibility, but visually separate them under `Legacy Images`.

- [ ] **Step 6: Run frontend checks**

Run:

```powershell
cd frontend
npm run lint
npm run build
cd ..
```

Expected: pass.

- [ ] **Step 7: Commit admin runtime UI**

Run:

```powershell
git add frontend/src/types/runtimePool.ts frontend/src/services/runtimePoolService.ts frontend/src/hooks/useWebSocket.ts frontend/src/pages/admin/RuntimePodsPage.tsx frontend/src/pages/admin/SystemSettingsPage.tsx frontend/src/router/index.tsx frontend/src/components/AdminLayout.tsx
git commit -m "feat: add admin runtime pod view"
```

## Task 11: One-YAML Deployment Manifests

**Files:**
- Modify: `deployments/k8s/clawmanager.yaml`
- Modify: `deployments/k3s/clawmanager.yaml`

- [ ] **Step 1: Rename Redis service while keeping compatibility**

In both manifests:
- Keep the Redis Deployment selector as a stable label.
- Add Service `clawmanager-redis`.
- Keep Service `clawmanager-team-redis` pointing to the same selector.
- Set backend env:

```yaml
- name: PLATFORM_REDIS_URL
  value: "redis://clawmanager-redis.clawmanager-system.svc.cluster.local:6379/0"
- name: TEAM_REDIS_URL
  value: "redis://clawmanager-redis.clawmanager-system.svc.cluster.local:6379/0"
```

- [ ] **Step 2: Add workspace store**

Add a lightweight workspace store Deployment, PVC, and Service:

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: clawmanager-workspaces
  namespace: clawmanager-system
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 500Gi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: workspace-store
  namespace: clawmanager-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: workspace-store
  template:
    metadata:
      labels:
        app: workspace-store
    spec:
      containers:
        - name: nfs
          image: itsthenetwork/nfs-server-alpine:12
          securityContext:
            privileged: true
          env:
            - name: SHARED_DIRECTORY
              value: /exports/workspaces
          ports:
            - name: nfs
              containerPort: 2049
          volumeMounts:
            - name: workspaces
              mountPath: /exports/workspaces
      volumes:
        - name: workspaces
          persistentVolumeClaim:
            claimName: clawmanager-workspaces
---
apiVersion: v1
kind: Service
metadata:
  name: workspace-store
  namespace: clawmanager-system
spec:
  selector:
    app: workspace-store
  ports:
    - name: nfs
      port: 2049
      targetPort: nfs
```

- [ ] **Step 3: Scale backend replicas and add runtime env**

Set backend Deployment:

```yaml
spec:
  replicas: 3
```

Add env:

```yaml
- name: RUNTIME_NAMESPACE
  value: "clawmanager-system"
- name: RUNTIME_WORKSPACE_ROOT
  value: "/workspaces"
- name: RUNTIME_MAX_GATEWAYS_PER_POD
  value: "100"
- name: RUNTIME_GATEWAY_PORT_START
  value: "20000"
- name: RUNTIME_GATEWAY_PORT_END
  value: "20299"
- name: RUNTIME_SCHEDULER_ENABLED
  value: "true"
- name: RUNTIME_AGENT_CONTROL_TOKEN
  valueFrom:
    secretKeyRef:
      name: clawmanager-secrets
      key: runtime-agent-control-token
- name: RUNTIME_AGENT_REPORT_TOKEN
  valueFrom:
    secretKeyRef:
      name: clawmanager-secrets
      key: runtime-agent-report-token
```

Mount `/workspaces` from the NFS service in backend:

```yaml
volumes:
  - name: workspaces
    nfs:
      server: workspace-store.clawmanager-system.svc.cluster.local
      path: /exports/workspaces
```

- [ ] **Step 4: Add runtime Deployments**

Add `openclaw-runtime` and `hermes-runtime` Deployments, each initial `replicas: 1`, each mounting the same NFS workspace and using the matching image env:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: openclaw-runtime
  namespace: clawmanager-system
spec:
  replicas: 1
  selector:
    matchLabels:
      app: openclaw-runtime
      clawmanager.io/runtime-type: openclaw
  template:
    metadata:
      labels:
        app: openclaw-runtime
        clawmanager.io/runtime-type: openclaw
    spec:
      containers:
        - name: runtime
          image: ghcr.io/yuan-lab-llm/clawmanager-openclaw-image/openclaw:latest
          ports:
            - name: agent
              containerPort: 19090
          env:
            - name: CLAWMANAGER_RUNTIME_TYPE
              value: openclaw
            - name: CLAWMANAGER_GATEWAY_PORT_START
              value: "20000"
            - name: CLAWMANAGER_GATEWAY_PORT_END
              value: "20299"
          volumeMounts:
            - name: workspaces
              mountPath: /workspaces
      volumes:
        - name: workspaces
          nfs:
            server: workspace-store.clawmanager-system.svc.cluster.local
            path: /exports/workspaces
```

Repeat for `hermes-runtime` with `clawmanager.io/runtime-type: hermes`.

- [ ] **Step 5: Extend RBAC**

Add permissions for:

```yaml
- apiGroups: ["coordination.k8s.io"]
  resources: ["leases"]
  verbs: ["get", "create", "update"]
- apiGroups: ["apps"]
  resources: ["deployments", "deployments/scale"]
  verbs: ["get", "list", "watch", "create", "update", "patch"]
- apiGroups: [""]
  resources: ["pods"]
  verbs: ["get", "list", "watch"]
```

- [ ] **Step 6: Validate YAML parse**

Run:

```powershell
kubectl apply --dry-run=client -f deployments/k8s/clawmanager.yaml
kubectl apply --dry-run=client -f deployments/k3s/clawmanager.yaml
```

Expected: both commands return `configured (dry run)` or `created (dry run)` lines. If `kubectl` is unavailable, run:

```powershell
python - <<'PY'
import yaml
for path in ["deployments/k8s/clawmanager.yaml", "deployments/k3s/clawmanager.yaml"]:
    with open(path, "r", encoding="utf-8") as f:
        docs = [d for d in yaml.safe_load_all(f) if d]
    print(path, len(docs))
PY
```

Expected: both files parse and print a document count.

- [ ] **Step 7: Commit manifests**

Run:

```powershell
git add deployments/k8s/clawmanager.yaml deployments/k3s/clawmanager.yaml
git commit -m "feat: install V2 runtime pool components"
```

## Task 12: Full Verification And Cleanup

**Files:**
- Modify: `docs/superpowers/specs/2026-06-01-clawmanager-v2-runtime-pool-design.md` only if implementation behavior differs from the accepted spec.
- Modify: `README.md` or deployment docs if they already contain install steps for the single YAML path.

- [ ] **Step 1: Run full backend tests**

Run:

```powershell
go test ./backend/... -count=1
```

Expected: pass.

- [ ] **Step 2: Run full frontend checks**

Run:

```powershell
cd frontend
npm run lint
npm run build
cd ..
```

Expected: pass.

- [ ] **Step 3: Run repository-wide status check**

Run:

```powershell
git status --short
```

Expected: only intended implementation files are changed since the last task commit.

- [ ] **Step 4: Smoke test local frontend**

Run:

```powershell
cd frontend
npm run dev -- --host 127.0.0.1
```

Open `http://127.0.0.1:9002/` in the in-app browser. Verify:
- User instance pages do not show Pod IP, Pod name, namespace, service, gateway port, scheduler capacity, or runtime resource internals.
- User detail page shows whether the instance can be used, the frame, and the workspace file manager.
- Admin navigation contains Runtime Pods.
- Runtime Pods page is admin-only and shows Pod status, usage, disk, network, and gateway count.
- System Settings has V2 runtime image and gray rollout controls.

- [ ] **Step 5: Commit final docs if changed**

Run:

```powershell
git add README.md docs/superpowers/specs/2026-06-01-clawmanager-v2-runtime-pool-design.md
git commit -m "docs: align V2 runtime pool implementation notes"
```

Skip this commit only when neither file changed.

## Self-Review

Spec coverage:
- Shared runtime Pods with 100 gateways and scale-out at the 101st request: Task 3, Task 4, Task 11.
- Agent-managed gateway processes and HTTP dual channel: Task 2, Task 7.
- Pod IP plus port routing instead of Services: Task 5.
- 5-10 second failover and gray upgrade drain: Task 4, Task 7, Task 10.
- One YAML install with embedded Redis and workspace store: Task 11.
- Backend multi-replica with leader election and Redis fanout: Task 3, Task 4, Task 7, Task 11.
- No VNC; frame directly maps OpenClaw/Hermes services: Task 5, Task 9.
- Workspace file list, preview, upload, download, rename, delete with path isolation: Task 6, Task 9.
- Ordinary user UI hides runtime scheduling details: Task 5, Task 8, Task 9.
- Admin Runtime Pods page with status, usage, disk, network realtime data: Task 7, Task 10.
- Only OpenClaw and Hermes are creatable in V2; legacy remains readable: Task 5, Task 9.

Placeholder scan:
- The plan contains concrete file paths, endpoint paths, payloads, method signatures, test names, commands, and expected outcomes.
- There are no open-ended markers requiring the implementer to invent missing product behavior.

Type consistency:
- Runtime types are consistently `openclaw` and `hermes`.
- Runtime Pod capacity is consistently 100.
- Gateway port range is consistently `20000-20299`.
- Workspace paths are consistently `/workspaces/{runtime}/user-{user_id}/instance-{instance_id}`.
- Runtime user and group IDs are consistently `200000 + instance_id`.
