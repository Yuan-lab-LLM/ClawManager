package services

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// A retained old pool and a newly committed pool can coexist. An excluded
// instance must not silently restart on the new core without data migration.
func (s *RuntimeUpgradeService) filterUpgradePlacement(ctx context.Context, id int, pods []models.RuntimePod) ([]models.RuntimePod, error) {
	return s.filterUpgradePlacementWithInventory(ctx, id, pods, nil)
}

// liveInventory may be supplied only by the current planning call. It is never
// cached across mutations or requests.
func (s *RuntimeUpgradeService) filterUpgradePlacementWithInventory(ctx context.Context, id int, pods, liveInventory []models.RuntimePod) ([]models.RuntimePod, error) {
	var item models.RuntimeUpgradeItem
	var latestStatus string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT ui.id,r.status FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.instance_id=? AND r.preflight_id IS NOT NULL AND r.status IN('pending','running','finished','error') ORDER BY r.id DESC LIMIT 1`, id)
	if err != nil {
		return nil, err
	}
	var itemID int64
	if err = row.Scan(&itemID, &latestStatus); err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == nil {
		if err = s.sess.Collection("runtime_upgrade_items").Find("id", itemID).One(&item); err != nil {
			return nil, err
		}
		if item.IsolatedAt != nil {
			result := []models.RuntimePod{}
			for _, p := range pods {
				if item.RuntimePodID != nil && p.ID == *item.RuntimePodID && persistedSourcePodMatches(p, item) && runtimePodMatchesImage(p, "retained@"+stringValue(item.SourceImageDigest)) {
					result = append(result, p)
				}
			}
			return result, nil
		}
	}
	var sourceJSON string
	var target string
	var targetDigest string
	var excluded bool
	var rolloutID int64
	var inPlace bool
	row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT r.id,COALESCE(r.source_images_json,'{}'),r.target_image_ref,COALESCE(r.target_image_digest,''),ui.id IS NULL FROM runtime_rollouts r LEFT JOIN runtime_upgrade_items ui ON ui.rollout_id=r.id AND ui.instance_id=? WHERE r.runtime_type='openclaw' AND r.status='finished' AND r.preflight_id IS NOT NULL AND (ui.id IS NOT NULL OR JSON_CONTAINS(JSON_EXTRACT(r.preflight_json,'$.exclude_instance_ids'),JSON_ARRAY(?))) ORDER BY r.id DESC LIMIT 1`, id, id)
	if err != nil {
		return nil, err
	}
	err = row.Scan(&rolloutID, &sourceJSON, &target, &targetDigest, &excluded)
	if err == sql.ErrNoRows {
		// A successful empty-pool switch supersedes the default for NEW instances,
		// not the per-instance migration/isolation contract handled above.
		current, loadErr := s.defaultPlacementRollout(ctx)
		if loadErr != nil {
			return nil, loadErr
		}
		if current == nil {
			return pods, nil
		}
		rolloutID, sourceJSON, target = current.ID, stringValue(current.SourceImagesJSON), current.TargetImageRef
		targetDigest = stringValue(current.TargetImageDigest)
		inPlace = current.PreflightID == nil
		var created time.Time
		row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT created_at FROM instances WHERE id=?`, id)
		if err != nil {
			return nil, err
		}
		if err = row.Scan(&created); err != nil {
			return nil, fmt.Errorf("verify instance birth before default placement: %w", err)
		}
		if created.IsZero() || current.StartedAt == nil {
			return nil, fmt.Errorf("%w: default placement has no verified instance/rollout birth boundary", errRuntimeControlPlanePending)
		}
		var emptyStarted *time.Time
		emptyRecord := current
		emptyBarrier := isEmptyOpenClawPoolReset(current)
		if isEmptyOpenClawPoolReset(current) {
			emptyStarted = current.StartedAt
		} else {
			emptyRecord = &models.RuntimeRollout{RuntimeType: RuntimeTypeOpenClaw, Phase: RuntimeUpgradePhaseEmptyPoolReset}
			row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT started_at,preflight_json FROM runtime_rollouts WHERE runtime_type='openclaw' AND status='finished' AND phase='empty_pool_reset' AND id<=? ORDER BY id DESC LIMIT 1`, current.ID)
			if err != nil {
				return nil, err
			}
			err = row.Scan(&emptyStarted, &emptyRecord.PreflightJSON)
			emptyBarrier = err == nil
			if err != nil && err != sql.ErrNoRows {
				return nil, err
			}
		}
		if emptyBarrier {
			if emptyStarted == nil || created.IsZero() || (created.Before(*emptyStarted) && !s.poolSwitchWaitingInstance(ctx, emptyRecord, id)) {
				return nil, fmt.Errorf("%w: existing instance data has no compatible placement after pool switch", errRuntimeControlPlanePending)
			}
		}
		if !inPlace && created.Before(*current.StartedAt) {
			// No item is not proof of no old data: an unselected existing instance
			// stays on a frozen compatible source, never the migrated target.
			var sources map[string]string
			if err := json.Unmarshal([]byte(sourceJSON), &sources); err != nil {
				return nil, err
			}
			result := []models.RuntimePod{}
			for _, p := range pods {
				if image := sources[p.Namespace+"/"+p.DeploymentName]; image != "" && runtimePodMatchesImage(p, image) {
					result = append(result, p)
				}
			}
			return result, nil
		}
		if inPlace && !isEmptyOpenClawPoolReset(current) && sourceJSON == "{}" {
			return s.legacyDefaultPlacement(ctx, current, pods)
		}
		err = nil
		excluded = false
	}
	if err != nil {
		return nil, err
	}
	if targetDigest == "" {
		targetDigest = imageDigestFromReference(target)
	}
	targetApproved := func(pod models.RuntimePod) bool {
		return targetDigest != "" && runtimePodMatchesImage(pod, target) && stringValue(pod.ImageDigest) == targetDigest
	}
	var sources map[string]string
	if err = json.Unmarshal([]byte(sourceJSON), &sources); err != nil {
		return nil, err
	}
	if len(sources) == 0 || strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("runtime placement has no frozen target inventory")
	}
	for source := range sources {
		parts := strings.SplitN(source, "/", 2)
		if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
			return nil, fmt.Errorf("invalid runtime placement deployment %q", source)
		}
	}
	result := make([]models.RuntimePod, 0, len(pods))
	approvedSources := sources
	if excluded {
		approvedSources = map[string]string{}
		if s.bindings != nil && s.pods != nil {
			binding, e := s.bindings.GetByInstanceID(ctx, id)
			if e != nil {
				return nil, e
			}
			if binding != nil {
				pod, e := s.pods.GetByID(ctx, binding.RuntimePodID)
				if e != nil {
					return nil, e
				}
				if pod != nil {
					key := pod.Namespace + "/" + pod.DeploymentName
					if image, ok := sources[key]; ok {
						approvedSources[key] = image
					}
				}
			}
		}
		if len(approvedSources) == 0 {
			unique := ""
			unambiguous := true
			for _, image := range sources {
				if unique == "" {
					unique = image
				}
				if unique != image {
					unambiguous = false
				}
			}
			if unambiguous && imageDigestFromReference(unique) != "" {
				approvedSources = sources
			}
		}
	}
	isTargetPool := func(pod models.RuntimePod) bool {
		isTarget := false
		for source := range sources {
			parts := strings.SplitN(source, "/", 2)
			name := parts[1]
			if !inPlace {
				name = runtimeUpgradeTargetDeploymentName(name, rolloutID)
			}
			if pod.Namespace+"/"+pod.DeploymentName == parts[0]+"/"+name {
				isTarget = true
			}
		}
		return isTarget
	}
	// An exclusion approves only its captured source binary. Merely being outside
	// the new Deployment must never admit a changed core.
	sourceApproved := func(pod models.RuntimePod) bool {
		for source, image := range approvedSources {
			parts := strings.SplitN(source, "/", 2)
			if len(parts) == 2 && parts[0] == pod.Namespace && runtimePodMatchesImage(pod, image) {
				return true
			}
		}
		return false
	}
	oldAllowed := func(pod models.RuntimePod) bool {
		if excluded {
			_, canonical := approvedSources[pod.Namespace+"/"+pod.DeploymentName]
			return canonical && !isTargetPool(pod) && sourceApproved(pod)
		}
		return isTargetPool(pod) && runtimePodMatchesImage(pod, target)
	}
	// Unfinished or unresolved recovery never gains a broader placement policy.
	mayUseCopies := itemID == 0 || (latestStatus == "finished" && (item.State == "verified" || item.State == "gateway_verified"))
	if !excluded && targetDigest == "" {
		mayUseCopies = false
	}
	if mayUseCopies && liveInventory == nil && s.deployments != nil {
		liveInventory, err = s.deployments.RuntimeDeploymentPods(ctx, RuntimeTypeOpenClaw)
		if err != nil {
			return nil, err
		}
	}
	witnesses := []models.RuntimePod{}
	confirmedIdentity := func(pod models.RuntimePod) bool {
		uid := stringValue(pod.PodUID)
		return pod.ID > 0 && uid != "" && stringValue(pod.RegisteredPodUID) == uid && stringValue(pod.KubernetesPodUID) == uid
	}
	for _, pod := range liveInventory {
		canonical := isTargetPool(pod)
		if excluded {
			_, canonical = approvedSources[pod.Namespace+"/"+pod.DeploymentName]
		}
		if canonical && confirmedIdentity(pod) && oldAllowed(pod) && (excluded || targetApproved(pod)) && !runtimePodIsUpgradeLab(pod) {
			witnesses = append(witnesses, pod)
		}
	}
	// A later completed verified recovery can have removed the original target
	// pool. Its currently bound approved binary is also an authenticated witness.
	if mayUseCopies && !excluded && itemID > 0 && item.RolloutID == rolloutID && s.bindings != nil {
		binding, e := s.bindings.GetByInstanceID(ctx, id)
		if e != nil {
			return nil, e
		}
		if binding != nil && binding.State == "running" {
			for _, pod := range liveInventory {
				if pod.ID == binding.RuntimePodID && confirmedIdentity(pod) && targetApproved(pod) && !runtimePodIsUpgradeLab(pod) {
					witnesses = append(witnesses, pod)
				}
			}
		}
	}
	for _, pod := range pods {
		allowed := oldAllowed(pod)
		if !allowed && mayUseCopies && !runtimePodIsUpgradeLab(pod) {
			for _, live := range liveInventory {
				if live.ID != pod.ID || !confirmedIdentity(live) || stringValue(live.PodUID) != stringValue(pod.PodUID) {
					continue
				}
				if excluded && !sourceApproved(live) {
					continue
				}
				if excluded {
					immutable := false
					for _, image := range approvedSources {
						if imageDigestFromReference(image) != "" && runtimePodMatchesImage(live, image) {
							immutable = true
						}
					}
					if !immutable {
						continue
					}
				}
				if !excluded && !targetApproved(live) {
					continue
				}
				for _, witness := range witnesses {
					if runtimeManagementCompatibleImage(witness, live) == nil {
						allowed = true
						break
					}
				}
				break
			}
		}
		if allowed {
			result = append(result, pod)
		}
	}
	return result, nil
}

