package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
)

const runtimeIdlePodGrace = time.Minute

func idleRuntimePodEmpty(pod models.RuntimePod, now time.Time) error {
	pod.State = pod.AgentState // Inventory keeps Kubernetes and Agent states separate.
	if err := runtimeMaintenanceEmptyReport(&pod, stringValue(pod.PodUID), now); err != nil {
		return err
	}
	return models.ConfirmedEmptyRuntimeObservation(pod, now)
}

// Protect concrete stop identities, not every pool of an unbound Creating row.
// Claims and bindings are rechecked under the admission lock before publication;
// a later start can restore sleeping capacity through the ordinary scheduler.
func (s *RuntimeManagementService) idlePoolDemandAdmission(ctx context.Context, tx db.Session, sources []RuntimePodScaleInSource) error {
	if len(sources) == 0 {
		return fmt.Errorf("idle pool source identity required")
	}
	conditions := make([]string, 0, len(sources))
	stopArgs := make([]any, 0, 2*len(sources))
	for _, source := range sources {
		if source.ID <= 0 || source.PodUID == "" {
			return fmt.Errorf("idle pool source identity required")
		}
		conditions = append(conditions, "(pod_uid=? OR (runtime_pod_id=? AND pod_uid=''))")
		stopArgs = append(stopArgs, source.PodUID, source.ID)
	}
	var count int
	row, err := tx.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM instance_idle_stop_operations WHERE phase NOT IN ('completed','failed') AND (`+strings.Join(conditions, " OR ")+`)`, stopArgs...)
	if err != nil {
		return err
	}
	if err = row.Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("idle pool removal postponed: instance start or unconfirmed stop exists")
	}
	return nil
}

// Run only from the existing leader's serial reconcile loop. A restart or any
// unavailable inventory resets the full grace period rather than assuming idle.
func (s *RuntimeManagementService) reconcileIdleRuntimePools(ctx context.Context, now time.Time) error {
	if s.scheduler.autoStopRepo == nil || now.Sub(s.lastIdlePoolScan) < 10*time.Second {
		return nil
	}
	if !s.lastIdlePoolScan.IsZero() && now.Sub(s.lastIdlePoolScan) > 30*time.Second {
		s.idlePodSince = nil
	}
	s.lastIdlePoolScan = now
	settings, err := s.scheduler.autoStopRepo.Settings(ctx)
	if err != nil {
		s.idlePodSince = nil
		return err
	}
	if !settings.Enabled {
		s.idlePodSince = nil
		return nil
	}
	// Keep observing other empty pools while one operation owns admission.
	// A busy pool loses its own observation below; unrelated pools retain their
	// continuously verified grace instead of starting over after each drain.
	admissionErr := s.scaleInAdmission(ctx, s.sess)
	kube, err := s.scaleInKube()
	if err != nil {
		s.idlePodSince = nil
		return err
	}
	inventory, err := s.scheduler.RuntimeDeploymentPods(ctx, "")
	if err != nil {
		s.idlePodSince = nil
		return err
	}
	seen := map[string]time.Time{}
	groups := map[string][]models.RuntimePod{}
	for _, pod := range inventory {
		if pod.Namespace != s.scheduler.runtimeNamespace || !runtimePodEligibleForOrdinaryScheduling(pod) {
			continue
		}
		key := pod.Namespace + "/" + pod.DeploymentName
		groups[key] = append(groups[key], pod)
		uid := stringValue(pod.PodUID)
		if runtimeScaleInFreshPod(pod, s.scheduler.heartbeatTimeout) != nil || idleRuntimePodEmpty(pod, now) != nil {
			continue
		}
		bindings, err := s.scheduler.bindingRepo.ListByRuntimePodID(ctx, pod.ID)
		if err != nil {
			s.idlePodSince = nil
			return err
		}
		if len(bindings) != 0 {
			continue
		}
		since := s.idlePodSince[uid]
		if since.IsZero() {
			since = now
		}
		seen[uid] = since
	}
	s.idlePodSince = seen
	if admissionErr != nil {
		return nil
	}
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pods := groups[key]
		selected := map[string]bool{}
		sources := []RuntimePodScaleInSource{}
		for _, pod := range pods {
			uid := stringValue(pod.PodUID)
			if since, ok := seen[uid]; ok && now.Sub(since) >= runtimeIdlePodGrace && len(selected) < 10 {
				selected[uid] = true
				sources = append(sources, RuntimePodScaleInSource{ID: pod.ID, PodUID: uid})
			}
		}
		if len(selected) == 0 {
			continue
		}
		if err := s.idlePoolDemandAdmission(ctx, s.sess, sources); err != nil {
			log.Printf("runtime idle pool %s removal postponed: %v", key, err)
			continue
		}
		journal, err := s.idleRuntimeScaleInPlan(ctx, kube, pods, selected)
		if err != nil {
			log.Printf("runtime idle pool %s removal postponed: %v", key, err)
			continue
		} // Unsafe/unstable pools retain capacity; other pools can still qualify.
		op, err := s.submitScaleInJournal(ctx, journal, nil)
		if err != nil {
			return err
		}
		log.Printf("runtime idle pool scale-in queued: pool=%s replicas=%d->%d operation=%s", key, journal.Plan.PreviousReplicas, journal.Plan.Replicas, op.ID)
		return nil // Shared admission permits one durable management operation at a time.
	}
	return nil
}

func (s *RuntimeManagementService) idleRuntimeScaleInPlan(ctx context.Context, kube k8s.RuntimePodScaleInDeploymentService, pods []models.RuntimePod, selected map[string]bool) (*runtimePodScaleInJournal, error) {
	first := pods[0]
	target := k8s.RuntimePodScaleTarget{RuntimeType: first.RuntimeType, Namespace: first.Namespace, DeploymentName: first.DeploymentName, PodName: first.PodName, PodUID: stringValue(first.PodUID), UpgradeID: first.UpgradeID, SourceDeployment: first.SourceDeployment, ImageRef: first.ImageRef, IdlePool: true}
	if err := repository.CheckRuntimePodDeletionIntent(ctx, s.sess, target.Namespace, target.DeploymentName); err != nil {
		return nil, err
	}
	snapshot, err := kube.InspectRuntimeScale(ctx, target)
	if err != nil {
		return nil, err
	}
	native, err := kube.InspectRuntimeScaleInPods(ctx, target, snapshot)
	if err != nil {
		return nil, err
	}
	if len(native) != len(pods) {
		return nil, fmt.Errorf("idle pool sibling registration is incomplete")
	}
	journal := &runtimePodScaleInJournal{AutomaticIdle: true, RuntimePodScaleInRequest: RuntimePodScaleInRequest{RuntimeType: first.RuntimeType, PodCount: len(selected)}, Target: target, Snapshot: snapshot, Phase: "pending"}
	journal.Plan = RuntimePodScaleInPreflight{Allowed: true, RuntimeType: first.RuntimeType, Namespace: first.Namespace, DeploymentName: first.DeploymentName, DeploymentUID: snapshot.Option.DeploymentUID, PreviousReplicas: snapshot.Option.Replicas, Replicas: snapshot.Option.Replicas - int32(len(selected)), RemovedPods: len(selected), Items: []RuntimeSchedulingItem{}}
	journal.Protection = k8s.RuntimePodScaleInProtection{Namespace: first.Namespace, DeploymentName: first.DeploymentName, DeploymentUID: snapshot.Option.DeploymentUID, PreviousReplicas: snapshot.Option.Replicas, Replicas: journal.Plan.Replicas}
	byUID := map[string]models.RuntimePod{}
	for _, pod := range pods {
		byUID[stringValue(pod.PodUID)] = pod
	}
	for _, identity := range native {
		pod, ok := byUID[identity.PodUID]
		if !ok || pod.PodName != identity.PodName || runtimeScaleInFreshPod(pod, s.scheduler.heartbeatTimeout) != nil {
			return nil, fmt.Errorf("idle pool sibling identity or readiness changed")
		}
		if selected[identity.PodUID] {
			if err = idleRuntimePodEmpty(pod, time.Now().UTC()); err != nil {
				return nil, err
			}
			journal.Plan.SourcePods = append(journal.Plan.SourcePods, RuntimePodScaleInSource{ID: pod.ID, PodName: pod.PodName, PodUID: identity.PodUID})
			journal.Protection.SourcePods = append(journal.Protection.SourcePods, identity)
		} else {
			journal.Protection.RetainedPods = append(journal.Protection.RetainedPods, identity)
		}
	}
	if len(journal.Plan.SourcePods) != len(selected) {
		return nil, fmt.Errorf("idle source set changed")
	}
	return journal, nil
}

func runtimeIdleWakeResult(err error) error {
	if err != nil {
		return fmt.Errorf("%w: restore sleeping Runtime capacity: %v", errRuntimeControlPlanePending, err)
	}
	return errRuntimeScaleOutPending
}

// This restores capacity only for an existing scheduler-managed start intent.
// Template descriptions below have no fabricated Pod ID, UID or live status;
// canonical upgrade placement can select them, but copies require live witnesses.
// Actual allocation ALWAYS returns through the normal live inventory path.
func (s *RuntimeScheduler) restoreIdleRuntimeCapacity(ctx context.Context, instance models.Instance, runtimeType string) (bool, error) {
	if s.management == nil {
		return false, nil
	}
	wake, ok := s.deployments.(k8s.RuntimeIdlePoolDeploymentService)
	if !ok {
		return false, nil
	}
	ids, err := wake.IdleRuntimePoolOperations(ctx, s.runtimeNamespace, runtimeType)
	if err != nil {
		return false, err
	}
	if len(ids) == 0 {
		return false, nil
	}
	// A restart keeps its last concrete source. New requests instead grow an
	// already proven serving pool, even when all of its slots are occupied.
	// Otherwise every burst can wake a different historical binary before the
	// ordinary scale-out path gets a chance to expand the current pool.
	source, sourceImage := "", ""
	if runtimeType != RuntimeTypeOpenClaw {
		source, sourceImage, err = s.idleRuntimeSource(ctx, instance.ID, runtimeType)
		if err != nil {
			return false, err
		}
	}
	var approvedImage string
	if source == "" && runtimeType != RuntimeTypeOpenClaw {
		row, readErr := s.management.sess.SQL().QueryRowContext(ctx, `SELECT target_image_ref FROM runtime_rollouts WHERE runtime_type=? AND status='finished' ORDER BY id DESC LIMIT 1`, runtimeType)
		if readErr != nil {
			return false, readErr
		}
		if readErr = row.Scan(&approvedImage); readErr != nil && readErr != sql.ErrNoRows {
			return false, readErr
		}
		reported, readErr := s.podRepo.List(ctx, runtimeType)
		if readErr != nil {
			return false, readErr
		}
		serving, readErr := s.verifiedOrdinaryRuntimePods(ctx, runtimeType, reported)
		if readErr != nil {
			return false, readErr
		}
		for _, pod := range serving {
			if pod.SchedulingPaused || pod.Draining || (approvedImage != "" && !runtimePodMatchesImage(pod, approvedImage)) {
				continue
			}
			// Quarantined-only slots do not prove a serving pool. Keep the
			// existing compatible cold-capacity recovery for those hosts.
			if free, capacityErr := s.allocatableGatewaySlots(ctx, pod); capacityErr == nil && (free > 0 || pod.UsedSlots >= pod.Capacity) {
				return false, nil
			}
		}
	}
	args := make([]any, 0, len(ids)+1)
	args = append(args, runtimeType)
	for _, id := range ids {
		args = append(args, id)
	}
	rows, err := s.management.sess.SQL().QueryContext(ctx, `SELECT request_json FROM runtime_management_operations WHERE kind='scale_in' AND status='completed' AND JSON_EXTRACT(request_json,'$.automatic_idle')=true AND JSON_UNQUOTE(JSON_EXTRACT(request_json,'$.runtime_type'))=? AND id IN (`+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+`) ORDER BY created_at DESC`, args...)
	if err != nil {
		return false, err
	}
	journals := map[string]runtimePodScaleInJournal{}
	for rows.Next() {
		var raw string
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var j runtimePodScaleInJournal
		if err = json.Unmarshal([]byte(raw), &j); err != nil {
			break
		}
		key := j.Target.Namespace + "/" + j.Target.DeploymentName
		if source != "" && key != source {
			continue
		}
		if _, seen := journals[key]; !seen {
			journals[key] = j
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return false, err
	}
	sourceStorage := ""
	if s.autoStopRepo != nil {
		sourceStorage, err = s.autoStopRepo.PreviousIdleStorage(ctx, instance.ID)
		if err != nil {
			return false, err
		}
	}
	candidates := []models.RuntimePod{}
	for _, j := range journals {
		if !j.AutomaticIdle || j.Phase != "finished" || j.Plan.Replicas != 0 || j.Snapshot == nil || j.Target.Namespace != s.runtimeNamespace {
			continue
		}
		if sourceStorage != "" && !runtimeWorkspaceStorageCompatible(sourceStorage, j.Snapshot.WorkspaceStorageIdentity) {
			continue
		}
		image := j.Snapshot.Option.ImageRef
		digest := imageDigestFromReference(image)
		candidate := models.RuntimePod{RuntimeType: runtimeType, Namespace: j.Target.Namespace, DeploymentName: j.Target.DeploymentName, ImageRef: image, ImageDigest: &digest}
		if sourceImage != "" && !runtimePodMatchesImage(candidate, sourceImage) {
			continue
		}
		candidates = append(candidates, candidate)
	}
	if runtimeType == RuntimeTypeOpenClaw && s.upgrade != nil {
		candidates, err = s.upgrade.filterUpgradePlacementWithInventory(ctx, instance.ID, candidates, []models.RuntimePod{})
		if err != nil {
			return false, err
		}
	}
	if source == "" && runtimeType != RuntimeTypeOpenClaw {
		compatible := candidates[:0]
		var contract *k8s.RuntimePodScaleSnapshot
		for _, candidate := range candidates {
			if approvedImage != "" && !runtimePodMatchesImage(candidate, approvedImage) {
				continue
			}
			snapshot := journals[candidate.Namespace+"/"+candidate.DeploymentName].Snapshot
			if contract != nil && !idleRuntimeContractMatches(contract, snapshot) {
				return false, fmt.Errorf("sleeping %s pools have conflicting native contracts; current default proof required", runtimeType)
			}
			contract = snapshot
			compatible = append(compatible, candidate)
		}
		candidates = compatible
	}
	sort.Slice(candidates, func(i, j int) bool { return candidates[i].DeploymentName < candidates[j].DeploymentName })
	for _, candidate := range candidates {
		j := journals[candidate.Namespace+"/"+candidate.DeploymentName]
		// Serialize against upgrade/maintenance admission and automatic scale-in.
		err = s.management.sess.TxContext(ctx, func(tx db.Session) error {
			if err := repository.LockRuntimeManagementAdmission(ctx, tx); err != nil {
				return err
			}
			if err := s.management.scaleInAdmission(ctx, tx); err != nil {
				return err
			}
			if err := repository.CheckRuntimePodDeletionIntent(ctx, tx, j.Target.Namespace, j.Target.DeploymentName); err != nil {
				return err
			}
			return wake.WakeIdleRuntimePool(ctx, j.Target, j.Snapshot)
		}, nil)
		if err == nil {
			return true, nil
		}
		if errors.Is(err, k8s.ErrRuntimeIdlePoolAlreadyActive) {
			// Kubernetes readiness precedes authenticated CM registration.
			// Wait for this pool unless its verified registered capacity is full;
			// otherwise the next tick would wake an unrelated sleeping pool.
			reported, readErr := s.podRepo.List(ctx, runtimeType)
			if readErr != nil {
				return false, readErr
			}
			verified, readErr := s.verifiedOrdinaryRuntimePods(ctx, runtimeType, reported)
			if readErr != nil {
				return false, readErr
			}
			registered := false
			inventory, readErr := s.RuntimeDeploymentPods(ctx, runtimeType)
			if readErr != nil {
				return false, readErr
			}
			// Readiness/admission filters deliberately exclude paused registrations.
			// Check current identity separately so an unusable registered Pod cannot
			// indefinitely prevent restoring a different compatible sleeping pool.
			for _, pod := range reported {
				if pod.Namespace != candidate.Namespace || pod.DeploymentName != candidate.DeploymentName {
					continue
				}
				for _, live := range inventory {
					if stringValue(pod.PodUID) != "" && ordinaryRuntimeIdentityMatches(pod, live) {
						registered = true
						break
					}
				}
			}
			for _, pod := range verified {
				if pod.Namespace != candidate.Namespace || pod.DeploymentName != candidate.DeploymentName || pod.SchedulingPaused || pod.Draining {
					continue
				}
				free, capacityErr := s.allocatableGatewaySlots(ctx, pod)
				if capacityErr != nil {
					return false, fmt.Errorf("Runtime pool %s physical capacity unavailable: %w", candidate.DeploymentName, capacityErr)
				}
				if free > 0 {
					return true, nil
				}
			}
			if !registered {
				return true, nil
			}
			err = nil
			continue
		}
	}
	if len(candidates) > 0 {
		return false, err
	}
	return false, nil
}

// Concrete source ownership outranks a later default image for an existing home.
func (s *RuntimeScheduler) idleRuntimeSource(ctx context.Context, instanceID int, runtimeType string) (string, string, error) {
	if s.bindingRepo != nil {
		binding, err := s.bindingRepo.GetByInstanceID(ctx, instanceID)
		if err != nil {
			return "", "", err
		}
		if binding != nil {
			pod, err := s.podRepo.GetByID(ctx, binding.RuntimePodID)
			if err != nil {
				return "", "", err
			}
			if pod != nil && pod.Namespace == s.runtimeNamespace && pod.RuntimeType == runtimeType {
				return pod.Namespace + "/" + pod.DeploymentName, pod.ImageRef, nil
			}
		}
	}
	row, err := s.management.sess.SQL().QueryRowContext(ctx, `SELECT p.namespace,p.deployment_name,p.image_ref FROM instance_idle_stop_operations s JOIN runtime_pods p ON p.id=s.runtime_pod_id WHERE s.instance_id=? AND s.phase='completed' AND p.namespace=? AND p.runtime_type=? ORDER BY s.created_at DESC LIMIT 1`, instanceID, s.runtimeNamespace, runtimeType)
	if err != nil {
		return "", "", err
	}
	var namespace, deployment, image string
	if err = row.Scan(&namespace, &deployment, &image); err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", err
	}
	return namespace + "/" + deployment, image, nil
}

func idleRuntimeContractMatches(a, b *k8s.RuntimePodScaleSnapshot) bool {
	return a.Option.ImageRef == b.Option.ImageRef && a.Option.SlotCount == b.Option.SlotCount &&
		a.WorkspaceStorageIdentity == b.WorkspaceStorageIdentity &&
		a.Option.PodResources == b.Option.PodResources && a.Option.CoordinatorResources == b.Option.CoordinatorResources && a.Option.SlotResources == b.Option.SlotResources &&
		apiequality.Semantic.DeepEqual(a.Template.Spec, b.Template.Spec) &&
		a.Template.Labels["clawmanager.io/runtime-protocol"] == b.Template.Labels["clawmanager.io/runtime-protocol"] &&
		a.Template.Labels["clawmanager.io/runtime-security"] == b.Template.Labels["clawmanager.io/runtime-security"] &&
		a.Template.Labels["clawmanager.io/sandbox-profile"] == b.Template.Labels["clawmanager.io/sandbox-profile"]
}
