package k8s

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
)

type RuntimeContainerStopIntent struct {
	Version          int    `json:"version"`
	OperationID      string `json:"operation_id"`
	PodUID           string `json:"pod_uid"`
	Slot             string `json:"slot"`
	GatewayID        string `json:"gateway_id"`
	InstanceID       int    `json:"instance_id"`
	UserID           int    `json:"user_id"`
	Generation       int    `json:"generation"`
	WorkerEpoch      string `json:"worker_epoch"`
	AssignmentDigest string `json:"assignment_digest"`
	Graceful         bool   `json:"graceful"`
	Kind             string `json:"kind,omitempty"`
}

func (s *RuntimeAssignmentService) ReadContainerStop(ctx context.Context, endpoint, gatewayID string) (RuntimeAssignment, string, *RuntimeContainerStopIntent, json.RawMessage, error) {
	var empty RuntimeAssignment
	if err := s.validService(); err != nil {
		return empty, "", nil, nil, err
	}
	id, gen, err := parseRuntimeAssignmentGatewayID(gatewayID)
	if err != nil {
		return empty, "", nil, nil, err
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
	if err != nil {
		return empty, "", nil, nil, err
	}
	a, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return empty, "", nil, nil, err
	}
	if a.InstanceID != id || a.Generation != gen || a.Endpoint != endpoint || a.GatewayID != gatewayID {
		return empty, "", nil, nil, fmt.Errorf("container stop assignment changed")
	}
	var intent *RuntimeContainerStopIntent
	if raw := cm.Data["container_stop_intent"]; raw != "" {
		intent = &RuntimeContainerStopIntent{}
		if err = json.Unmarshal([]byte(raw), intent); err != nil || !containerIntentAssignment(*intent, a) {
			return empty, "", nil, nil, fmt.Errorf("invalid stored container stop intent")
		}
	}
	return a, state, intent, json.RawMessage(cm.Data["container_termination_proof"]), nil
}

func containerIntentAssignment(i RuntimeContainerStopIntent, a RuntimeAssignment) bool {
	return i.Version == 1 && i.OperationID != "" && i.Slot != "" && i.WorkerEpoch != "" && i.AssignmentDigest != "" &&
		i.InstanceID == a.InstanceID && i.UserID == a.UserID && i.Generation == a.Generation && i.PodUID == a.PodUID && i.GatewayID == a.GatewayID
}

// BeginContainerStop uses the same assignment CAS as placement. A persisted
// graceful operation is immutable for ordinary retries. Only the explicit
// force-delete kind may supersede it for the same pinned execution.
func (s *RuntimeAssignmentService) BeginContainerStop(ctx context.Context, endpoint string, candidate RuntimeContainerStopIntent) (*RuntimeContainerStopIntent, error) {
	if candidate.Kind == "force-delete" && candidate.Graceful {
		return nil, fmt.Errorf("force deletion cannot request graceful waiting")
	}
	if err := s.validService(); err != nil {
		return nil, err
	}
	var result RuntimeContainerStopIntent
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(candidate.InstanceID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		a, state, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		if a.Endpoint != endpoint || !containerIntentAssignment(candidate, a) || state != "draining" {
			return fmt.Errorf("container stop requires matching draining assignment")
		}
		if raw := cm.Data["container_stop_intent"]; raw != "" {
			if err = json.Unmarshal([]byte(raw), &result); err != nil || !containerIntentAssignment(result, a) {
				return fmt.Errorf("invalid existing container stop intent")
			}
			if result.Slot != candidate.Slot || result.WorkerEpoch != candidate.WorkerEpoch || result.AssignmentDigest != candidate.AssignmentDigest {
				return fmt.Errorf("container execution identity changed")
			}
			if candidate.Kind == "force-delete" && !candidate.Graceful && result.Kind == "" && cm.Data["container_termination_proof"] == "" {
				forced := result
				forced.Kind = "force-delete"
				forced.Graceful = false
				if forced != candidate {
					return fmt.Errorf("force deletion must preserve the current stop execution and operation")
				}
				data, marshalErr := json.Marshal(candidate)
				if marshalErr != nil {
					return marshalErr
				}
				cm.Data["container_stop_intent"] = string(data)
				_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
				if err == nil {
					result = candidate
				}
				return err
			}
			return nil
		}
		data, err := json.Marshal(candidate)
		if err != nil {
			return err
		}
		cm.Data["container_stop_intent"] = string(data)
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		if err == nil {
			result = candidate
		}
		return err
	})
	return &result, err
}

func (s *RuntimeAssignmentService) StoreContainerTermination(ctx context.Context, endpoint string, intent RuntimeContainerStopIntent, proof json.RawMessage) error {
	if err := s.validService(); err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(intent.InstanceID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		a, state, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		var stored RuntimeContainerStopIntent
		if a.Endpoint != endpoint || !containerIntentAssignment(intent, a) || (state != "draining" && state != "stopped") || json.Unmarshal([]byte(cm.Data["container_stop_intent"]), &stored) != nil || stored != intent {
			return fmt.Errorf("container stop proof is no longer current")
		}
		if previous := cm.Data["container_termination_proof"]; previous != "" && previous != string(proof) {
			return fmt.Errorf("container termination proof changed")
		}
		cm.Data["container_termination_proof"] = string(proof)
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

func (s *RuntimeAssignmentService) CompleteContainerRelease(ctx context.Context, endpoint string, intent RuntimeContainerStopIntent) error {
	if err := s.validService(); err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(intent.InstanceID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		a, state, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		var stored RuntimeContainerStopIntent
		if a.Endpoint != endpoint || state != "stopped" || !containerIntentAssignment(intent, a) || json.Unmarshal([]byte(cm.Data["container_stop_intent"]), &stored) != nil || stored != intent || cm.Data["container_termination_proof"] == "" {
			return fmt.Errorf("container release does not match confirmed stop")
		}
		cm.Data["container_stop_released"] = "true"
		cm.Labels["clawmanager.io/needs-reconcile"] = "false"
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

// ResolveCompletedContainerDelete records the actual old stop receipt when the
// old worker completed during an explicit same-execution force-delete CAS. The
// caller verifies the kubelet commitment before invoking this atomic transition.
func (s *RuntimeAssignmentService) ResolveCompletedContainerDelete(ctx context.Context, endpoint string, forced, actual RuntimeContainerStopIntent, proof json.RawMessage) error {
	if err := s.validService(); err != nil {
		return err
	}
	normalized := actual
	normalized.Kind = "force-delete"
	normalized.Graceful = false
	if forced.Kind != "force-delete" || forced.Graceful || actual.Kind != "" || normalized != forced || !json.Valid(proof) {
		return fmt.Errorf("completed stop does not match superseded force deletion")
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(forced.InstanceID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		a, state, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		var stored RuntimeContainerStopIntent
		if a.Endpoint != endpoint || !containerIntentAssignment(actual, a) || (state != "draining" && state != "stopped") || json.Unmarshal([]byte(cm.Data["container_stop_intent"]), &stored) != nil || stored != forced && stored != actual {
			return fmt.Errorf("force deletion assignment changed before completion")
		}
		if previous := cm.Data["container_termination_proof"]; previous != "" && previous != string(proof) {
			return fmt.Errorf("container termination proof changed")
		}
		encoded, err := json.Marshal(actual)
		if err != nil {
			return err
		}
		cm.Data["container_stop_intent"] = string(encoded)
		cm.Data["container_termination_proof"] = string(proof)
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}
