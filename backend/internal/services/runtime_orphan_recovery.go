package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/runtimeeviction"
	"clawreef/internal/services/k8s"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"net"
	"net/url"
	"sort"
	"time"
)

type runtimeOrphanRecoveryBackend interface {
	PrepareRuntimeOrphanSource(context.Context, string, string, string, string) (*k8s.RuntimeOrphanSource, error)
	RetireRuntimeOrphanController(context.Context, *k8s.RuntimeOrphanSource, string) (bool, error)
	RecoverRuntimeOrphan(context.Context, runtimeeviction.PodScope, *corev1.Pod) (*runtimeeviction.PodReceipt, error)
	FinalizeRuntimeOrphanSource(context.Context, *k8s.RuntimeOrphanSource) error
}
type RuntimeOrphanRecoveryJournal struct {
	Phase              string                           `json:"phase"`
	SourcePodID        int64                            `json:"source_pod_id"`
	Source             *k8s.RuntimeOrphanSource         `json:"source"`
	Owners             []runtimeeviction.PodLedgerOwner `json:"owners"`
	Publications       []k8s.RuntimeOrphanPublication   `json:"publications"`
	Scope              runtimeeviction.PodScope         `json:"scope"`
	Receipt            *runtimeeviction.PodReceipt      `json:"receipt,omitempty"`
	SourceEndpoint     string                           `json:"source_endpoint"`
	GuardedInstanceIDs []int                            `json:"guarded_instance_ids,omitempty"`
	FenceCreated       bool                             `json:"fence_created"`
}

