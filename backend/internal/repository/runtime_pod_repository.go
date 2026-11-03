package repository

import (
	"context"
	"fmt"
	"sync"
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
	sess                 db.Session
	registrationMu       sync.RWMutex
	registrationVerifier func(context.Context, *models.RuntimePod) error
}

// Optional setter keeps legacy repository fakes compatible. Native servers set
// this before their scheduler and reporting routes start.
func (r *runtimePodRepository) SetRuntimePodRegistrationVerifier(verify func(context.Context, *models.RuntimePod) error) {
	r.registrationMu.Lock()
	r.registrationVerifier = verify
	r.registrationMu.Unlock()
}

func NewRuntimePodRepository(sess db.Session) RuntimePodRepository {
	return &runtimePodRepository{sess: sess}
}

func (r *runtimePodRepository) UpsertFromAgent(ctx context.Context, pod *models.RuntimePod) error {
	r.registrationMu.RLock()
	verify := r.registrationVerifier
	r.registrationMu.RUnlock()
	if verify != nil {
		if err := verify(ctx, pod); err != nil {
			return err
		}
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if pod.PodUID != nil && *pod.PodUID != "" {
			rows, err := tx.SQL().QueryContext(ctx, "SELECT namespace,pod_name,deployment_name,runtime_type FROM runtime_pods WHERE pod_uid=?", *pod.PodUID)
			if err != nil {
				return err
			}
			type priorIdentity struct{ namespace, name, owner, runtime string }
			var existing []priorIdentity
			for rows.Next() {
				var prior priorIdentity
				if err = rows.Scan(&prior.namespace, &prior.name, &prior.owner, &prior.runtime); err != nil {
					rows.Close()
					return err
				}
				existing = append(existing, prior)
			}
			err = rows.Err()
			rows.Close()
			if err != nil {
				return err
			}
			for _, prior := range existing {
				if prior.namespace != pod.Namespace || prior.name != pod.PodName {
					return fmt.Errorf("Pod UID already belongs to a different namespace/name")
				}
				if prior.owner != pod.DeploymentName || prior.runtime != pod.RuntimeType {
					if err := CheckRuntimePodDeletionIntent(ctx, tx, prior.namespace, prior.owner); err != nil {
						return fmt.Errorf("fenced Runtime registration cannot change controller identity: %w", err)
					}
				}
			}
		}
		ensureTimestamps(&pod.CreatedAt, &pod.UpdatedAt)
		res, err := tx.SQL().ExecContext(ctx, `
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
		fenced, err := RuntimePodDeletionIntentUID(ctx, tx, pod.Namespace, pod.DeploymentName)
		if err != nil {
			return err
		}
		if fenced != "" {
			if _, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET scheduling_paused=1 WHERE id=?", pod.ID); err != nil {
				return err
			}
			pod.SchedulingPaused = true
		}
		return nil
	}, nil)
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
		WHERE runtime_type = ? AND state = 'ready' AND draining = 0 AND scheduling_paused = 0 AND used_slots < capacity
		  AND LOWER(TRIM(deployment_name)) NOT LIKE 'openclaw-upgrade-lab-%'
 AND NOT EXISTS (SELECT 1 FROM runtime_pod_deletion_intents di WHERE di.namespace=runtime_pods.namespace AND di.deployment_name=runtime_pods.deployment_name)
		ORDER BY used_slots, id
	`, runtimeType)
	defer iter.Close()
	if err := iter.All(&pods); err != nil {
		return nil, fmt.Errorf("failed to list schedulable runtime pods: %w", err)
	}
	return pods, nil
}

func (r *runtimePodRepository) TryClaimSlot(ctx context.Context, podID int64) (bool, error) {
	return r.tryClaimSlot(ctx, podID, nil)
}

// The scheduler supplies its configured freshness window. Recheck atomically
// with capacity acquisition, rather than trusting an earlier SELECT snapshot.
func (r *runtimePodRepository) TryClaimSlotSince(ctx context.Context, podID int64, cutoff time.Time) (bool, error) {
	return r.tryClaimSlot(ctx, podID, &cutoff)
}

func (r *runtimePodRepository) tryClaimSlot(ctx context.Context, podID int64, cutoff *time.Time) (bool, error) {
	var claimed bool
	err := r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := CheckRuntimePodScaleInAdmission(ctx, tx, podID); err != nil {
			return err
		}
		query := `
 UPDATE runtime_pods SET used_slots=used_slots+1,updated_at=?
 WHERE id=? AND state='ready' AND draining=0 AND scheduling_paused=0 AND used_slots<capacity
 AND NOT EXISTS (SELECT 1 FROM runtime_pod_deletion_intents di WHERE di.namespace=runtime_pods.namespace AND di.deployment_name=runtime_pods.deployment_name)
 AND NOT EXISTS (SELECT 1 FROM runtime_rollouts rr WHERE rr.runtime_type=runtime_pods.runtime_type AND rr.status IN ('pending','running'))`
		args := []any{time.Now().UTC(), podID}
		if cutoff != nil {
			query += ` AND last_seen_at IS NOT NULL AND last_seen_at>=?`
			args = append(args, *cutoff)
		}
		res, err := tx.SQL().ExecContext(ctx, query, args...)
		if err != nil {
			return fmt.Errorf("failed to claim runtime pod slot: %w", err)
		}
		affected, err := res.RowsAffected()
		claimed = affected == 1
		return err
	}, nil)
	return claimed, err
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

// SetSchedulingPaused is control-plane owned; Agent heartbeats cannot clear it.
func (r *runtimePodRepository) SetSchedulingPaused(ctx context.Context, podID int64, paused bool) error {
	if paused {
		_, err := r.sess.SQL().ExecContext(ctx, "UPDATE runtime_pods SET scheduling_paused=1,updated_at=? WHERE id=?", time.Now().UTC(), podID)
		return err
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		if err := LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := CheckRuntimePodScaleInAdmission(ctx, tx, podID); err != nil {
			return err
		}
		var namespace, name string
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT namespace,deployment_name FROM runtime_pods WHERE id=?", podID)
		if err == nil {
			err = row.Scan(&namespace, &name)
		}
		if err != nil {
			return err
		}
		if err = CheckRuntimePodDeletionIntent(ctx, tx, namespace, name); err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET scheduling_paused=0,updated_at=? WHERE id=?", time.Now().UTC(), podID)
		return err
	}, nil)
}
