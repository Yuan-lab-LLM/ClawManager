package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type RuntimePodMetricsUpdate struct {
	CPUMillisUsed   int64
	MemoryBytesUsed int64
	DiskBytesUsed   int64
	NetworkRXBytes  int64
	NetworkTXBytes  int64
	MetricsJSON     *string
	LastSeenAt      *time.Time
}

type RuntimePodRepository interface {
	UpsertFromAgent(ctx context.Context, pod *models.RuntimePod) error
	GetByID(ctx context.Context, id int64) (*models.RuntimePod, error)
	GetByNamespaceName(ctx context.Context, namespace, podName string) (*models.RuntimePod, error)
	List(ctx context.Context, runtimeType string) ([]models.RuntimePod, error)
	ListSchedulable(ctx context.Context, runtimeType string) ([]models.RuntimePod, error)
	TryClaimSlot(ctx context.Context, podID int64) (bool, error)
	ReleaseSlot(ctx context.Context, podID int64) error
	MarkState(ctx context.Context, podID int64, state string, draining bool) error
	UpdateHeartbeat(ctx context.Context, podID int64, state string, usedSlots int, capacity int, draining bool, lastSeenAt time.Time) error
	MarkUnseenUnhealthy(ctx context.Context, cutoff time.Time) error
	UpdateMetrics(ctx context.Context, podID int64, metrics RuntimePodMetricsUpdate) error
}

type runtimePodRepository struct {
	sess db.Session
}

func NewRuntimePodRepository(sess db.Session) RuntimePodRepository {
	return &runtimePodRepository{sess: sess}
}

func (r *runtimePodRepository) UpsertFromAgent(ctx context.Context, pod *models.RuntimePod) error {
	ensureTimestamps(&pod.CreatedAt, &pod.UpdatedAt)
	res, err := r.sess.SQL().ExecContext(ctx, `
		INSERT INTO runtime_pods (
			runtime_type, namespace, pod_name, pod_uid, pod_ip, node_name, deployment_name,
			image_ref, openclaw_version, agent_protocol_version, team_plugin_version, session_store,
			image_digest, capabilities_json, agent_endpoint, state, capacity, used_slots, draining, cpu_millis_used,
			memory_bytes_used, disk_bytes_used, network_rx_bytes, network_tx_bytes, metrics_json,
			last_seen_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			id = LAST_INSERT_ID(id),
			runtime_type = VALUES(runtime_type),
			pod_uid = VALUES(pod_uid),
			pod_ip = VALUES(pod_ip),
			node_name = VALUES(node_name),
			deployment_name = VALUES(deployment_name),
			image_ref = VALUES(image_ref),
			openclaw_version = VALUES(openclaw_version),
			agent_protocol_version = VALUES(agent_protocol_version),
			team_plugin_version = VALUES(team_plugin_version),
			session_store = VALUES(session_store),
			image_digest = VALUES(image_digest),
			capabilities_json = VALUES(capabilities_json),
			agent_endpoint = VALUES(agent_endpoint),
			capacity = VALUES(capacity),
			cpu_millis_used = VALUES(cpu_millis_used),
			memory_bytes_used = VALUES(memory_bytes_used),
			disk_bytes_used = VALUES(disk_bytes_used),
			network_rx_bytes = VALUES(network_rx_bytes),
			network_tx_bytes = VALUES(network_tx_bytes),
			metrics_json = VALUES(metrics_json),
			last_seen_at = VALUES(last_seen_at),
			updated_at = VALUES(updated_at)
	`, pod.RuntimeType, pod.Namespace, pod.PodName, pod.PodUID, pod.PodIP, pod.NodeName, pod.DeploymentName,
		pod.ImageRef, pod.OpenClawVersion, pod.AgentProtocolVersion, pod.TeamPluginVersion, pod.SessionStore,
		pod.ImageDigest, pod.CapabilitiesJSON, pod.AgentEndpoint, pod.State, pod.Capacity, pod.UsedSlots, pod.Draining, pod.CPUMillisUsed,
		pod.MemoryBytesUsed, pod.DiskBytesUsed, pod.NetworkRXBytes, pod.NetworkTXBytes, pod.MetricsJSON,
		pod.LastSeenAt, pod.CreatedAt, pod.UpdatedAt)
	if err != nil {
		return fmt.Errorf("failed to upsert runtime pod: %w", err)
	}
	if id, err := res.LastInsertId(); err == nil {
		pod.ID = id
	}
	return nil
}

func (r *runtimePodRepository) GetByID(ctx context.Context, id int64) (*models.RuntimePod, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var pod models.RuntimePod
	if err := r.sess.Collection("runtime_pods").Find(db.Cond{"id": id}).One(&pod); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get runtime pod: %w", err)
	}
	return &pod, nil
}

