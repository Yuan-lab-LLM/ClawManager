package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"clawreef/internal/models"
)

// These are recorded checkpoint identities, not a fresh native or Kubernetes
// observation. Never serialize the maintenance plan, assignments or raw proofs.
type OpenCodeUpgradeOperationView struct {
	Kind              string                          `json:"kind"`
	RolloutID         int64                           `json:"rollout_id"`
	Stage             string                          `json:"stage"`
	Blocked           bool                            `json:"blocked"`
	BlockedReason     string                          `json:"blocked_reason,omitempty"`
	ProtectionHeld    bool                            `json:"protection_held"`
	ProtectionScope   string                          `json:"protection_scope"`
	Message           string                          `json:"message"`
	ErrorMessage      string                          `json:"error_message,omitempty"`
	ErrorScope        string                          `json:"error_scope,omitempty"`
	TargetControllers []OpenCodeUpgradeControllerView `json:"target_controllers"`
	Targets           []OpenCodeUpgradePodView        `json:"targets"`
	Instances         []OpenCodeUpgradeInstanceView   `json:"instances"`
}
type OpenCodeUpgradeControllerView struct {
	Namespace      string `json:"namespace"`
	DeploymentName string `json:"deployment_name"`
	DeploymentUID  string `json:"deployment_uid"`
	ImageRef       string `json:"image_ref"`
}
type OpenCodeUpgradePodView struct {
	RuntimePodID   int64  `json:"runtime_pod_id"`
	Namespace      string `json:"namespace"`
	DeploymentName string `json:"deployment_name"`
	PodName        string `json:"pod_name"`
	PodUID         string `json:"pod_uid"`
	ImageRef       string `json:"image_ref"`
}
type OpenCodeUpgradeInstanceView struct {
	InstanceID   int    `json:"instance_id"`
	Generation   int    `json:"generation"`
	State        string `json:"state"`
	HasStopProof bool   `json:"has_stop_proof"`
	HasCapsule   bool   `json:"has_capsule"`
}

// ProtectionHeld describes only the SQL active Runtime-type guard. It does not
// assert fresh verification of per-workspace fences, leases or stop receipts.
func LoadOpenCodeUpgradeOperation(ctx context.Context, repo any, r *models.RuntimeRollout) (*OpenCodeUpgradeOperationView, error) {
	if !isOpenCodeNativeRollout(r) {
		return nil, nil
	}
	store, ok := repo.(rolloutCheckpointStore)
	if !ok {
		return nil, fmt.Errorf("OpenCode checkpoint storage unavailable")
	}
	raw, err := store.LoadRolloutCheckpoint(ctx, r.ID)
	if err != nil {
		return nil, err
	}
	var p openCodeUpgradePlan
	if err = json.Unmarshal([]byte(raw), &p); err != nil {
		return nil, err
	}
	if p.Kind != "opencode-native-preserve" || p.Version != 1 || p.Strategy != RuntimeUpgradeStrategyOpenCodePreserve {
		return nil, fmt.Errorf("invalid OpenCode native checkpoint")
	}
	v := &OpenCodeUpgradeOperationView{Kind: p.Kind, RolloutID: r.ID, Stage: p.Stage, ProtectionHeld: r.Status == "pending" || r.Status == "running", ProtectionScope: "active_runtime_type_guard", Message: "原生维护阶段及逐项状态来自持久记录；此读取不会推进升级。", TargetControllers: []OpenCodeUpgradeControllerView{}, Targets: []OpenCodeUpgradePodView{}, Instances: []OpenCodeUpgradeInstanceView{}}
	v.ErrorMessage = strings.TrimSpace(stringValue(r.ErrorMessage))
	if v.ErrorMessage != "" {
		v.ErrorScope = "last_recorded_controller_error_not_revalidated"
	}
	for _, target := range p.TargetControllers {
		v.TargetControllers = append(v.TargetControllers, OpenCodeUpgradeControllerView{Namespace: target.Namespace, DeploymentName: target.Name, DeploymentUID: target.UID, ImageRef: target.Image})
	}
	for _, pod := range p.Targets {
		v.Targets = append(v.Targets, OpenCodeUpgradePodView{RuntimePodID: pod.ID, Namespace: pod.Namespace, DeploymentName: pod.DeploymentName, PodName: pod.PodName, PodUID: stringValue(pod.PodUID), ImageRef: pod.ImageRef})
	}
	allQuiesced := len(p.Instances) > 0
	for _, item := range p.Instances {
		hasStop := len(item.StopProof) > 0 && strings.TrimSpace(string(item.StopProof)) != "null"
		v.Instances = append(v.Instances, OpenCodeUpgradeInstanceView{InstanceID: item.ID, Generation: item.Generation, State: item.State, HasStopProof: hasStop, HasCapsule: item.Capsule != nil})
		allQuiesced = allQuiesced && item.State == "quiesced" && hasStop && item.Capsule == nil
	}
	// Only classify the observed, deterministic pre-capsule payload refusal.
	// Generic/older errors may be transient, and remain explicitly historical.
	v.Blocked = v.ProtectionHeld && p.Stage == "preserve" && allQuiesced && v.ErrorMessage == "OpenCode native payload must be an immutable root-owned regular file"
	if v.Blocked {
		v.BlockedReason = "native_payload_trust_rejected"
		v.Message = "CM 最后记录了原生兼容性拒绝；实例已有停止证明记录，尚无迁移备份。操作仍受 Runtime 类型保护，请勿重提或清除保护。"
	}
	return v, nil
}
