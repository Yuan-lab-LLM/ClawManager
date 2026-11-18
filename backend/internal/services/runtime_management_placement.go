package services

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"sort"
)

// This constraint survives the source Pod's removal and scheduler replacement.
// It contains no endpoint credentials and is created only by source preflight.
type RuntimeEvictionPlacement struct {
	RuntimeType     string   `json:"runtime_type"`
	Namespace       string   `json:"namespace"`
	DeploymentName  string   `json:"deployment_name"`
	ImageRef        string   `json:"image_ref"`
	ImageDigest     string   `json:"image_digest"`
	StorageIdentity string   `json:"storage_identity"`
	NativeVersion   string   `json:"native_version,omitempty"`
	AgentProtocol   string   `json:"agent_protocol,omitempty"`
	SessionStore    string   `json:"session_store,omitempty"`
	Capabilities    []string `json:"capabilities,omitempty"`
	CPURequest      int64    `json:"cpu_request"`
	MemoryRequest   int64    `json:"memory_request"`
	CPULimit        int64    `json:"cpu_limit"`
	MemoryLimit     int64    `json:"memory_limit"`
}

func runtimeEvictionPlacement(source models.RuntimePod, slot models.RuntimeSlot) *RuntimeEvictionPlacement {
	capabilities := source.Capabilities()
	sort.Strings(capabilities)
	return &RuntimeEvictionPlacement{RuntimeType: source.RuntimeType, Namespace: source.Namespace, DeploymentName: source.DeploymentName, ImageRef: source.ImageRef, ImageDigest: stringValue(source.ImageDigest), StorageIdentity: source.WorkspaceStorageIdentity, NativeVersion: stringValue(source.OpenClawVersion), AgentProtocol: stringValue(source.AgentProtocolVersion), SessionStore: stringValue(source.SessionStore), Capabilities: capabilities, CPURequest: *slot.CPUMillisRequest, MemoryRequest: *slot.MemoryBytesRequest, CPULimit: *slot.CPUMillisLimit, MemoryLimit: *slot.MemoryBytesLimit}
}

func runtimeEvictionPlacementMatches(current, confirmed *RuntimeEvictionPlacement) bool {
	if current == nil || confirmed == nil {
		return false
	}
	a, b := *current, *confirmed
	// Old queued operations captured only deployment/image/storage/resources.
	// Their placement still cannot cross a Deployment without the new proof.
	if b.NativeVersion == "" && b.AgentProtocol == "" && b.SessionStore == "" && len(b.Capabilities) == 0 {
		a.NativeVersion = ""
		a.AgentProtocol = ""
		a.SessionStore = ""
		a.Capabilities = nil
		b.Capabilities = nil
	} else {
		a.Capabilities = append([]string{}, a.Capabilities...)
		b.Capabilities = append([]string{}, b.Capabilities...)
		sort.Strings(a.Capabilities)
		sort.Strings(b.Capabilities)
	}
	return reflect.DeepEqual(a, b)
}

// Applies at the shared port reservation boundary, including port-conflict
// retries. Ordinary new instances have no force policy and retain their path.
func (s *RuntimeManagementService) forceEvictionExcludedPorts(ctx context.Context, instance models.Instance, pod models.RuntimePod, excluded map[int]struct{}) (map[int]struct{}, error) {
	if s.sess == nil {
		return excluded, nil
	}
	var raw string
	row, err := s.sess.SQL().QueryRowContext(ctx, `SELECT items_json FROM runtime_management_operations WHERE kind='eviction' AND JSON_CONTAINS(items_json,JSON_OBJECT('instance_id',?,'source_generation',?)) ORDER BY created_at DESC LIMIT 1`, instance.ID, instance.RuntimeGeneration-1)
	if err == nil {
		err = row.Scan(&raw)
	}
	if errors.Is(err, sql.ErrNoRows) {
		return excluded, nil
	}
	if err != nil {
		return nil, fmt.Errorf("%w: load eviction placement: %v", errRuntimeControlPlanePending, err)
	}
	var items []RuntimeSchedulingItem
	if err = json.Unmarshal([]byte(raw), &items); err != nil {
		return nil, fmt.Errorf("%w: decode eviction placement: %v", errRuntimeControlPlanePending, err)
	}
	var policy *RuntimeEvictionPlacement
	for _, item := range items {
		if item.InstanceID == instance.ID && item.SourceGeneration+1 == instance.RuntimeGeneration {
			policy = item.SourcePlacement
			break
		}
	}
	if policy == nil {
		return excluded, nil
	}
	inventory, err := s.scheduler.RuntimeDeploymentPods(ctx, policy.RuntimeType)
	if err != nil {
		return nil, fmt.Errorf("%w: load eviction target: %v", errRuntimeControlPlanePending, err)
	}
	source := models.RuntimePod{RuntimeType: policy.RuntimeType, Namespace: policy.Namespace, DeploymentName: policy.DeploymentName, ImageRef: policy.ImageRef, ImageDigest: &policy.ImageDigest, WorkspaceStorageIdentity: policy.StorageIdentity}
	capabilities, _ := json.Marshal(policy.Capabilities)
	source.OpenClawVersion = stringPtrOrNil(policy.NativeVersion)
	source.AgentProtocolVersion = stringPtrOrNil(policy.AgentProtocol)
	source.SessionStore = stringPtrOrNil(policy.SessionStore)
	source.CapabilitiesJSON = stringPtrOrNil(string(capabilities))
	required := models.RuntimeSlot{CPUMillisRequest: &policy.CPURequest, MemoryBytesRequest: &policy.MemoryRequest, CPUMillisLimit: &policy.CPULimit, MemoryBytesLimit: &policy.MemoryLimit}
	for _, target := range inventory {
		if target.ID != pod.ID {
			continue
		}
		// The caller already owns one SQL admission claim. Capacity/port occupancy
		// is checked by the reservation repository; do not count our own claim twice.
		target.UsedSlots = 0
		if !target.KubernetesReady || target.KubernetesPodUID == nil || stringValue(pod.PodUID) != *target.KubernetesPodUID || validateRuntimeManagementTarget(source, target) != nil || runtimeManagementResourceBudget(target) != nil {
			break
		}
		blocked := make(map[int]struct{}, len(excluded)+target.Capacity)
		for port := range excluded {
			blocked[port] = struct{}{}
		}
		for index := 1; index <= target.Capacity; index++ {
			if !runtimeManagementSlotReady(target, index) || !runtimeManagementSlotCanFit(target, index, required, s.scheduler.gatewayPortStart) {
				blocked[s.scheduler.gatewayPortStart+(index-1)*RuntimeGatewayPortBlockSize(target.RuntimeType)] = struct{}{}
			}
		}
		return blocked, nil
	}
	return nil, fmt.Errorf("%w: waiting for a compatible eviction target with equivalent physical resources", errRuntimeGatewayStartPending)
}