func (s *RuntimeManagementService) supportsOrphanRecovery() bool {
	if s == nil || s.sess == nil || s.scheduler == nil {
		return false
	}
	client, ok := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	if !ok || client.kube == nil || client.assignments == nil {
		return false
	}
	_, ok = s.scheduler.deployments.(runtimeOrphanRecoveryBackend)
	return ok
}
func (s *RuntimeManagementService) checkOrphanOwners(ctx context.Context, tx db.Session, j *RuntimeOrphanRecoveryJournal, c *slotRuntimeAgentClient) error {
	if e := repository.CheckRuntimeOrphanSource(ctx, tx, journalPodID(j), j.Source.PodUID, j.Source.Namespace, j.Source.DeploymentName); e != nil {
		return e
	}
	// Publication runs under the same global admission lock. Re-read the full
	// native source inventory after acquiring it, so a just-published owner is seen.
	var orphanWrites int
	row, queryErr := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instance_workspace_write_intents w LEFT JOIN instances i ON i.id=w.instance_id WHERE i.id IS NULL")
	if queryErr != nil {
		return queryErr
	}
	if queryErr = row.Scan(&orphanWrites); queryErr != nil {
		return queryErr
	}
	if orphanWrites > 0 {
		return fmt.Errorf("unattributed durable workspace writer intent prevents orphan proof")
	}
	known := map[string]bool{}
	for _, p := range j.Publications {
		if p.CurrentSource != nil {
			known[p.Audit.GatewayID] = true
		}
	}
	maps, e := c.kube.CoreV1().ConfigMaps(j.Source.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-instance"})
	if e != nil {
		return e
	}
	endpoint, _ := url.Parse(j.SourceEndpoint)
	for _, cm := range maps.Items {
		sameEndpoint := false
		if other, e := url.Parse(cm.Data["endpoint"]); e == nil && endpoint != nil {
			sameEndpoint = other.Hostname() != "" && other.Hostname() == endpoint.Hostname()
		}
		if cm.Data["pod_uid"] == j.Source.PodUID {
			if !known[cm.Data["gateway_id"]] {
				return fmt.Errorf("source gained a publication after preflight; inspect again")
			}
		} else if sameEndpoint && cm.Data["desired_state"] != "stopped" {
			return fmt.Errorf("source endpoint has a different current publisher")
		}
	}
	for _, record := range j.Publications {
		a := record.Audit
		var owner, generation int
		var status, kind, mode, runtime string
		row, e := tx.SQL().QueryRowContext(ctx, "SELECT user_id,runtime_generation,status,type,instance_mode,runtime_type FROM instances WHERE id=? FOR UPDATE", a.InstanceID)
		if e != nil {
			return e
		}
		e = row.Scan(&owner, &generation, &status, &kind, &mode, &runtime)
		if e != nil && e != sql.ErrNoRows {
			return e
		}
		exists := e == nil
		var count int
		for _, q := range []string{"SELECT COUNT(*) FROM instance_runtime_bindings WHERE instance_id=?", "SELECT COUNT(*) FROM instance_workspace_write_intents WHERE instance_id=?", "SELECT COUNT(*) FROM team_members WHERE instance_id=? AND status NOT IN ('deleted','deleting')", "SELECT COUNT(*) FROM gateway_runtime_upgrades WHERE instance_id=? AND state NOT IN ('cancelled','completed')"} {
			row, e = tx.SQL().QueryRowContext(ctx, q, a.InstanceID)
			if e != nil {
				return e
			}
			if e = row.Scan(&count); e != nil {
				return e
			}
			if count > 0 {
				return fmt.Errorf("source instance %d has a binding, write, Team or protected upgrade", a.InstanceID)
			}
		}
		if !exists {
			continue
		}
		if owner != a.UserID || generation < a.Generation || kind != a.RuntimeType || mode != "lite" || runtime != RuntimeBackendGateway {
			return fmt.Errorf("source instance %d owner or generation changed", a.InstanceID)
		}
		if status != "stopped" && status != "deleting" {
			return fmt.Errorf("source instance %d is %s and may publish; use instance recovery", a.InstanceID, status)
		}

	}
	return nil
}

// The immutable SQL source ID is stored outside the public helper scope.
func journalPodID(j *RuntimeOrphanRecoveryJournal) int64 { return j.SourcePodID }
func (s *RuntimeManagementService) orphanRecoveryPreflight(ctx context.Context, req RuntimeSchedulingRequest) (*RuntimeSchedulingPreflight, error) {
	if req.SourcePodID <= 0 || req.SourcePodUID == "" || len(req.InstanceIDs) != 0 || req.TargetPodID != 0 || req.TargetSlot != 0 {
		return nil, fmt.Errorf("orphan recovery requires exact source_pod_uid and no instance or target selection")
	}
	if !s.supportsOrphanRecovery() {
		return nil, fmt.Errorf("independent orphan recovery is unavailable")
	}

	c := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	source, e := s.scheduler.podRepo.GetByID(ctx, req.SourcePodID)
	if e != nil {
		return nil, e
	}
	if source == nil || stringValue(source.PodUID) != req.SourcePodUID || source.Namespace != s.scheduler.runtimeNamespace || source.AgentEndpoint == nil {
		return nil, fmt.Errorf("selected orphan Pod UID changed")
	}
	backend := s.scheduler.deployments.(runtimeOrphanRecoveryBackend)
	pinned, e := backend.PrepareRuntimeOrphanSource(ctx, source.Namespace, source.PodName, req.SourcePodUID, s.scheduler.workspaceRoot)
	if e != nil {
		return nil, e
	}
	if source.RuntimeType != pinned.RuntimeType {
		return nil, fmt.Errorf("registered Runtime type differs from verified controller")
	}
	if e = k8s.VerifyRuntimeOrphanManagementWorkspace(ctx, c.kube, pinned); e != nil {
		return nil, e
	}
	endpoint, e := url.Parse(*source.AgentEndpoint)
	if e != nil || endpoint.Scheme != "https" || endpoint.Port() == "" || net.ParseIP(endpoint.Hostname()) == nil {
		return nil, fmt.Errorf("source endpoint cannot be independently attributed")
	}
	live, e := c.kube.CoreV1().Pods(source.Namespace).Get(ctx, source.PodName, metav1.GetOptions{})
	if e != nil {
		return nil, e
	}
	if endpoint.Hostname() != live.Status.PodIP {
		return nil, fmt.Errorf("source endpoint differs from current exact Pod IP")
	}
	owners, e := runtimeeviction.InspectPodLedgerOwners(s.scheduler.workspaceRoot, pinned.RuntimeType, req.SourcePodUID)
	if e != nil {
		return nil, e
	}
	records, e := c.assignments.SnapshotRuntimeOrphanPublications(ctx, pinned, owners, *source.AgentEndpoint)
	if e != nil {
		return nil, e
	}
	j := &RuntimeOrphanRecoveryJournal{Phase: "planned", Source: pinned, Owners: owners, Publications: records, SourceEndpoint: *source.AgentEndpoint, SourcePodID: source.ID}
	if len(records) == 0 {
		if e = k8s.CheckRuntimeOrphanExternalWriters(ctx, c.kube, pinned); e != nil {
			return nil, e
		}
	}
	e = s.sess.TxContext(ctx, func(tx db.Session) error {
		if e := repository.LockRuntimeManagementAdmission(ctx, tx); e != nil {
			return e
		}
		return s.checkOrphanOwners(ctx, tx, j, c)
	}, nil)
	if e != nil {
		return nil, e
	}
	return &RuntimeSchedulingPreflight{Allowed: true, Items: []RuntimeSchedulingItem{{InstanceID: 0, SourcePodID: source.ID, SourcePodUID: req.SourcePodUID, State: "ready", OrphanRecovery: j}}, Blockers: []string{}}, nil
}
func (s *RuntimeManagementService) submitOrphanRecovery(ctx context.Context, podID int64, req RuntimeSchedulingRequest, actor *int) (*RuntimeManagementOperation, error) {
	req.SourcePodID = podID
	plan, e := s.orphanRecoveryPreflight(ctx, req)
	if e != nil {
		return nil, e
	}
	now := time.Now().UTC()
	op := &RuntimeManagementOperation{ID: "runtime_mgmt_" + uuid.NewString(), Kind: "orphan_recovery", Status: "pending", Items: plan.Items, CreatedAt: now, UpdatedAt: now}
	j := op.Items[0].OrphanRecovery
	j.Scope = j.Source.Scope(op.ID)
	op.Items[0].State = "pending"
	c := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	seen := map[int]bool{}
	for _, publication := range j.Publications {
		id := publication.Audit.InstanceID
		if seen[id] {
			continue
		}
		seen[id] = true
		var count int
		row, err := s.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instances WHERE id=?", id)
		if err != nil {
			return nil, err
		}
		if err = row.Scan(&count); err != nil {
			return nil, err
		}
		if count > 0 {
			j.GuardedInstanceIDs = append(j.GuardedInstanceIDs, id)
		}
	}
	sort.Ints(j.GuardedInstanceIDs)
	if len(j.GuardedInstanceIDs) > 0 {
		locker, ok := s.scheduler.instanceRepo.(interface {
			AcquireLifecycleLock(context.Context, int) (func(), error)
		})
		if !ok {
			return nil, fmt.Errorf("durable source lifecycle locks unavailable")
		}
		releases := []func(){}
		defer func() {
			for i := len(releases) - 1; i >= 0; i-- {
				releases[i]()
			}
		}()
		for _, id := range j.GuardedInstanceIDs {
			release, err := locker.AcquireLifecycleLock(ctx, id)
			if err != nil {
				return nil, err
			}
			releases = append(releases, release)
		}
		if s.scheduler.upgrade == nil {
			return nil, fmt.Errorf("shared workspace mutation fences unavailable")
		}
		release, err := s.scheduler.upgrade.lockWorkspaceOperations(j.GuardedInstanceIDs)
		if err != nil {
			return nil, err
		}
		defer release()
		for _, id := range j.GuardedInstanceIDs {
			if err = s.scheduler.upgrade.validateInstanceMutation(ctx, s.sess, id); err != nil {
				return nil, err
			}
		}
	}
	e = s.sess.TxContext(ctx, func(tx db.Session) error {
		if err := repository.LockRuntimeOrphanLifecycleAdmission(ctx, tx); err != nil {
			return err
		}

		if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
			return err
		}
		var active int
		for _, q := range []string{"SELECT COUNT(*) FROM runtime_rollouts WHERE status IN ('pending','running')", "SELECT COUNT(*) FROM runtime_management_operations WHERE status IN ('pending','processing','starting','needs_attention')"} {
			row, err := tx.SQL().QueryRowContext(ctx, q)
			if err != nil {
				return err
			}
			if err = row.Scan(&active); err != nil {
				return err
			}
			if active > 0 {
				return fmt.Errorf("another Runtime operation is active")
			}
		}
		attributed := []int{}
		for _, publication := range j.Publications {
			attributed = append(attributed, publication.Audit.InstanceID)
		}
		if err := repository.CheckRuntimeOrphanLifecycleReservations(ctx, tx, attributed); err != nil {
			return err
		}
		if err := s.checkOrphanOwners(ctx, tx, j, c); err != nil {
			return err
		}
		guarded := map[int]bool{}
		for _, id := range j.GuardedInstanceIDs {
			guarded[id] = true
		}
		for _, publication := range j.Publications {
			var exists int
			row, err := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instances WHERE id=?", publication.Audit.InstanceID)
			if err != nil {
				return err
			}
			if err = row.Scan(&exists); err != nil {
				return err
			}
			if exists > 0 && !guarded[publication.Audit.InstanceID] {
				return fmt.Errorf("source instance appeared after eligibility; refresh preflight")
			}
		}
		existing, err := repository.RuntimePodDeletionIntentUID(ctx, tx, j.Source.Namespace, j.Source.DeploymentName)
		if err != nil {
			return err
		}
		j.FenceCreated = existing == ""
		if err = repository.FenceRuntimeOrphanSource(ctx, tx, podID, j.Source.PodUID, j.Source.Namespace, j.Source.DeploymentName, j.Source.DeploymentUID); err != nil {
			return err
		}
		for _, id := range j.GuardedInstanceIDs {
			if _, err = tx.SQL().ExecContext(ctx, "INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(?,?,?)", id, op.ID, now); err != nil {
				return fmt.Errorf("source lifecycle reservation is busy: %w", err)
			}
		}
		raw, _ := json.Marshal(req)
		items, _ := json.Marshal(op.Items)
		_, err = tx.SQL().ExecContext(ctx, "INSERT INTO runtime_management_operations(id,kind,status,actor_user_id,request_json,items_json,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)", op.ID, op.Kind, op.Status, actor, string(raw), string(items), now, now)
		return err
	}, nil)
	return op, e
}

