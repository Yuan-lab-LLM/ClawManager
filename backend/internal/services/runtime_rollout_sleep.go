package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/upper/db/v4"
	"time"
)

const rolloutWakeKind = "rollout_wake"

// A failed publication/confirmation response cannot prove the SQL commit was absent.
var ErrRuntimeUpgradeCommitUncertain = errors.New("Runtime upgrade submission outcome is uncertain; inspect operation history")

type rolloutSleepPool struct {
	Target        k8s.RuntimePodScaleTarget    `json:"target"`
	Snapshot      *k8s.RuntimePodScaleSnapshot `json:"snapshot"`
	WakeRequested bool                         `json:"wake_requested"`
}
type rolloutWakeJournal struct {
	RuntimeType    string                     `json:"runtime_type"`
	Phase          string                     `json:"phase"`
	ErrorMessage   string                     `json:"error_message,omitempty"`
	Pools          []rolloutSleepPool         `json:"pools"`
	Deadline       time.Time                  `json:"deadline"`
	Scope          []k8s.RuntimeRolloutTarget `json:"scope"`
	ReadyUntilUnix int64                      `json:"ready_until_unix"`
	ConsumedBy     int64                      `json:"consumed_by"`
}

// Preparation deliberately precedes OpenClaw's read-only compatibility preflight:
// it restores the current validated configuration and never starts a user Gateway.
func (s *RuntimeManagementService) PrepareRollout(ctx context.Context, runtimeType string, actor *int) (*RuntimeManagementOperation, error) {
	rt, ok := NormalizeV2RuntimeType(runtimeType)
	if !ok {
		return nil, errors.New("unsupported Runtime type")
	}
	if rt == RuntimeTypeOpenCode {
		return nil, models.ErrOpenCodeSafeUpgradeRequired
	}
	if s == nil || s.sess == nil || s.scheduler == nil {
		return nil, errors.New("Runtime upgrade preparation unavailable")
	}
	inspector, ok := s.scheduler.deployments.(k8s.RuntimeRolloutSleepService)
	if !ok {
		return nil, errors.New("sleep-aware Runtime upgrade preparation unavailable")
	}
	idle, ok := s.scheduler.deployments.(k8s.RuntimeIdlePoolDeploymentService)
	if !ok {
		return nil, errors.New("Runtime wake unavailable")
	}

	now := time.Now().UTC()
	j := rolloutWakeJournal{RuntimeType: rt, Phase: "pending", Pools: []rolloutSleepPool{}, Scope: []k8s.RuntimeRolloutTarget{}, Deadline: now.Add(15 * time.Minute)}
	op := &RuntimeManagementOperation{ID: "runtime_mgmt_" + uuid.NewString(), Kind: rolloutWakeKind, RuntimeType: rt, Status: "pending", Phase: j.Phase, Items: []RuntimeSchedulingItem{}, CreatedAt: now, UpdatedAt: now}
	publicationAttempted := false
	err := s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		if err := s.scaleInAdmission(ctx, tx, rt); err != nil {
			return err
		}
		targets, err := inspector.SleepingRuntimePools(ctx, s.scheduler.runtimeNamespace, rt)
		if err != nil {
			return err
		}

		for _, target := range targets {
			ops, err := idle.IdleRuntimePoolOperations(ctx, target.Namespace, rt)
			if err != nil {
				return err
			}
			var match *runtimePodScaleInJournal
			for _, id := range ops {
				var raw string
				row, e := tx.SQL().QueryRowContext(ctx, "SELECT request_json FROM runtime_management_operations WHERE id=? AND kind='scale_in' AND status='completed'", id)
				if e == nil {
					e = row.Scan(&raw)
				}
				if e == sql.ErrNoRows {
					continue
				}
				if e != nil {
					return e
				}
				var proof runtimePodScaleInJournal
				if e = json.Unmarshal([]byte(raw), &proof); e != nil {
					return e
				}
				if proof.AutomaticIdle && proof.Phase == "finished" && proof.Plan.Replicas == 0 && proof.Snapshot != nil && proof.Target.Namespace == target.Namespace && proof.Target.DeploymentName == target.DeploymentName && proof.RuntimeType == rt && proof.Snapshot.IdleSleepOperation == id {
					if current, e := inspector.CaptureRuntimeRolloutSleep(ctx, target, proof.Snapshot); e == nil && current.Option.Replicas == 0 {
						proof.Snapshot = current
						match = &proof
						break
					}
				}
			}
			if match == nil {
				return fmt.Errorf("sleeping Runtime %s/%s lacks unchanged completed sleep proof; inspect the pool before upgrading", target.Namespace, target.DeploymentName)
			}
			j.Pools = append(j.Pools, rolloutSleepPool{Target: target, Snapshot: match.Snapshot})
		}

		inventory, ok := s.scheduler.deployments.(k8s.RuntimeRolloutInventory)
		if !ok {
			return errors.New("Runtime upgrade inventory unavailable")
		}
		live, e := inventory.RolloutTargets(ctx, s.scheduler.runtimeNamespace, rt)
		if e != nil {
			return e
		}
		j.Scope = append(j.Scope, live...)
		for _, p := range j.Pools {
			j.Scope = append(j.Scope, k8s.RuntimeRolloutTarget{Namespace: p.Target.Namespace, Name: p.Target.DeploymentName, UID: p.Snapshot.Option.DeploymentUID, Image: p.Snapshot.Option.ImageRef})
		}
		if len(j.Scope) == 0 {
			return errors.New("no eligible live Runtime or verified automatic sleeping pool")
		}

		for _, p := range j.Scope {
			if err := repository.CheckRuntimePodDeletionIntent(ctx, tx, p.Namespace, p.Name); err != nil {
				return err
			}
		}
		raw, err := json.Marshal(j)
		if err != nil {
			return err
		}
		publicationAttempted = true
		_, err = tx.SQL().ExecContext(ctx, "INSERT INTO runtime_management_operations(id,kind,status,actor_user_id,request_json,items_json,created_at,updated_at) VALUES(?,?,?,?,?,'[]',?,?)", op.ID, op.Kind, op.Status, actor, string(raw), now, now)
		return err
	}, nil)
	if err != nil && publicationAttempted {
		return op, fmt.Errorf("%w: preparation %s: %v", ErrRuntimeUpgradeCommitUncertain, op.ID, err)
	}
	return op, err
}

