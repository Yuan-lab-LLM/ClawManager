package services

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
)

// A committed ordinary rollout is the pool's image authority. Static process
// configuration can still name its predecessor after a native CM upgrade.
// Resolve the exact current binding/controller; the security authority then
// independently checks every live coordinator and slot container's digest.
func (s *RuntimeUpgradeService) ManagedPreviewPoolImage(ctx context.Context, id runtimeauth.Identity) (string, string, error) {
	var rt, ns, deployment, podName, podUID string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT i.type,p.namespace,p.deployment_name,p.pod_name,p.pod_uid FROM instances i JOIN instance_runtime_bindings b ON b.instance_id=i.id JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE i.id=? AND i.user_id=? AND i.runtime_generation=? AND b.generation=? AND BINARY p.pod_uid=BINARY ? AND i.instance_mode='lite' AND BINARY i.type=BINARY b.runtime_type AND BINARY b.runtime_type=BINARY p.runtime_type`, id.InstanceID, id.UserID, id.Generation, id.Generation, id.PodUID)
	if err != nil {
		return "", "", err
	}
	if err = row.Scan(&rt, &ns, &deployment, &podName, &podUID); errors.Is(err, sql.ErrNoRows) {
		return "", "", nil
	} else if err != nil {
		return "", "", err
	}
	rollouts, checkpoints, err := runtimeScaleDefaultRollouts(ctx, s.sess, rt)
	if err != nil {
		return "", "", err
	}
	if len(rollouts) == 0 {
		return rt, "", nil
	}
	base := k8s.RuntimeSlotPoolName("", rt)
	if naming, ok := s.deployments.(interface{ DefaultRuntimePoolName(string) string }); ok {
		base = naming.DefaultRuntimePoolName(rt)
	}
	targets, err := runtimeDefaultScaleTargets(ns, rt, base, rollouts, checkpoints)
	if err != nil {
		return "", "", err
	}
	for _, target := range targets {
		if target.Namespace != ns || target.DeploymentName != deployment || target.ImageRef == "" {
			continue
		}
		if !IsImmutableRuntimeImage(target.ImageRef) {
			return "", "", errRuntimeSecurity
		}
		client, ok := s.agent.(*slotRuntimeAgentClient)
		if !ok || client.kube == nil {
			return "", "", errRuntimeSecurity
		}
		resolver, ok := k8s.NewRuntimeDeploymentService(client.kube).(k8s.RuntimePodControllerResolver)
		if !ok {
			return "", "", errRuntimeSecurity
		}
		actual, err := resolver.ResolveRuntimePodController(ctx, ns, podName, podUID)
		if err != nil || actual.RuntimeType != rt || actual.DeploymentName != deployment || target.DeploymentUID != "" && actual.DeploymentUID != target.DeploymentUID {
			return "", "", errRuntimeSecurity
		}
		return rt, target.ImageRef, nil
	}
	return rt, "", nil
}

// A scoped upgrade may activate its frozen target before changing the fleet
// default. Approve only its exact reserved successor and validated migration;
// VerifyManagedPreviewImage still independently checks every live container.
func (s *RuntimeUpgradeService) ManagedPreviewUpgradeImage(ctx context.Context, id runtimeauth.Identity) (string, error) {
	var image, digest, receipt string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT r.target_image_ref,r.target_image_digest,ui.postflight_json FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id JOIN instances i ON i.id=ui.instance_id JOIN instance_runtime_bindings b ON b.instance_id=i.id JOIN runtime_pods p ON p.id=b.runtime_pod_id WHERE i.id=? AND i.user_id=? AND i.runtime_generation=? AND b.generation=? AND ui.source_generation+1=? AND BINARY p.pod_uid=BINARY ? AND BINARY ui.restart_target_pod_uid=BINARY ? AND i.type='openclaw' AND b.runtime_type='openclaw' AND p.runtime_type='openclaw' AND r.runtime_type='openclaw' AND r.preflight_id IS NOT NULL AND r.status IN ('running','finished') AND r.phase IN ('gateway_restart','postflight') AND ui.state IN ('restart_ready','gateway_verified','verified') AND ui.isolated_at IS NULL AND ui.recovery_status IN ('starting','retrying','verified') AND ui.postflight_json IS NOT NULL ORDER BY r.id DESC LIMIT 1`, id.InstanceID, id.UserID, id.Generation, id.Generation, id.Generation, id.PodUID, id.PodUID)
	if err != nil {
		return "", err
	}
	if err = row.Scan(&image, &digest, &receipt); errors.Is(err, sql.ErrNoRows) {
		// A completed scoped migration does not change the fleet default. A
		// later ordinary start may retain its approved image only through a real
		// completed stop of that image and the same owner/workspace. The live
		// container image is still independently checked by the normal guard.
		row, err = s.sess.SQL().QueryRowContext(ctx, `SELECT r.target_image_ref,r.target_image_digest,ui.postflight_json FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON r.id=ui.rollout_id JOIN instances i ON i.id=ui.instance_id JOIN instance_runtime_bindings b ON b.instance_id=i.id JOIN runtime_pods p ON p.id=b.runtime_pod_id JOIN instance_idle_stop_operations stop ON stop.instance_id=i.id AND stop.user_id=i.user_id AND stop.generation=?-1 JOIN runtime_pods source ON source.id=stop.runtime_pod_id AND BINARY source.pod_uid=BINARY stop.pod_uid AND BINARY source.agent_endpoint=BINARY stop.endpoint WHERE i.id=? AND i.user_id=? AND i.runtime_generation=? AND b.generation=? AND BINARY p.pod_uid=BINARY ? AND i.type='openclaw' AND b.runtime_type='openclaw' AND p.runtime_type='openclaw' AND source.runtime_type='openclaw' AND r.runtime_type='openclaw' AND r.preflight_id IS NOT NULL AND r.status='finished' AND r.phase='postflight' AND r.finished_at IS NOT NULL AND ui.state='verified' AND ui.recovery_status='verified' AND ui.isolated_at IS NULL AND ui.source_generation+1<? AND stop.generation>=ui.source_generation+1 AND stop.phase='completed' AND stop.created_at>=r.finished_at AND stop.storage_identity<>'' AND BINARY stop.workspace_path=BINARY b.workspace_path AND BINARY i.workspace_path=BINARY b.workspace_path AND BINARY stop.gateway_id=BINARY CONCAT('gw-',i.id,'-',stop.generation) AND b.gateway_id IN(CONCAT('gw-',i.id,'-',b.generation),CONCAT('pending-',i.id,'-',b.generation)) AND i.status IN('creating','running') AND b.state IN('creating','starting','running') AND BINARY p.image_digest=BINARY r.target_image_digest AND BINARY source.image_digest=BINARY r.target_image_digest AND NOT EXISTS(SELECT 1 FROM runtime_upgrade_items newer JOIN runtime_rollouts nr ON nr.id=newer.rollout_id WHERE newer.instance_id=i.id AND nr.id>r.id AND nr.preflight_id IS NOT NULL AND nr.status IN('pending','running','error','finished')) ORDER BY r.id DESC LIMIT 1`, id.Generation, id.InstanceID, id.UserID, id.Generation, id.Generation, id.PodUID, id.Generation)
		if err != nil {
			return "", err
		}
		if err = row.Scan(&image, &digest, &receipt); errors.Is(err, sql.ErrNoRows) {
			return "", nil
		} else if err != nil {
			return "", err
		}
	} else if err != nil {
		return "", err
	}
	var evidence struct {
		Migration *RuntimeAgentSessionSQLiteMigration `json:"migration"`
	}
	if json.Unmarshal([]byte(receipt), &evidence) != nil || !validSessionSQLiteMigration(evidence.Migration) || evidence.Migration.InstanceID != id.InstanceID || digest == "" || imageDigestFromReference(image) != digest {
		return "", errRuntimeSecurity
	}
	return image, nil
}