func (s *RuntimeManagementService) cancelOrphanRecovery(ctx context.Context, id string) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		if e := repository.LockRuntimeManagementAdmission(ctx, tx); e != nil {
			return e
		}
		var raw, status string
		row, e := tx.SQL().QueryRowContext(ctx, "SELECT items_json,status FROM runtime_management_operations WHERE id=? AND kind='orphan_recovery' FOR UPDATE", id)
		if e != nil {
			return e
		}
		if e = row.Scan(&raw, &status); e != nil {
			return e
		}
		if status != "pending" && status != "processing" && status != "needs_attention" {
			return fmt.Errorf("orphan recovery cannot be cancelled in this state")
		}
		var items []RuntimeSchedulingItem
		if e = json.Unmarshal([]byte(raw), &items); e != nil {
			return e
		}
		if len(items) != 1 || items[0].OrphanRecovery == nil || items[0].OrphanRecovery.Phase != "planned" {
			return fmt.Errorf("orphan recovery already fenced source execution; Retry the same operation")
		}
		j := items[0].OrphanRecovery
		if j.FenceCreated {
			if _, e = tx.SQL().ExecContext(ctx, "DELETE FROM runtime_pod_deletion_intents WHERE namespace=? AND deployment_name=? AND deployment_uid=?", j.Source.Namespace, j.Source.DeploymentName, j.Source.DeploymentUID); e != nil {
				return e
			}
		}
		if _, e = tx.SQL().ExecContext(ctx, "DELETE FROM instance_lifecycle_manual_guards WHERE token=?", id); e != nil {
			return e
		}
		items[0].State = "cancelled"
		j.Phase = "cancelled"
		encoded, _ := json.Marshal(items)
		_, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET status='cancelled',cancel_requested=1,items_json=?,updated_at=? WHERE id=?", string(encoded), time.Now().UTC(), id)
		return e
	}, nil)
}
func (s *RuntimeManagementService) beginOrphanRecovery(ctx context.Context, op *RuntimeManagementOperation) error {
	return s.sess.TxContext(ctx, func(tx db.Session) error {
		var owner, status, raw string
		var cancelled bool
		row, e := tx.SQL().QueryRowContext(ctx, "SELECT owner_token,status,items_json,cancel_requested FROM runtime_management_operations WHERE id=? FOR UPDATE", op.ID)
		if e != nil {
			return e
		}
		if e = row.Scan(&owner, &status, &raw, &cancelled); e != nil {
			return e
		}
		if owner != s.token || status != "processing" || cancelled {
			return fmt.Errorf("orphan recovery ownership changed or cancelled")
		}
		var items []RuntimeSchedulingItem
		if e = json.Unmarshal([]byte(raw), &items); e != nil {
			return e
		}
		if len(items) != 1 || items[0].OrphanRecovery == nil || items[0].OrphanRecovery.Phase != "planned" {
			return fmt.Errorf("orphan recovery phase changed")
		}
		items[0].OrphanRecovery.Phase = "fencing"
		encoded, _ := json.Marshal(items)
		if _, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET items_json=?,updated_at=? WHERE id=?", string(encoded), time.Now().UTC(), op.ID); e != nil {
			return e
		}
		op.Items = items
		return nil
	}, nil)
}
func (s *RuntimeManagementService) processOrphanRecovery(ctx context.Context, op *RuntimeManagementOperation) error {
	if len(op.Items) != 1 || op.Items[0].OrphanRecovery == nil {
		return fmt.Errorf("orphan recovery checkpoint missing")
	}
	j := op.Items[0].OrphanRecovery
	backend, ok := s.scheduler.deployments.(runtimeOrphanRecoveryBackend)
	if !ok {
		return fmt.Errorf("orphan recovery backend unavailable")
	}
	c, ok := s.scheduler.agentClient.(*slotRuntimeAgentClient)
	if !ok {
		return fmt.Errorf("orphan recovery assignment authority unavailable")
	}
	hold := func(e error) error {
		op.Items[0].ErrorMessage = e.Error()
		op.Items[0].State = "processing"
		op.Status = "needs_attention"
		if errors.Is(e, k8s.ErrRuntimeFaultEvictionPending) || errors.Is(e, models.ErrInstanceOperationInProgress) {
			op.Status = "pending"
		}
		return s.save(ctx, op)
	}
	if j.Source == nil || j.Source.SourcePod == nil || j.SourcePodID != op.Items[0].SourcePodID || j.Source.PodUID != op.Items[0].SourcePodUID || string(j.Source.SourcePod.UID) != j.Source.PodUID || j.Source.SourcePod.Namespace != j.Source.Namespace || j.Source.SourcePod.Name != j.Source.PodName || j.Source.SourcePod.Spec.NodeName != j.Source.NodeName || j.Source.SourcePod.Annotations[k8s.RuntimeFaultStorageAnnotation] != j.Source.StorageDigest || j.Source.HelperImage == "" || j.Source.SourcePod.Annotations[k8s.RuntimeFaultHelperImageAnnotation] != j.Source.HelperImage || runtimeeviction.PodScopeDigest(j.Source.Scope(op.ID)) != runtimeeviction.PodScopeDigest(j.Scope) {
		return hold(fmt.Errorf("orphan journal source scope identity changed"))
	}
	if j.Phase == "planned" {
		if e := s.beginOrphanRecovery(ctx, op); e != nil {
			return e
		}
		j = op.Items[0].OrphanRecovery
	}

	if j.Receipt == nil && (j.Phase == "fencing" || j.Phase == "terminating" || j.Phase == "helper_pending") {
		if e := s.assertOwner(ctx, op.ID); e != nil {
			return e
		}
		node, e := c.kube.CoreV1().Nodes().Get(ctx, j.Source.NodeName, metav1.GetOptions{})
		if e != nil {
			return hold(e)
		}
		ready := false
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady && condition.Status == corev1.ConditionTrue {
				ready = true
			}
		}
		if string(node.UID) != j.Source.NodeUID || node.Status.NodeInfo.BootID == "" || !ready {
			return hold(fmt.Errorf("source node UID changed or node is unavailable"))
		}
		if node.Status.NodeInfo.BootID != j.Source.NodeBootID {
			if observer, ok := backend.(interface {
				ObserveRuntimeOrphanReceipt(context.Context, runtimeeviction.PodScope, *corev1.Pod) (*runtimeeviction.PodReceipt, error)
			}); ok && j.Phase == "helper_pending" {
				receipt, e := observer.ObserveRuntimeOrphanReceipt(ctx, j.Scope, j.Source.SourcePod)
				if e != nil {
					return hold(e)
				}
				if receipt != nil {
					if e = runtimeeviction.ValidatePodReceipt(j.Scope, receipt); e != nil {
						return hold(e)
					}
					j.Receipt = receipt
					j.Phase = "source_stopped"
				}
			}
			if j.Receipt == nil {
				j.Source.NodeBootID = node.Status.NodeInfo.BootID
				j.Scope = j.Source.Scope(op.ID)
			}
			if e = s.save(ctx, op); e != nil {
				return e
			}
		}
	}
	if j.Phase == "fencing" || j.Phase == "terminating" {
		if j.Phase == "fencing" {
			if e := s.assertOwner(ctx, op.ID); e != nil {
				return e
			}
			fresh, e := backend.PrepareRuntimeOrphanSource(ctx, j.Source.Namespace, j.Source.PodName, j.Source.PodUID, j.Source.WorkspaceRoot)
			if e != nil {
				return hold(e)
			}
			if fresh.DeploymentUID != j.Source.DeploymentUID || fresh.ReplicaSetUID != j.Source.ReplicaSetUID || fresh.NodeUID != j.Source.NodeUID || fresh.NodeBootID != j.Source.NodeBootID || fresh.StorageDigest != j.Source.StorageDigest {
				return hold(fmt.Errorf("source controller, node boot or storage changed before retirement"))
			}
		}
		e := s.sess.TxContext(ctx, func(tx db.Session) error {
			if e := repository.LockRuntimeManagementAdmission(ctx, tx); e != nil {
				return e
			}
			if e := s.checkOrphanOwners(ctx, tx, j, c); e != nil {
				return e
			}
			for _, id := range j.GuardedInstanceIDs {
				var token string
				row, e := tx.SQL().QueryRowContext(ctx, "SELECT token FROM instance_lifecycle_manual_guards WHERE instance_id=? FOR UPDATE", id)
				if e != nil {
					return e
				}
				if e = row.Scan(&token); e != nil {
					return e
				}
				if token != op.ID {
					return fmt.Errorf("orphan source lifecycle reservation changed")
				}
			}
			return nil
		}, nil)
		if e != nil {
			return hold(e)
		}
		for _, p := range j.Publications {
			var exists int
			row, e := s.sess.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM instances WHERE id=?", p.Audit.InstanceID)
			if e != nil {
				return hold(e)
			}
			if e = row.Scan(&exists); e != nil {
				return hold(e)
			}
			identity := runtimeauth.Identity{InstanceID: p.Audit.InstanceID, UserID: p.Audit.UserID, Generation: p.Audit.Generation, PodUID: p.Audit.PodUID}
			if exists > 0 {
				if e = s.assertOwner(ctx, op.ID); e != nil {
					return e
				}
				if e = c.requireWorkspaceWriteFence(ctx, identity); e != nil {
					return hold(e)
				}
			}
			if e = s.assertOwner(ctx, op.ID); e != nil {
				return e
			}
			if e = c.requireExternalWriterCleanup(ctx, identity); e != nil {
				return hold(e)
			}
		}
		if e = k8s.CheckRuntimeOrphanExternalWriters(ctx, c.kube, j.Source); e != nil {
			return hold(e)
		}
		if e = s.assertOwner(ctx, op.ID); e != nil {
			return e
		}
		if e = c.assignments.FenceRuntimeOrphanPublications(ctx, j.Publications, op.ID); e != nil {
			return hold(e)
		}
		j.Source.SourcePod.Annotations["clawmanager.io/orphan-recovery"] = op.ID
		j.Phase = "terminating"
		if e = s.save(ctx, op); e != nil {
			return e
		}
		if e = s.assertOwner(ctx, op.ID); e != nil {
			return e
		}
		terminal, e := backend.RetireRuntimeOrphanController(ctx, j.Source, op.ID)
		if e != nil {
			return hold(e)
		}
		if !terminal {
			return hold(k8s.ErrRuntimeFaultEvictionPending)
		}
		j.Scope = j.Source.Scope(op.ID)
		j.Phase = "helper_pending"
		if e = s.save(ctx, op); e != nil {
			return e
		}
	}
	if j.Phase == "helper_pending" {
		if e := s.assertOwner(ctx, op.ID); e != nil {
			return e
		}
		receipt, e := backend.RecoverRuntimeOrphan(ctx, j.Scope, j.Source.SourcePod)
		if e != nil {
			return hold(e)
		}
		if e = runtimeeviction.ValidatePodReceipt(j.Scope, receipt); e != nil {
			return hold(e)
		}
		j.Receipt = receipt
		j.Phase = "source_stopped"
		if e = s.save(ctx, op); e != nil {
			return e
		}
	}
	if j.Phase == "source_stopped" {
		if e := runtimeeviction.ValidatePodReceipt(j.Scope, j.Receipt); e != nil {
			return hold(e)
		}
		if e := s.assertOwner(ctx, op.ID); e != nil {
			return e
		}
		if e := c.assignments.ConfirmRuntimeOrphanPublications(ctx, j.Publications, j.Scope, j.Receipt); e != nil {
			return hold(e)
		}
		j.Phase = "sources_retired"
		if e := s.save(ctx, op); e != nil {
			return e
		}
	}
	if j.Phase == "sources_retired" {
		if e := runtimeeviction.ValidatePodReceipt(j.Scope, j.Receipt); e != nil {
			return hold(e)
		}
		if e := s.assertOwner(ctx, op.ID); e != nil {
			return e
		}
		if e := backend.FinalizeRuntimeOrphanSource(ctx, j.Source); e != nil {
			return hold(e)
		}
		e := s.sess.TxContext(ctx, func(tx db.Session) error {
			var owner, status string
			row, e := tx.SQL().QueryRowContext(ctx, "SELECT owner_token,status FROM runtime_management_operations WHERE id=? FOR UPDATE", op.ID)
			if e != nil {
				return e
			}
			if e = row.Scan(&owner, &status); e != nil {
				return e
			}
			if owner != s.token || status != "processing" {
				return fmt.Errorf("orphan recovery owner changed")
			}
			j.Phase = "completed"
			op.Items[0].State = "completed"
			op.Items[0].ErrorMessage = ""
			op.Status = "completed"
			encoded, _ := json.Marshal(op.Items)
			if _, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_pods SET state='deleted',scheduling_paused=1,used_slots=(SELECT COUNT(*) FROM instance_runtime_bindings WHERE runtime_pod_id=?) WHERE id=? AND pod_uid=?", op.Items[0].SourcePodID, op.Items[0].SourcePodID, j.Source.PodUID); e != nil {
				return e
			}
			if _, e = tx.SQL().ExecContext(ctx, "DELETE FROM instance_lifecycle_manual_guards WHERE token=?", op.ID); e != nil {
				return e
			}
			_, e = tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET status='completed',items_json=?,updated_at=? WHERE id=?", string(encoded), time.Now().UTC(), op.ID)
			return e
		}, nil)
		return e
	}
	return nil
}