func (s *RuntimeManagementService) saveRolloutWake(ctx context.Context, op *RuntimeManagementOperation, j *rolloutWakeJournal) error {
	raw, err := json.Marshal(j)
	if err != nil {
		return err
	}
	result, err := s.sess.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET status=?,request_json=?,updated_at=? WHERE id=? AND kind=? AND owner_token=? AND status IN ('processing','starting','needs_attention')", op.Status, string(raw), time.Now().UTC(), op.ID, rolloutWakeKind, s.token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("Runtime upgrade preparation ownership lost")
	}
	return err
}

// Every native write holds the same admission fence as scale-in/rollout submit.
// The sole exemption is this exact leader-owned operation; no other held work is ignored.
func (s *RuntimeManagementService) withRolloutWakeAdmission(ctx context.Context, op *RuntimeManagementOperation, fn func(db.Session) error) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		var n int
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM runtime_management_operations WHERE id=? AND kind=? AND owner_token=? AND status IN ('processing','starting') AND cancel_requested=0", op.ID, rolloutWakeKind, s.token)
		if err == nil {
			err = row.Scan(&n)
		}
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("Runtime upgrade preparation cancelled or ownership changed")
		}
		if err := s.scaleInAdmissionExcept(ctx, tx, op.ID, op.RuntimeType); err != nil {
			return err
		}
		if s.scheduler.leader != nil && !s.scheduler.leader.IsLeader(ctx) {
			return errors.New("Runtime upgrade preparation leadership lost")
		}
		return fn(tx)
	}, nil)
}

