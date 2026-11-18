package k8s

import (
	"clawreef/internal/runtimeeviction"
	"context"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/util/retry"
	"reflect"
	"strconv"
)

const faultEvictionAnnotation = "clawmanager.io/fault-eviction"

type RuntimeFaultAssignment struct {
	RuntimeAssignment
	UID           string `json:"uid"`
	SecretUID     string `json:"secret_uid"`
	SecretVersion string `json:"secret_version"`
}

func (s *RuntimeAssignmentService) FaultEvictionSource(ctx context.Context, endpoint, gateway string) (*RuntimeFaultAssignment, error) {
	id, generation, err := parseRuntimeAssignmentGatewayID(gateway)
	if err != nil {
		return nil, err
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	a, _, err := storedRuntimeAssignment(cm)
	if err != nil {
		return nil, err
	}
	if a.InstanceID != id || a.Generation != generation || a.GatewayID != gateway || a.Endpoint != endpoint || a.PodUID == "" || cm.UID == "" || cm.ResourceVersion == "" {
		return nil, fmt.Errorf("fault eviction source identity is incomplete or changed")
	}
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if secret.UID == "" || secret.ResourceVersion == "" || secret.Immutable == nil || !*secret.Immutable || secret.DeletionTimestamp != nil || secret.Type != corev1.SecretTypeOpaque {
		return nil, fmt.Errorf("fault eviction requires immutable source credentials")
	}
	for key, want := range map[string]string{"clawmanager.io/instance-id": strconv.Itoa(a.InstanceID), "clawmanager.io/user-id": strconv.Itoa(a.UserID), "clawmanager.io/generation": strconv.Itoa(a.Generation), "clawmanager.io/runtime-type": a.RuntimeType} {
		if secret.Labels[key] != want {
			return nil, fmt.Errorf("source credential ownership changed")
		}
	}
	return &RuntimeFaultAssignment{RuntimeAssignment: a, UID: string(cm.UID), SecretUID: string(secret.UID), SecretVersion: secret.ResourceVersion}, nil
}

func sameFaultAssignment(a, b *RuntimeFaultAssignment) bool {
	return a != nil && b != nil && a.UID == b.UID && a.SecretUID == b.SecretUID && a.SecretVersion == b.SecretVersion && reflect.DeepEqual(a.RuntimeAssignment, b.RuntimeAssignment)
}

// The durable operation is the fence owner. The assignment stays draining even
// if its old Agent cannot answer; a second operation cannot steal its authority.
func (s *RuntimeAssignmentService) FenceFaultEviction(ctx context.Context, source *RuntimeFaultAssignment, operation string) error {
	if source == nil || operation == "" {
		return fmt.Errorf("fault eviction operation is required")
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := s.FaultEvictionSource(ctx, source.Endpoint, source.GatewayID)
		if err != nil {
			return err
		}
		if !sameFaultAssignment(current, source) {
			return fmt.Errorf("fault eviction source changed")
		}
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(source.InstanceID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		a, _, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		if string(cm.UID) != source.UID || !reflect.DeepEqual(a, source.RuntimeAssignment) {
			return fmt.Errorf("source changed while fencing")
		}
		if old := cm.Annotations[faultEvictionAnnotation]; old != "" && old != operation {
			return fmt.Errorf("another fault eviction owns this assignment")
		}
		if cm.Annotations == nil {
			cm.Annotations = map[string]string{}
		}
		cm.Annotations[faultEvictionAnnotation] = operation
		if cm.Data["desired_state"] != "stopped" {
			cm.Data["desired_state"] = "draining"
		}
		cm.Labels["clawmanager.io/needs-reconcile"] = "false"
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

// Preserve native evidence in the independent recovery audit rather than forging
// worker epochs or acknowledgements. Retire only this exact old assignment.
func (s *RuntimeAssignmentService) ConfirmFaultEvictionStopped(ctx context.Context, source *RuntimeFaultAssignment, scope runtimeeviction.Scope, receipt *runtimeeviction.Receipt) error {
	if err := runtimeeviction.ValidateReceipt(scope, receipt); err != nil {
		return err
	}
	if source == nil || scope.InstanceID != source.InstanceID || scope.UserID != source.UserID || scope.Generation != source.Generation || scope.PodUID != source.PodUID || scope.GatewayID != source.GatewayID || scope.RuntimeType != source.RuntimeType {
		return fmt.Errorf("receipt does not cover the source assignment")
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		current, err := s.FaultEvictionSource(ctx, source.Endpoint, source.GatewayID)
		if err != nil {
			return err
		}
		if !sameFaultAssignment(current, source) {
			return fmt.Errorf("fault eviction assignment changed before retirement")
		}
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(source.InstanceID), metav1.GetOptions{})
		if err != nil {
			return err
		}
		a, _, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		if string(cm.UID) != source.UID || !reflect.DeepEqual(a, source.RuntimeAssignment) || cm.Annotations[faultEvictionAnnotation] != scope.OperationID {
			return fmt.Errorf("fault eviction fence changed")
		}
		previous := map[string]string{}
		for _, key := range []string{"container_stop_intent", "container_termination_proof", "container_stop_released"} {
			if v := cm.Data[key]; v != "" {
				previous[key] = v
			}
		}
		audit, _ := json.Marshal(struct {
			Scope   runtimeeviction.Scope    `json:"scope"`
			Receipt *runtimeeviction.Receipt `json:"receipt"`
			Native  map[string]string        `json:"native_evidence,omitempty"`
		}{scope, receipt, previous})
		if old := cm.Annotations["clawmanager.io/fault-eviction-receipt-"+strconv.Itoa(source.Generation)]; old != "" {
			var prior struct {
				Scope   runtimeeviction.Scope    `json:"scope"`
				Receipt *runtimeeviction.Receipt `json:"receipt"`
			}
			if json.Unmarshal([]byte(old), &prior) != nil || runtimeeviction.ScopeDigest(prior.Scope) != runtimeeviction.ScopeDigest(scope) || runtimeeviction.ValidateReceipt(scope, prior.Receipt) != nil {
				return fmt.Errorf("source has conflicting recovery evidence")
			}
			return nil
		}
		cm.Annotations["clawmanager.io/fault-eviction-receipt-"+strconv.Itoa(source.Generation)] = string(audit)
		for key := range previous {
			delete(cm.Data, key)
		}
		cm.Data["desired_state"] = "stopped"
		cm.Labels["clawmanager.io/needs-reconcile"] = "false"
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

// Recover a terminal checkpoint lost between its Kubernetes CAS and SQL save.
func (s *RuntimeAssignmentService) FaultEvictionReceipt(ctx context.Context, source *RuntimeFaultAssignment, scope runtimeeviction.Scope) (*runtimeeviction.Receipt, error) {
	if source == nil {
		return nil, fmt.Errorf("fault eviction source missing")
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(source.InstanceID), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	a, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return nil, err
	}
	if string(cm.UID) != source.UID || !reflect.DeepEqual(a, source.RuntimeAssignment) || cm.Annotations[faultEvictionAnnotation] != scope.OperationID {
		return nil, fmt.Errorf("fault eviction source fence changed")
	}
	raw := cm.Annotations["clawmanager.io/fault-eviction-receipt-"+strconv.Itoa(source.Generation)]
	if raw == "" {
		return nil, nil
	}
	var audit struct {
		Scope   runtimeeviction.Scope    `json:"scope"`
		Receipt *runtimeeviction.Receipt `json:"receipt"`
	}
	if state != "stopped" || json.Unmarshal([]byte(raw), &audit) != nil || runtimeeviction.ScopeDigest(audit.Scope) != runtimeeviction.ScopeDigest(scope) {
		return nil, fmt.Errorf("stored fault eviction receipt conflicts with this operation")
	}
	if err = runtimeeviction.ValidateReceipt(scope, audit.Receipt); err != nil {
		return nil, err
	}
	return audit.Receipt, nil
}
