package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/upper/db/v4"
)

func (s *RuntimeManagementService) saveScaleIn(ctx context.Context, op *RuntimeManagementOperation, journal *runtimePodScaleInJournal) error {
	op.UpdatedAt = time.Now().UTC()
	op.ErrorMessage = journal.ErrorMessage
	op.Phase = journal.Phase
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	items, err := json.Marshal(op.Items)
	if err != nil {
		return err
	}
	result, err := s.sess.SQL().ExecContext(ctx, `UPDATE runtime_management_operations SET status=?,request_json=?,items_json=?,updated_at=? WHERE id=? AND status IN ('starting','needs_attention','processing') AND owner_token=?`, op.Status, string(raw), string(items), op.UpdatedAt, op.ID, s.token)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errors.New("scale-in execution ownership lost")
	}
	return nil
}
func (s *RuntimeManagementService) finishScaleIn(ctx context.Context, op *RuntimeManagementOperation, journal *runtimePodScaleInJournal, status string, resumeSources bool) error {
	raw, err := json.Marshal(journal)
	if err != nil {
		return err
	}
	items, err := json.Marshal(op.Items)
	if err != nil {
		return err
	}
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		// Release the durable source fence atomically with scheduling restoration.
		// Success leaves removed identities paused; registration cannot re-use a UID.
		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		result, err := tx.SQL().ExecContext(ctx, `UPDATE runtime_management_operations SET status=?,request_json=?,items_json=?,updated_at=? WHERE id=? AND status IN ('starting','needs_attention','processing') AND owner_token=?`, status, string(raw), string(items), time.Now().UTC(), op.ID, s.token)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errors.New("scale-in execution ownership lost")
		}
		if resumeSources {
			for _, pod := range journal.Plan.SourcePods {
				if _, err = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET scheduling_paused=0,updated_at=? WHERE id=? AND pod_uid=?", time.Now().UTC(), pod.ID, pod.PodUID); err != nil {
					return err
				}
			}
		}
		_, err = tx.SQL().ExecContext(ctx, "DELETE FROM instance_lifecycle_manual_guards WHERE token=?", op.ID)
		if err == nil {
			op.Status = status
			op.ErrorMessage = journal.ErrorMessage
			op.Phase = journal.Phase
		}
		return err
	}, nil)
}