func (s *RuntimeManagementService) advanceRolloutWake(ctx context.Context, op *RuntimeManagementOperation, raw string) error {
	var j rolloutWakeJournal
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		return err
	}
	op.RuntimeType = j.RuntimeType
	finish := func(status, phase, message string) error {
		op.Status = status
		j.Phase = phase
		j.ErrorMessage = message
		return s.saveRolloutWake(ctx, op, &j)
	}
	cancelled, err := s.cancelRequested(ctx, op.ID)
	if err != nil {
		return err
	}
	if cancelled {
		return finish("cancelled", "cancelled", "")
	}
	if j.Deadline.IsZero() || time.Now().After(j.Deadline) {
		return finish("failed", "timeout", "Runtime upgrade preparation timed out; no image or user instance was changed")
	}
	inspector, ok := s.scheduler.deployments.(k8s.RuntimeRolloutSleepService)
	if !ok {
		return finish("failed", "blocked", "sleep-aware Runtime inspection unavailable")
	}
	wake, ok := s.scheduler.deployments.(k8s.RuntimeIdlePoolDeploymentService)
	if !ok {
		return finish("failed", "blocked", "Runtime wake unavailable")
	}
	for index := range j.Pools {
		p := &j.Pools[index]
		active, err := inspector.ValidateRuntimeRolloutSleep(ctx, p.Target, p.Snapshot)
		if err != nil {
			return finish("needs_attention", "blocked", err.Error())
		}
		if !active {
			op.Status = "starting"
			j.Phase = "waking"
			p.WakeRequested = true
			if err = s.saveRolloutWake(ctx, op, &j); err != nil {
				return err
			}
			err = s.withRolloutWakeAdmission(ctx, op, func(tx db.Session) error {
				if err := repository.CheckRuntimePodDeletionIntent(ctx, tx, p.Target.Namespace, p.Target.DeploymentName); err != nil {
					return err
				}
				active, err := inspector.ValidateRuntimeRolloutSleep(ctx, p.Target, p.Snapshot)
				if err != nil {
					return err
				}
				if active {
					return nil
				}
				return wake.WakeIdleRuntimePool(ctx, p.Target, p.Snapshot)
			})
			if err != nil {
				j.ErrorMessage = err.Error()
				return s.saveRolloutWake(ctx, op, &j)
			}
			// Native readiness and authenticated registration are observed on a later tick.
			return nil
		}
	}
	reported, err := s.scheduler.podRepo.List(ctx, j.RuntimeType)
	if err != nil {
		return err
	}
	ready, err := s.scheduler.verifiedOrdinaryRuntimePods(ctx, j.RuntimeType, reported)
	if err != nil {
		return err
	}

	for _, p := range j.Pools {
		found := false
		for _, pod := range ready {
			if pod.Namespace == p.Target.Namespace && pod.DeploymentName == p.Target.DeploymentName && runtimePodMatchesImage(pod, p.Snapshot.Option.ImageRef) {
				found = true
				break
			}
		}
		if !found {
			op.Status = "starting"
			j.Phase = "waiting_registration"
			return s.saveRolloutWake(ctx, op, &j)
		}
	}
	j.ReadyUntilUnix = time.Now().Add(15 * time.Minute).Unix()
	return finish("completed", "ready", "")
}

