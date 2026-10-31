package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"path"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation"
)

var stageResourceIDPattern = regexp.MustCompile(`^srs_[a-f0-9]{32}$`)

type StageVolumeRequest struct {
	Phase              string `json:"phase"`
	Namespace          string `json:"namespace"`
	NamespaceUID       string `json:"namespace_uid"`
	SourceNamespaceUID string `json:"source_namespace_uid,omitempty"`
	NodeName           string `json:"node_name"`
	HostRoot           string `json:"host_root"`
	StorageClass       string `json:"storage_class"`
	CapacityBytes      int64  `json:"capacity_bytes"`
}
type StageVolume struct {
	ID, TaskID, State, Hash     string
	Request                     StageVolumeRequest
	PVCUID, PVUID               string
	PVCCreateSent, PVCreateSent bool
}

func (r StageVolumeRequest) Validate() error {
	if r.Phase == "restore" {
		if !refPattern.MatchString(r.SourceNamespaceUID) || r.SourceNamespaceUID == r.NamespaceUID {
			return errors.New("isolation_invalid")
		}
	} else if r.SourceNamespaceUID != "" {
		return errors.New("validation_failed")
	}
	if r.Phase != "capture" && r.Phase != "secret_capture" && r.Phase != "restore" || r.Namespace == "" || len(validation.IsDNS1123Subdomain(r.Namespace)) != 0 || !refPattern.MatchString(r.NamespaceUID) || r.NodeName == "" || len(validation.IsDNS1123Subdomain(r.NodeName)) != 0 || !path.IsAbs(r.HostRoot) || path.Clean(r.HostRoot) != r.HostRoot || r.HostRoot == "/" || strings.ContainsAny(r.HostRoot, "\\:\x00") || r.StorageClass == "" || len(validation.IsDNS1123Subdomain(r.StorageClass)) != 0 || r.CapacityBytes < 1<<20 || r.CapacityBytes > 1<<40 {
		return errors.New("validation_failed")
	}
	return nil
}

func stagePreparing(t Task, phase string) bool {
	return t.Type == Backup && phase != "restore" && t.Status == "preparing" || t.Type == Drill && phase == "restore" && t.Status == "provisioning"
}
func (s Store) beginStageMutation(ctx context.Context, l Leadership, t Task, phase string) (*sql.Tx, Task, error) {
	if phase == "restore" {
		return s.beginDrillMutation(ctx, l, t)
	}
	return s.beginCaptureMutation(ctx, l, t)
}
func (v StageVolume) Name() string     { return "backup-stage-" + strings.TrimPrefix(v.ID, "srs_") }
func (v StageVolume) HostPath() string { return path.Join(v.Request.HostRoot, v.ID) }

const stageVolumeColumns = "public_id,task_id,state,request_hash,request_json,pvc_uid,pv_uid,pvc_create_sent,pv_create_sent"

func scanStageVolume(row rowScanner) (StageVolume, error) {
	var v StageVolume
	var raw []byte
	if err := row.Scan(&v.ID, &v.TaskID, &v.State, &v.Hash, &raw, &v.PVCUID, &v.PVUID, &v.PVCCreateSent, &v.PVCreateSent); err != nil {
		return v, err
	}
	if !stageResourceIDPattern.MatchString(v.ID) || strictDecode(raw, &v.Request) != nil || v.Request.Validate() != nil {
		return v, errors.New("evidence_incomplete")
	}
	hash, err := RequestHash(v.Request)
	if err != nil || hash != v.Hash {
		return v, errors.New("evidence_incomplete")
	}
	return v, nil
}
func (s Store) StageVolume(ctx context.Context, taskID, phase string) (StageVolume, error) {
	return scanStageVolume(s.DB.QueryRowContext(ctx, "SELECT "+stageVolumeColumns+" FROM system_backup_stage_resources WHERE task_id=? AND phase=?", taskID, phase))
}

