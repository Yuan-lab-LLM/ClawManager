package k8s

import (
	"context"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// RuntimePendingCancellation carries no business environment or filesystem
// authority. The worker derives its trusted writer fence from these identities.
type RuntimePendingCancellation struct {
	InstanceID  int    `json:"instance_id"`
	UserID      int    `json:"user_id"`
	Generation  int    `json:"generation"`
	PodUID      string `json:"pod_uid"`
	AgentType   string `json:"agent_type"`
	GatewayPort int    `json:"gateway_port"`
}

// PendingCancellation requires the original published request and a durable
// draining intent. It never creates an assignment or infers a stop from absence.
func (s *RuntimeAssignmentService) PendingCancellation(ctx context.Context, endpoint, gatewayID, podUID string) (RuntimePendingCancellation, error) {
	var scope RuntimePendingCancellation
	if err := s.validService(); err != nil {
		return scope, err
	}
	instanceID, generation, err := parseRuntimeAssignmentGatewayID(gatewayID)
	if err != nil {
		return scope, err
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(instanceID), metav1.GetOptions{})
	if err != nil {
		return scope, err
	}
	a, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return scope, err
	}
	if cm.Namespace != s.namespace || cm.Labels["clawmanager.io/purpose"] != "lite-instance" || state != "draining" || a.InstanceID != instanceID || a.Generation != generation || a.Endpoint != endpoint || a.GatewayID != gatewayID || podUID == "" || a.PodUID != podUID {
		return scope, fmt.Errorf("pending cancellation does not match draining assignment")
	}
	if json.Unmarshal(a.Request, &scope) != nil || scope.InstanceID != a.InstanceID || scope.UserID != a.UserID || scope.Generation != a.Generation || scope.PodUID != a.PodUID || scope.AgentType != a.RuntimeType || scope.GatewayPort < 1 || scope.GatewayPort > 65535 {
		return RuntimePendingCancellation{}, fmt.Errorf("pending cancellation request identity changed")
	}
	return scope, nil
}