func (s *RuntimeManagementService) failScaleInBeforeResize(ctx context.Context, op *RuntimeManagementOperation, journal *runtimePodScaleInJournal, cause error) error {
	journal.ErrorMessage = cause.Error()
	if journal.Phase != "pending" {
		kube, err := s.scaleInKube()
		if err == nil {
			err = s.withScaleInKubeMutation(ctx, op, journal, func() error { return kube.ReleaseRuntimeScaleIn(ctx, journal.Protection, true) })
		}
		if err != nil {
			op.Status = "needs_attention"
			journal.ErrorMessage = fmt.Sprintf("%v; protection retained: %v", cause, err)
			return s.saveScaleIn(ctx, op, journal)
		}
	}
	status := "failed"
	cancelled, err := s.cancelRequested(ctx, op.ID)
	if err != nil {
		return err
	}
	if cancelled {
		status = "cancelled"
	}
	journal.Phase = "finished"
	return s.finishScaleIn(ctx, op, journal, status, true)
}
func (s *RuntimeManagementService) processScaleIn(ctx context.Context, op *RuntimeManagementOperation, raw string) error {
	var journal runtimePodScaleInJournal
	if err := json.Unmarshal([]byte(raw), &journal); err != nil {
		return err
	}
	kube, err := s.scaleInKube()
	if err != nil {
		return s.failScaleInBeforeResize(ctx, op, &journal, err)
	}
	cancelled, err := s.cancelRequested(ctx, op.ID)
	if err != nil {
		return err
	}
	if cancelled {
		return s.failScaleInBeforeResize(ctx, op, &journal, errors.New("scale-in cancelled before migrations"))
	}
	if err = s.assertOwner(ctx, op.ID); err != nil {
		return err
	}
	snapshot, e := kube.InspectRuntimeScale(ctx, journal.Target)
	if e != nil {
		return s.failScaleInBeforeResize(ctx, op, &journal, e)
	}
	if journal.Snapshot == nil || snapshot.Option.DeploymentUID != journal.Snapshot.Option.DeploymentUID || snapshot.Option.Replicas != journal.Snapshot.Option.Replicas || !reflect.DeepEqual(snapshot.Template, journal.Snapshot.Template) {
		return s.failScaleInBeforeResize(ctx, op, &journal, errors.New("confirmed default Deployment template or replicas changed before worker execution"))
	}
	journal.Phase = "acquiring_protection"
	if err = s.saveScaleIn(ctx, op, &journal); err != nil {
		return err
	}
	if err = s.withScaleInKubeMutation(ctx, op, &journal, func() error { return kube.ProtectRuntimeScaleIn(ctx, journal.Protection) }); err != nil {
		return s.failScaleInBeforeResize(ctx, op, &journal, err)
	}
	journal.Phase = "migrating"
	if err = s.saveScaleIn(ctx, op, &journal); err != nil {
		return err
	}
	for i := range op.Items {
		cancelled, err = s.cancelRequested(ctx, op.ID)
		if err != nil {
			return err
		}
		if cancelled {
			for j := i; j < len(op.Items); j++ {
				op.Items[j].State = "cancelled"
			}
			return s.failScaleInBeforeResize(ctx, op, &journal, errors.New("scale-in cancelled; replica count preserved"))
		}
		op.Items[i].State = "processing"
		if err = s.saveScaleIn(ctx, op, &journal); err != nil {
			return err
		}
		item := op.Items[i]
		item.StopGraceSeconds = 30
		moveCtx, cancel := context.WithTimeout(ctx, 30*time.Second+runtimeAgentControlTimeout)
		run := func() error { return s.move(moveCtx, item, op.ID) }
		if s.scheduler.upgrade != nil {
			err = s.scheduler.upgrade.WithInstanceMutation(moveCtx, item.InstanceID, run)
		} else {
			err = run()
		}
		cancel()
		if err != nil && !isRuntimeAssignmentPending(err) {
			op.Items[i].State = "failed"
			op.Items[i].ErrorMessage = err.Error()
			for j := i + 1; j < len(op.Items); j++ {
				op.Items[j].State = "cancelled"
			}
			return s.failScaleInBeforeResize(ctx, op, &journal, fmt.Errorf("instance %d migration failed; replica count preserved: %w", item.InstanceID, err))
		}
		op.Items[i].State = "starting"
		if err != nil {
			op.Items[i].ErrorMessage = err.Error()
		}
		if err = s.saveScaleIn(ctx, op, &journal); err != nil {
			return err
		}
	}
	op.Status = "starting"
	if err = s.saveScaleIn(ctx, op, &journal); err != nil {
		return err
	}
	if len(op.Items) == 0 {
		return s.advanceScaleIn(ctx, op, &journal)
	}
	return nil
}