func (s Store) withStageDirectoryFence(ctx context.Context, l Leadership, t Task, v StageVolume, fn func() error) error {
	tx, x, err := s.beginStageMutation(ctx, l, t, v.Request.Phase)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	actual, err := scanStageVolume(tx.QueryRowContext(ctx, "SELECT "+stageVolumeColumns+" FROM system_backup_stage_resources WHERE public_id=? AND task_id=? FOR UPDATE", v.ID, x.ID))
	if err != nil {
		return err
	}
	if !stagePreparing(x, v.Request.Phase) || actual.Hash != v.Hash || actual.State != "prepared" && actual.State != "ready" {
		return errors.New("state_conflict")
	}
	if err = fn(); err != nil {
		return err
	}
	return tx.Commit()
}
func (s Store) PrepareStageVolume(ctx context.Context, l Leadership, t Task, r StageVolumeRequest) (StageVolume, error) {
	var zero StageVolume
	if r.Validate() != nil {
		return zero, errors.New("validation_failed")
	}
	tx, x, err := s.beginStageMutation(ctx, l, t, r.Phase)
	if err != nil {
		return zero, err
	}
	defer tx.Rollback()
	hash, err := RequestHash(r)
	if err != nil {
		return zero, err
	}
	old, err := scanStageVolume(tx.QueryRowContext(ctx, "SELECT "+stageVolumeColumns+" FROM system_backup_stage_resources WHERE task_id=? AND phase=? FOR UPDATE", x.ID, r.Phase))
	if err == nil {
		if old.Hash != hash {
			return zero, errors.New("artifact_conflict")
		}
		return old, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return zero, err
	}
	if !stagePreparing(x, r.Phase) || x.CommitStarted {
		return zero, errors.New("state_conflict")
	}
	var valid bool
	if err = tx.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)<?", x.DeadlineAt).Scan(&valid); err != nil {
		return zero, err
	}
	if !valid {
		return zero, errors.New("deadline_exceeded")
	}
	id, err := RandomID("srs_")
	if err != nil {
		return zero, err
	}
	raw, err := canonicalJSON(r)
	if err != nil {
		return zero, err
	}
	if _, err = tx.ExecContext(ctx, "INSERT INTO system_backup_stage_resources(public_id,task_id,phase,request_hash,request_json) VALUES(?,?,?,?,?)", id, x.ID, r.Phase, hash, raw); err != nil {
		return zero, err
	}
	if err = appendEvent(ctx, tx, x.ID, "diagnostic.staging.prepared", map[string]string{"resource_id": id, "phase": r.Phase, "request_hash": hash}); err != nil {
		return zero, err
	}
	return StageVolume{ID: id, TaskID: x.ID, State: "prepared", Hash: hash, Request: r}, tx.Commit()
}

func (s Store) updateStageVolume(ctx context.Context, l Leadership, t Task, v StageVolume, action, uid string) error {
	tx, _, err := s.beginStageMutation(ctx, l, t, v.Request.Phase)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	actual, err := scanStageVolume(tx.QueryRowContext(ctx, "SELECT "+stageVolumeColumns+" FROM system_backup_stage_resources WHERE public_id=? AND task_id=? FOR UPDATE", v.ID, t.ID))
	if err != nil {
		return err
	}
	if actual.Hash != v.Hash {
		return errors.New("artifact_conflict")
	}
	query := ""
	var args []any
	switch action {
	case "pvc_sent", "pv_sent":
		if actual.State != "prepared" {
			return errors.New("state_conflict")
		}
		if action == "pvc_sent" && actual.PVCCreateSent || action == "pv_sent" && actual.PVCreateSent {
			return unknownBackupExecution(errors.New("artifact_unavailable"))
		}
		column := "pvc_create_sent"
		if action == "pv_sent" {
			column = "pv_create_sent"
		}
		query = "UPDATE system_backup_stage_resources SET " + column + "=1 WHERE public_id=?"
		args = []any{v.ID}
	case "pvc_uid", "pv_uid":
		if !refPattern.MatchString(uid) || actual.State == "deleted" {
			return errors.New("validation_failed")
		}
		old, column := actual.PVCUID, "pvc_uid"
		if action == "pv_uid" {
			old, column = actual.PVUID, "pv_uid"
		}
		if old != "" && old != uid {
			return errors.New("artifact_conflict")
		}
		query = "UPDATE system_backup_stage_resources SET " + column + "=? WHERE public_id=?"
		args = []any{uid, v.ID}
	case "ready":
		if actual.State != "prepared" && actual.State != "ready" || actual.PVCUID == "" || actual.PVUID == "" {
			return errors.New("state_conflict")
		}
		query = "UPDATE system_backup_stage_resources SET state='ready' WHERE public_id=?"
		args = []any{v.ID}
	case "delete_requested":
		if actual.State == "deleted" {
			return tx.Commit()
		}
		query = "UPDATE system_backup_stage_resources SET state='delete_requested' WHERE public_id=?"
		args = []any{v.ID}
	case "deleted":
		if actual.State != "delete_requested" && actual.State != "deleted" {
			return errors.New("state_conflict")
		}
		query = "UPDATE system_backup_stage_resources SET state='deleted',deleted_at=COALESCE(deleted_at,UTC_TIMESTAMP(6)) WHERE public_id=?"
		args = []any{v.ID}
	default:
		return errors.New("validation_failed")
	}
	if _, err = tx.ExecContext(ctx, query, args...); err != nil {
		return err
	}
	return tx.Commit()
}