func (s *RuntimeManagementService) refreshRolloutWakes(ctx context.Context) error {
	rows, err := s.sess.SQL().QueryContext(ctx, "SELECT id,request_json FROM runtime_management_operations WHERE kind=? AND status='starting' ORDER BY created_at", rolloutWakeKind)
	if err != nil {
		return err
	}
	type entry struct{ id, raw string }
	entries := []entry{}
	for rows.Next() {
		var e entry
		if err = rows.Scan(&e.id, &e.raw); err != nil {
			rows.Close()
			return err
		}
		entries = append(entries, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, e := range entries {
		result, err := s.sess.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET owner_token=?,updated_at=? WHERE id=? AND status='starting' AND request_json=?", s.token, time.Now().UTC(), e.id, e.raw)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			continue
		}
		if err = s.advanceRolloutWake(ctx, &RuntimeManagementOperation{ID: e.id, Kind: rolloutWakeKind, Status: "starting"}, e.raw); err != nil {
			return err
		}
	}
	return nil
}

// Cancellation stops preparation without undoing capacity already proven and
// awakened. The same lock serializes cancellation against an in-flight wake CAS.
func (s *RuntimeManagementService) cancelRolloutWake(ctx context.Context, id string) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		var raw, status string
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT request_json,status FROM runtime_management_operations WHERE id=? AND kind=? FOR UPDATE", id, rolloutWakeKind)
		if err == nil {
			err = row.Scan(&raw, &status)
		}
		if err != nil {
			return err
		}
		if status == "needs_attention" {
			return errors.New("Runtime preparation requires inspection; protected operations cannot be cancelled")
		}
		if status != "pending" && status != "processing" && status != "starting" && status != "completed" {
			return errors.New("operation is already terminal")
		}
		var j rolloutWakeJournal
		if err = json.Unmarshal([]byte(raw), &j); err != nil {
			return err
		}
		if status == "completed" && (j.Phase != "ready" || j.ConsumedBy != 0) {
			return errors.New("preparation was already consumed")
		}
		j.Phase = "cancelled"
		rawJSON, err := json.Marshal(j)
		if err != nil {
			return err
		}
		_, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET status='cancelled',cancel_requested=1,request_json=?,updated_at=? WHERE id=?", string(rawJSON), time.Now().UTC(), id)
		return err
	}, nil)
}

// Called before preflight and again before admission. The SQL ready reservation
// keeps CM's own scale-in writers out until the rollout transaction consumes it.
func (s *RuntimeScheduler) ValidateRolloutPreparation(ctx context.Context, id, runtimeType string) error {
	inspector, ok := s.deployments.(k8s.RuntimeRolloutSleepService)
	if !ok {
		if id != "" {
			return errors.New("Runtime preparation validation unavailable")
		}
		return nil
	}
	if id == "" {
		pools, err := inspector.SleepingRuntimePools(ctx, s.runtimeNamespace, runtimeType)
		if err != nil {
			return err
		}
		if len(pools) > 0 {
			return errors.New("sleeping Runtime pools require upgrade preparation before preflight or rollout")
		}
		return nil
	}
	if s.management == nil {
		return errors.New("Runtime preparation service unavailable")
	}
	op, raw, _, err := s.management.runtimeManagementOperation(ctx, id)
	if err != nil {
		return err
	}
	var j rolloutWakeJournal
	if err = json.Unmarshal([]byte(raw), &j); err != nil {
		return err
	}
	if op.Kind != rolloutWakeKind || op.Status != "completed" || j.RuntimeType != runtimeType || j.Phase != "ready" || j.ReadyUntilUnix <= time.Now().Unix() || j.ConsumedBy != 0 || len(j.Scope) == 0 {
		return errors.New("Runtime preparation is expired, consumed or mismatched; prepare and preflight again")
	}
	for _, p := range j.Scope {
		if err := repository.CheckRuntimePodDeletionIntent(ctx, s.management.sess, p.Namespace, p.Name); err != nil {
			return err
		}
	}
	for _, p := range j.Pools {
		active, err := inspector.ValidateRuntimeRolloutSleep(ctx, p.Target, p.Snapshot)
		if err != nil {
			return err
		}
		if !active {
			return errors.New("prepared Runtime went back to sleep; prepare and preflight again")
		}
	}
	inventory, ok := s.deployments.(k8s.RuntimeRolloutInventory)
	if !ok {
		return errors.New("Runtime inventory unavailable")
	}
	actual, err := inventory.RolloutTargets(ctx, s.runtimeNamespace, runtimeType)
	if err != nil {
		return err
	}
	if len(actual) != len(j.Scope) {
		return errors.New("Runtime preparation pool scope changed; prepare and preflight again")
	}
	expected := map[string]k8s.RuntimeRolloutTarget{}
	for _, p := range j.Scope {
		expected[p.Namespace+"/"+p.Name] = p
	}
	for _, p := range actual {
		e, ok := expected[p.Namespace+"/"+p.Name]
		if !ok || e.UID != p.UID || e.Image != p.Image {
			return errors.New("Runtime preparation pool identity or image changed; prepare and preflight again")
		}
	}
	return nil
}