// Recovery/lab runs are not a new global default. Read existing records rather
// than rewriting upgrade history, so already-finished switches are repaired too.
func (s *RuntimeUpgradeService) defaultPlacementRollout(ctx context.Context) (*models.RuntimeRollout, error) {
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT id,COALESCE(source_images_json,'{}'),target_image_ref,COALESCE(target_image_digest,''),phase,started_at,preflight_id,preflight_json
	 FROM runtime_rollouts WHERE runtime_type='openclaw' AND status='finished'
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(preflight_json,'$.recovery_mode')),'')=''
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(preflight_json,'$.upgrade_lab_run_id')),'null')='null'
	 ORDER BY id DESC LIMIT 1`)
	if err != nil {
		return nil, err
	}
	r := &models.RuntimeRollout{RuntimeType: RuntimeTypeOpenClaw}
	var source string
	if err = row.Scan(&r.ID, &source, &r.TargetImageRef, &r.TargetImageDigest, &r.Phase, &r.StartedAt, &r.PreflightID, &r.PreflightJSON); err == sql.ErrNoRows {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	r.SourceImagesJSON = &source
	return r, nil
}

// Older ordinary rollouts did not save source_images_json. They still replace
// the ordinary pool's image. Recover their scope from the last committed pool,
// when present, and intersect it with the scheduler's live verified inventory.
// Never mutate history, trust a registration alone, or relax an instance fence.
func (s *RuntimeUpgradeService) legacyDefaultPlacement(ctx context.Context, r *models.RuntimeRollout, pods []models.RuntimePod) ([]models.RuntimePod, error) {
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT id,source_images_json,preflight_id FROM runtime_rollouts
	 WHERE runtime_type='openclaw' AND status='finished' AND id<? AND source_images_json IS NOT NULL
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(preflight_json,'$.recovery_mode')),'')=''
	 AND COALESCE(JSON_UNQUOTE(JSON_EXTRACT(preflight_json,'$.upgrade_lab_run_id')),'null')='null'
	 ORDER BY id DESC LIMIT 1`, r.ID)
	if err != nil {
		return nil, err
	}
	var previous int64
	var raw string
	var preflight *string
	err = row.Scan(&previous, &raw, &preflight)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	var scope map[string]string
	if err == nil {
		var sources map[string]string
		if err := json.Unmarshal([]byte(raw), &sources); err != nil {
			return nil, err
		}
		if len(sources) == 0 {
			return nil, fmt.Errorf("%w: prior default has empty deployment scope", errRuntimeControlPlanePending)
		}
		scope = make(map[string]string, len(sources))
		for key := range sources {
			parts := strings.SplitN(key, "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, fmt.Errorf("invalid prior placement scope")
			}
			if preflight != nil {
				parts[1] = runtimeUpgradeTargetDeploymentName(parts[1], previous)
			}
			scope[parts[0]+"/"+parts[1]] = r.TargetImageRef
		}
	}
	result := []models.RuntimePod{}
	for _, pod := range pods {
		if !pod.KubernetesReady || stringValue(pod.KubernetesPodUID) == "" || !runtimePodEligibleForOrdinaryScheduling(pod) {
			continue
		}
		if scope != nil && scope[pod.Namespace+"/"+pod.DeploymentName] == "" {
			continue
		}
		if runtimePodMatchesImage(pod, r.TargetImageRef) {
			result = append(result, pod)
		}
	}
	return result, nil
}

