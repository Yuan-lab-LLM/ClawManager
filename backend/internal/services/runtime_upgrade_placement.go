package services

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"encoding/json"
	"strings"
)

// A retained old pool and a newly committed pool can coexist. An excluded
// instance must not silently restart on the new core without data migration.
func (s *RuntimeUpgradeService) filterUpgradePlacement(ctx context.Context, id int, pods []models.RuntimePod) ([]models.RuntimePod, error) {
	var item models.RuntimeUpgradeItem
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT ui.id FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id WHERE ui.instance_id=? AND r.preflight_id IS NOT NULL AND r.status IN('pending','running','finished','error') ORDER BY r.id DESC LIMIT 1`, id)
	if err != nil {
		return nil, err
	}
	var itemID int64
	if err = row.Scan(&itemID); err != nil && err != sql.ErrNoRows {
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
	var excluded bool
	var rolloutID int64
	row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT r.id,COALESCE(r.source_images_json,'{}'),r.target_image_ref,ui.id IS NULL FROM runtime_rollouts r LEFT JOIN runtime_upgrade_items ui ON ui.rollout_id=r.id AND ui.instance_id=? WHERE r.runtime_type='openclaw' AND r.status='finished' AND r.preflight_id IS NOT NULL AND (ui.id IS NOT NULL OR JSON_CONTAINS(JSON_EXTRACT(r.preflight_json,'$.exclude_instance_ids'),JSON_ARRAY(?))) ORDER BY r.id DESC LIMIT 1`, id, id)
	if err != nil {
		return nil, err
	}
	err = row.Scan(&rolloutID, &sourceJSON, &target, &excluded)
	if err == sql.ErrNoRows {
		// New instances have no format/history affinity. Prefer the newest committed
		// target rather than assigning them to a retained legacy pool.
		row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT id,COALESCE(source_images_json,'{}'),target_image_ref FROM runtime_rollouts WHERE runtime_type='openclaw' AND status='finished' AND preflight_id IS NOT NULL ORDER BY id DESC LIMIT 1`)
		if err != nil {
			return nil, err
		}
		err = row.Scan(&rolloutID, &sourceJSON, &target)
		if err == sql.ErrNoRows {
			return pods, nil
		}
		excluded = false
	}
	if err != nil {
		return nil, err
	}
	var sources map[string]string
	if err = json.Unmarshal([]byte(sourceJSON), &sources); err != nil {
		return nil, err
	}
	result := make([]models.RuntimePod, 0, len(pods))
	for _, pod := range pods {
		isTarget := false
		for source := range sources {
			parts := strings.SplitN(source, "/", 2)
			if len(parts) == 2 && pod.Namespace+"/"+pod.DeploymentName == parts[0]+"/"+runtimeUpgradeTargetDeploymentName(parts[1], rolloutID) {
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
