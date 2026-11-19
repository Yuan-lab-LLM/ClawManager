package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"clawreef/internal/backupgate"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
)

const (
	runtimeIdlePoolScanInterval = 10 * time.Second
	runtimeIdlePodGrace         = time.Minute
)

var errRuntimeIdleMinimumCapacity = errors.New("automatic idle scale-in must retain at least one Runtime Pod")

// Keep slot observations fresh while instance placement or startup is slow.
func (s *RuntimeScheduler) runIdleRuntimePools(ctx context.Context) {
	ticker := time.NewTicker(runtimeIdlePoolScanInterval)
	defer ticker.Stop()
	for ctx.Err() == nil {
		s.reconcileIdleRuntimePoolsIfLeader(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *RuntimeScheduler) reconcileIdleRuntimePoolsIfLeader(ctx context.Context) {
	release, err := backupgate.HoldWorkUnit(ctx, "runtime.idle-pools")
	if err != nil {
		if s.management != nil {
			s.management.idlePodSince = nil
		}
		return
	}
	defer release()
	if s.leader != nil && !s.leader.IsLeader(ctx) {
		// Preserve the grace only if the API still confirms our unexpired lease.
		observer, ok := s.leader.(interface{ HoldsLease(context.Context) bool })
		if s.management != nil && (!ok || !observer.HoldsLease(ctx)) {
			s.management.idlePodSince = nil
		}
		return
	}
	err = s.slotAudit.Reconcile(ctx, s)
	if s.management != nil {
		err = errors.Join(err, s.management.reconcileIdleRuntimePools(ctx, time.Now().UTC()))
	}
	if err != nil {
		log.Printf("runtime idle pool reconcile failed: %v", err)
	}
}

func idleRuntimePodEmpty(pod models.RuntimePod, now time.Time) error {
	pod.State = pod.AgentState // Inventory keeps Kubernetes and Agent states separate.
	return runtimeMaintenanceEmptyReport(&pod, stringValue(pod.PodUID), now)
}

// Protect concrete stop identities, not every pool of an unbound Creating row.
// Claims and bindings are rechecked under the admission lock before publication;
// a later start uses the retained pool or expands its capacity.
func (s *RuntimeManagementService) idlePoolDemandAdmission(ctx context.Context, tx db.Session, sources []RuntimePodScaleInSource) error {
	if len(sources) == 0 {
		return nil // A plan can remove only never-scheduled replica identities.
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

func idleUnscheduledReplicaAdmission(ctx context.Context, tx db.Session, journal *runtimePodScaleInJournal) error {
	checkDemand := false
	for _, source := range journal.Protection.SourcePods {
		if !source.Unscheduled {
			continue
		}
		checkDemand = true
		var registered bool
		row, err := tx.SQL().QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM runtime_pods WHERE namespace=? AND pod_name=? AND pod_uid=?)`, journal.Plan.Namespace, source.PodName, source.PodUID)
		if err != nil {
			return err
		}
		if err = row.Scan(&registered); err != nil {
			return err
		}
		if registered {
			return fmt.Errorf("unscheduled replica %s acquired a Runtime registration", source.PodName)
		}
	}
	if checkDemand {
		demand, err := repository.CountRuntimeSlotDemand(ctx, tx, journal.RuntimeType)
		if err != nil {
			return err
		}
		if demand != 0 {
			return fmt.Errorf("%d %s instances are still waiting for Runtime slots", demand, journal.RuntimeType)
		}
	}
	return nil
}

// Run only from the leader's idle-pool loop. A restart or an unavailable
// inventory resets the grace period.
func (s *RuntimeManagementService) reconcileIdleRuntimePools(ctx context.Context, now time.Time) error {
	if s.scheduler.autoStopRepo == nil {
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
		if !idleUnregisteredPendingPod(pod) {
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
		}
		since := s.idlePodSince[uid]
		if since.IsZero() {
			since = now
		}
		seen[uid] = since
	}
	s.idlePodSince = seen
	keys := make([]string, 0, len(groups))
	for key := range groups {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		pods := groups[key]
		// Keep a live serving pool; instance sleep does not require Pod sleep.
		if len(pods) <= 1 {
			continue
		}
		if err := s.scaleInAdmission(ctx, s.sess, pods[0].RuntimeType); err != nil {
			continue
		}
		selected := map[string]bool{}
		for _, pod := range pods {
			uid := stringValue(pod.PodUID)
			if since, ok := seen[uid]; ok && now.Sub(since) >= runtimeIdlePodGrace {
				selected[uid] = true
			}
		}
		if len(selected) == len(pods) {
			for i := len(pods) - 1; i >= 0; i-- {
				if runtimeScaleInFreshPod(pods[i], s.scheduler.heartbeatTimeout) == nil {
					delete(selected, stringValue(pods[i].PodUID))
					break
				}
			}
		}
		if len(selected) == 0 {
			continue
		}
		journal, err := s.idleRuntimeScaleInPlan(ctx, kube, pods, selected)
		if err == nil {
			err = s.idlePoolDemandAdmission(ctx, s.sess, journal.Plan.SourcePods)
		}
		if err == nil {
			err = idleUnscheduledReplicaAdmission(ctx, s.sess, journal)
		}
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

func idleUnregisteredPendingPod(pod models.RuntimePod) bool {
	return pod.State == "pending" && pod.NodeName == nil && pod.ID < 0 && stringValue(pod.PodUID) != ""
}

func (s *RuntimeManagementService) checkIdlePoolServingPod(ctx context.Context, tx db.Session, journal *runtimePodScaleInJournal) error {
	if !journal.AutomaticIdle {
		return nil
	}
	pods := repository.NewRuntimePodRepository(tx)
	cutoff := s.scheduler.schedulingHeartbeatCutoff()
	for _, retained := range journal.Protection.RetainedPods {
		if retained.Unscheduled {
			continue
		}
		pod, err := pods.GetByNamespaceName(ctx, journal.Plan.Namespace, retained.PodName)
		if err != nil {
			return err
		}
		if pod != nil && stringValue(pod.PodUID) == retained.PodUID && pod.DeploymentName == journal.Plan.DeploymentName && pod.State == "ready" && !pod.Draining && !pod.SchedulingPaused && pod.AgentEndpoint != nil && pod.LastSeenAt != nil && !pod.LastSeenAt.Before(cutoff) {
			return nil
		}
	}
	return fmt.Errorf("automatic idle scale-in requires a retained Pod with a fresh Agent report")
}

func (s *RuntimeManagementService) idleRuntimeScaleInPlan(ctx context.Context, kube k8s.RuntimePodScaleInDeploymentService, pods []models.RuntimePod, selected map[string]bool) (*runtimePodScaleInJournal, error) {
	if len(selected) == 0 || len(selected) >= len(pods) {
		return nil, errRuntimeIdleMinimumCapacity
	}
	byUID := map[string]models.RuntimePod{}
	var anchor models.RuntimePod
	for _, pod := range pods {
		uid := stringValue(pod.PodUID)
		byUID[uid] = pod
		if runtimeScaleInFreshPod(pod, s.scheduler.heartbeatTimeout) == nil && (anchor.ID == 0 || selected[stringValue(anchor.PodUID)]) {
			anchor = pod
		}
	}
	if anchor.ID == 0 {
		return nil, errRuntimeIdleMinimumCapacity
	}
	target := k8s.RuntimePodScaleTarget{RuntimeType: anchor.RuntimeType, Namespace: anchor.Namespace, DeploymentName: anchor.DeploymentName, PodName: anchor.PodName, PodUID: stringValue(anchor.PodUID), UpgradeID: anchor.UpgradeID, SourceDeployment: anchor.SourceDeployment, ImageRef: anchor.ImageRef, IdlePool: true}
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
	journal := &runtimePodScaleInJournal{AutomaticIdle: true, RuntimePodScaleInRequest: RuntimePodScaleInRequest{RuntimeType: target.RuntimeType}, Target: target, Snapshot: snapshot, Phase: "pending"}
	journal.Plan = RuntimePodScaleInPreflight{Allowed: true, RuntimeType: target.RuntimeType, Namespace: target.Namespace, DeploymentName: target.DeploymentName, DeploymentUID: snapshot.Option.DeploymentUID, PreviousReplicas: snapshot.Option.Replicas, SourcePods: []RuntimePodScaleInSource{}, Items: []RuntimeSchedulingItem{}}
	journal.Protection = k8s.RuntimePodScaleInProtection{Namespace: target.Namespace, DeploymentName: target.DeploymentName, DeploymentUID: snapshot.Option.DeploymentUID, PreviousReplicas: snapshot.Option.Replicas}
	for _, identity := range native {
		if identity.Unscheduled {
			pod, ok := byUID[identity.PodUID]
			if !ok || pod.PodName != identity.PodName || !idleUnregisteredPendingPod(pod) {
				return nil, fmt.Errorf("unscheduled Runtime replica set is not safe to reclaim")
			}
			if len(journal.Protection.SourcePods) >= 10 {
				journal.Protection.RetainedPods = append(journal.Protection.RetainedPods, identity)
				continue
			}
			journal.Protection.SourcePods = append(journal.Protection.SourcePods, identity)
		}
	}
	for _, identity := range native {
		if identity.Unscheduled {
			continue
		}
		pod, ok := byUID[identity.PodUID]
		if !ok || pod.PodName != identity.PodName {
			return nil, fmt.Errorf("idle pool sibling identity changed")
		}
		if identity.PodUID != target.PodUID && selected[identity.PodUID] && len(journal.Protection.SourcePods) < 10 {
			if err = runtimeScaleInFreshPod(pod, s.scheduler.heartbeatTimeout); err != nil {
				return nil, err
			}
			if err = idleRuntimePodEmpty(pod, time.Now().UTC()); err != nil {
				return nil, err
			}
			journal.Plan.SourcePods = append(journal.Plan.SourcePods, RuntimePodScaleInSource{ID: pod.ID, PodName: pod.PodName, PodUID: identity.PodUID})
			journal.Protection.SourcePods = append(journal.Protection.SourcePods, identity)
		} else {
			journal.Protection.RetainedPods = append(journal.Protection.RetainedPods, identity)
		}
	}
	journal.PodCount = len(journal.Protection.SourcePods)
	if journal.PodCount == 0 || len(journal.Protection.RetainedPods) == 0 {
		return nil, errRuntimeIdleMinimumCapacity
	}
	journal.Plan.RemovedPods = journal.PodCount
	journal.Plan.Replicas = int32(len(journal.Protection.RetainedPods))
	journal.Protection.Replicas = journal.Plan.Replicas
	return journal, nil
}