func (r *runtimePodRepository) GetByNamespaceName(ctx context.Context, namespace, podName string) (*models.RuntimePod, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var pod models.RuntimePod
	if err := r.sess.Collection("runtime_pods").Find(db.Cond{"namespace": namespace, "pod_name": podName}).One(&pod); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get runtime pod by namespace/name: %w", err)
	}
	return &pod, nil
}

func (r *runtimePodRepository) List(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cond := db.Cond{}
	if runtimeType != "" {
		cond["runtime_type"] = runtimeType
	}
	var pods []models.RuntimePod
	if err := r.sess.Collection("runtime_pods").Find(cond).OrderBy("namespace", "pod_name").All(&pods); err != nil {
		return nil, fmt.Errorf("failed to list runtime pods: %w", err)
	}
	return pods, nil
}

func (r *runtimePodRepository) ListSchedulable(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var pods []models.RuntimePod
	iter := r.sess.SQL().IteratorContext(ctx, `
		SELECT *
		FROM runtime_pods
		WHERE runtime_type = ? AND state = 'ready' AND draining = 0 AND used_slots < capacity
		  AND LOWER(TRIM(deployment_name)) NOT LIKE 'openclaw-upgrade-lab-%'
		ORDER BY used_slots, id
	`, runtimeType)
	defer iter.Close()
	if err := iter.All(&pods); err != nil {
		return nil, fmt.Errorf("failed to list schedulable runtime pods: %w", err)
	}
	return pods, nil
}

func (r *runtimePodRepository) TryClaimSlot(ctx context.Context, podID int64) (bool, error) {
	res, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE runtime_pods
		SET used_slots = used_slots + 1, updated_at = ?
		WHERE id = ? AND state = 'ready' AND draining = 0 AND used_slots < capacity
		AND NOT EXISTS (SELECT 1 FROM runtime_rollouts rr
		  WHERE rr.runtime_type = runtime_pods.runtime_type AND rr.status IN ('pending','running'))
	`, time.Now().UTC(), podID)
	if err != nil {
		return false, fmt.Errorf("failed to claim runtime pod slot: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect runtime pod slot claim: %w", err)
	}
	return affected == 1, nil
}

func (r *runtimePodRepository) ReleaseSlot(ctx context.Context, podID int64) error {
	_, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE runtime_pods
		SET used_slots = CASE WHEN used_slots > 0 THEN used_slots - 1 ELSE 0 END, updated_at = ?
		WHERE id = ?
	`, time.Now().UTC(), podID)
	if err != nil {
		return fmt.Errorf("failed to release runtime pod slot: %w", err)
	}
	return nil
}

func (r *runtimePodRepository) MarkState(ctx context.Context, podID int64, state string, draining bool) error {
	_, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE runtime_pods
		SET state = ?, draining = ?, updated_at = ?
		WHERE id = ?
	`, state, draining, time.Now().UTC(), podID)
	if err != nil {
		return fmt.Errorf("failed to mark runtime pod state: %w", err)
	}
	return nil
}

func (r *runtimePodRepository) UpdateHeartbeat(ctx context.Context, podID int64, state string, usedSlots int, capacity int, draining bool, lastSeenAt time.Time) error {
	_, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE runtime_pods
		SET state = ?, used_slots = ?, capacity = ?, draining = ?, last_seen_at = ?, updated_at = ?
		WHERE id = ?
	`, state, usedSlots, capacity, draining, lastSeenAt, time.Now().UTC(), podID)
	if err != nil {
		return fmt.Errorf("failed to update runtime pod heartbeat: %w", err)
	}
	return nil
}

func (r *runtimePodRepository) MarkUnseenUnhealthy(ctx context.Context, cutoff time.Time) error {
	_, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE runtime_pods
		SET state = 'unhealthy', updated_at = ?
		WHERE (last_seen_at IS NULL OR last_seen_at < ?) AND state <> 'unhealthy'
	`, time.Now().UTC(), cutoff)
	if err != nil {
		return fmt.Errorf("failed to mark unseen runtime pods unhealthy: %w", err)
	}
	return nil
}

func (r *runtimePodRepository) UpdateMetrics(ctx context.Context, podID int64, metrics RuntimePodMetricsUpdate) error {
	_, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE runtime_pods
		SET cpu_millis_used = ?, memory_bytes_used = ?, disk_bytes_used = ?,
			network_rx_bytes = ?, network_tx_bytes = ?, metrics_json = ?,
			last_seen_at = ?, updated_at = ?
		WHERE id = ?
	`, metrics.CPUMillisUsed, metrics.MemoryBytesUsed, metrics.DiskBytesUsed,
		metrics.NetworkRXBytes, metrics.NetworkTXBytes, metrics.MetricsJSON,
		metrics.LastSeenAt, time.Now().UTC(), podID)
	if err != nil {
		return fmt.Errorf("failed to update runtime pod metrics: %w", err)
	}
	return nil
}