func filterPlacementTarget(pods []models.RuntimePod, rolloutID int64, sourceJSON, target string, excluded, inPlace bool) ([]models.RuntimePod, error) {
	var sources map[string]string
	if err := json.Unmarshal([]byte(sourceJSON), &sources); err != nil {
		return nil, err
	}
	if len(sources) == 0 || strings.TrimSpace(target) == "" {
		return nil, fmt.Errorf("runtime placement has no frozen target inventory")
	}
	result := make([]models.RuntimePod, 0, len(pods))
	for _, pod := range pods {
		isTarget := false
		for source := range sources {
			parts := strings.SplitN(source, "/", 2)
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				return nil, fmt.Errorf("invalid runtime placement deployment %q", source)
			}
			name := parts[1]
			if !inPlace {
				name = runtimeUpgradeTargetDeploymentName(name, rolloutID)
			}
			if pod.Namespace+"/"+pod.DeploymentName == parts[0]+"/"+name {
				isTarget = true
			}
		}
		allowed := isTarget && runtimePodMatchesImage(pod, target)
		if excluded {
			// Sources not involved in this scoped rollout were never changed; exclude
			// only its target pool, including same-image repair clones.
			allowed = !isTarget
		}
		if allowed {
			result = append(result, pod)
		}
	}
	return result, nil
}