func (s *RuntimeManagementService) refreshScaleIn(ctx context.Context) error {
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT id,status,request_json,items_json,cancel_requested FROM runtime_management_operations WHERE kind='scale_in' AND status IN ('starting','needs_attention') ORDER BY created_at`)
	if err != nil {
		return err
	}
	type entry struct {
		op      RuntimeManagementOperation
		journal runtimePodScaleInJournal
	}
	entries := []entry{}
	for rows.Next() {
		var e entry
		var raw, items string
		if err = rows.Scan(&e.op.ID, &e.op.Status, &raw, &items, &e.op.CancelRequested); err != nil {
			rows.Close()
			return err
		}
		e.op.Kind = "scale_in"
		if err = json.Unmarshal([]byte(raw), &e.journal); err != nil {
			rows.Close()
			return err
		}
		if err = json.Unmarshal([]byte(items), &e.op.Items); err != nil {
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
		raw, marshalErr := json.Marshal(&e.journal)
		if marshalErr != nil {
			return marshalErr
		}
		result, claimErr := s.sess.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET owner_token=?,updated_at=UTC_TIMESTAMP(6) WHERE id=? AND status=? AND request_json=?", s.token, e.op.ID, e.op.Status, string(raw))
		if claimErr != nil {
			return claimErr
		}
		claimed, claimErr := result.RowsAffected()
		if claimErr != nil {
			return claimErr
		}
		if claimed != 1 {
			continue
		}
		if e.op.Status == "needs_attention" && e.journal.Phase != "resizing" && e.journal.Phase != "observing" && e.journal.Phase != "cleanup" {
			// No interrupted move is replayed. Restore capacity admission only after
			// proving the replica write never happened and safely releasing protection.
			if err = s.failScaleInBeforeResize(ctx, &e.op, &e.journal, errors.New("scale-in worker interrupted; instance moves require inspection")); err != nil {
				return err
			}
			continue
		}
		if err = s.advanceScaleIn(ctx, &e.op, &e.journal); err != nil {
			return err
		}
	}
	return nil
}
func (s *RuntimeManagementService) advanceScaleIn(ctx context.Context, op *RuntimeManagementOperation, journal *runtimePodScaleInJournal) error {
	kube, err := s.scaleInKube()
	if err != nil {
		return err
	}
	if journal.Phase == "resizing" || journal.Phase == "observing" || journal.Phase == "cleanup" {
		if journal.Phase != "cleanup" {
			ready, e := kube.ObserveRuntimeScaleIn(ctx, journal.Protection)
			if e != nil {
				// If a lost write was not accepted, cleanup proves the old count/set.
				// An accepted or uncertain resize can never take this rollback path.
				if journal.Phase == "resizing" {
					return s.failScaleInBeforeResize(ctx, op, journal, e)
				}
				op.Status = "needs_attention"
				journal.ErrorMessage = e.Error()
				return s.saveScaleIn(ctx, op, journal)
			}
			if !ready {
				return nil
			}
			journal.Phase = "cleanup"
			op.Status = "starting"
			if err = s.saveScaleIn(ctx, op, journal); err != nil {
				return err
			}
		}
		if err = s.withScaleInKubeMutation(ctx, op, journal, func() error { return kube.ReleaseRuntimeScaleIn(ctx, journal.Protection, false) }); err != nil {
			op.Status = "needs_attention"
			journal.ErrorMessage = err.Error()
			return s.saveScaleIn(ctx, op, journal)
		}
		journal.Phase = "finished"
		journal.ErrorMessage = ""
		return s.finishScaleIn(ctx, op, journal, "completed", false)
	}
	cancelled, err := s.cancelRequested(ctx, op.ID)
	if err != nil {
		return err
	}
	if cancelled {
		return s.failScaleInBeforeResize(ctx, op, journal, errors.New("scale-in cancelled; replica count preserved"))
	}
	inventory, err := s.scheduler.RuntimeDeploymentPods(ctx, journal.RuntimeType)
	if err != nil {
		return err
	}
	liveTargets := map[int64]models.RuntimePod{}
	for _, p := range inventory {
		liveTargets[p.ID] = p
	}
	for i := range op.Items {
		item := &op.Items[i]
		if item.State != "starting" && item.State != "completed" {
			return s.failScaleInBeforeResize(ctx, op, journal, errors.New("scale-in contains an unsuccessful migration"))
		}
		binding, e := s.scheduler.bindingRepo.GetByInstanceID(ctx, item.InstanceID)
		if e != nil {
			return e
		}
		target, e := s.scheduler.podRepo.GetByID(ctx, item.TargetPodID)
		if e != nil {
			return e
		}
		if binding == nil {
			instance, e := s.scheduler.instanceRepo.GetByID(item.InstanceID)
			if e != nil {
				return e
			}
			if instance == nil || instance.RuntimeGeneration != item.SourceGeneration+1 || instance.Status == "stopped" || instance.Status == "deleting" || instance.Status == "deleted" {
				item.State = "failed"
				return s.failScaleInBeforeResize(ctx, op, journal, errors.New("instance intent changed during scale-in migration"))
			}
			item.State = "starting"
			continue
		}
		if target == nil || stringValue(target.PodUID) != item.TargetPodUID || binding.RuntimePodID != item.TargetPodID || binding.Generation != item.SourceGeneration+1 {
			item.State = "failed"
			return s.failScaleInBeforeResize(ctx, op, journal, errors.New("migration destination identity changed"))
		}
		live, ok := liveTargets[item.TargetPodID]
		if !ok || stringValue(live.PodUID) != item.TargetPodUID || runtimeScaleInFreshPod(live, s.scheduler.heartbeatTimeout) != nil {
			return s.failScaleInBeforeResize(ctx, op, journal, errors.New("migration destination is no longer fresh and ready"))
		}
		instance, e := s.scheduler.instanceRepo.GetByID(item.InstanceID)
		if e != nil {
			return e
		}
		if instance == nil || instance.RuntimeGeneration != item.SourceGeneration+1 {
			return s.failScaleInBeforeResize(ctx, op, journal, errors.New("migrated instance generation changed"))
		}
		if binding.State == "running" {
			item.State = "completed"
			item.ErrorMessage = ""
		} else if binding.State == "failed" || binding.State == "error" || binding.State == "unhealthy" {
			item.State = "failed"
			return s.failScaleInBeforeResize(ctx, op, journal, errors.New("migration destination did not become healthy"))
		} else {
			item.State = "starting"
		}

	}
	if err = s.saveScaleIn(ctx, op, journal); err != nil {
		return err
	}
	for _, item := range op.Items {
		if item.State != "completed" {
			return nil
		}
	}
	// Before journaling the irreversible phase, both independent stores must
	// show the exact selected source UIDs empty. Pauses/fences exclude new writers.
	for _, source := range journal.Plan.SourcePods {
		pod, e := s.scheduler.podRepo.GetByID(ctx, source.ID)
		if e != nil {
			return e
		}
		if e = runtimeMaintenanceEmptyReport(pod, source.PodUID, time.Now()); e != nil {
			journal.ErrorMessage = "waiting for fresh empty source report: " + e.Error()
			return s.saveScaleIn(ctx, op, journal)
		}
		if !pod.SchedulingPaused {
			return s.failScaleInBeforeResize(ctx, op, journal, errors.New("scale-in source scheduling pause changed"))
		}
		bindings, e := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, source.ID)
		if e != nil {
			return e
		}
		if len(bindings) > 0 {
			return s.failScaleInBeforeResize(ctx, op, journal, errors.New("source acquired an unplanned binding; replica count preserved"))
		}
	}
	if err = kube.ConfirmRuntimeScaleInAssignmentsEmpty(ctx, journal.Plan.Namespace, journal.Protection.SourcePods); err != nil {
		return s.failScaleInBeforeResize(ctx, op, journal, err)
	}
	// Acquire processing ownership for the final mutation, and re-read cancel
	// under the same SQL row lock used by Cancel. Once resizing is journaled,
	// cancellation cannot unprotect retained Pods until controller convergence.
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		var cancelled bool
		var status, owner string
		row, e := tx.SQL().QueryRowContext(ctx, "SELECT status,cancel_requested,owner_token FROM runtime_management_operations WHERE id=? FOR UPDATE", op.ID)
		if e == nil {
			e = row.Scan(&status, &cancelled, &owner)
		}
		if e != nil {
			return e
		}
		if cancelled {
			return errors.New("scale-in cancelled before replica write")
		}
		if status != "starting" || owner != s.token {
			return errors.New("scale-in ownership changed before replica write")
		}
		journal.Phase = "resizing"
		raw, e := json.Marshal(journal)
		if e != nil {
			return e
		}
		_, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET status='processing',owner_token=?,request_json=?,updated_at=? WHERE id=?", s.token, string(raw), time.Now().UTC(), op.ID)
		return e
	}, nil)
	if err != nil {
		return s.failScaleInBeforeResize(ctx, op, journal, err)
	}
	op.Status = "processing"
	if err = s.assertOwner(ctx, op.ID); err != nil {
		return err
	}
	if err = s.withScaleInKubeMutation(ctx, op, journal, func() error {
		return kube.ResizeRuntimeScaleIn(ctx, journal.Target, journal.Snapshot, journal.Protection)
	}); err != nil {
		op.Status = "needs_attention"
		journal.ErrorMessage = "replica write result requires observation: " + err.Error()
		return s.saveScaleIn(ctx, op, journal)
	}
	journal.Phase = "observing"
	op.Status = "starting"
	return s.saveScaleIn(ctx, op, journal)
}

// Operation-row serialization covers the external controller mutations. A
// scheduler replacement cannot unprotect retained Pods while an old term's
// replica CAS is in flight, even after it requested cancellation or recovery.
func (s *RuntimeManagementService) withScaleInKubeMutation(ctx context.Context, op *RuntimeManagementOperation, journal *runtimePodScaleInJournal, fn func() error) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		var status, owner, raw string
		row, err := tx.SQL().QueryRowContext(ctx, "SELECT status,owner_token,request_json FROM runtime_management_operations WHERE id=? FOR UPDATE", op.ID)
		if err == nil {
			err = row.Scan(&status, &owner, &raw)
		}
		if err != nil {
			return err
		}
		var current runtimePodScaleInJournal
		if err = json.Unmarshal([]byte(raw), &current); err != nil {
			return err
		}
		if owner != s.token || (status != "processing" && status != "starting" && status != "needs_attention") || current.Phase != journal.Phase {
			return errors.New("scale-in execution owner or phase changed; Kubernetes mutation held")
		}
		return fn()
	}, nil)
}
