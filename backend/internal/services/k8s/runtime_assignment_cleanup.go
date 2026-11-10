package k8s

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const RuntimeStoppedSourceAnnotationPrefix = "clawmanager.io/stopped-source-"

// RuntimeStoppedSourceAudit preserves existing controller-confirmed stop
// authority before EnsureDesired replaces a generation's current assignment.
// It contains no credentials, request environment, or synthetic Agent ACK.
type RuntimeStoppedSourceAudit struct {
	InstanceID    int    `json:"instance_id"`
	UserID        int    `json:"user_id"`
	Generation    int    `json:"generation"`
	RuntimeType   string `json:"runtime_type"`
	PodUID        string `json:"pod_uid"`
	Endpoint      string `json:"endpoint"`
	GatewayID     string `json:"gateway_id"`
	WorkspacePath string `json:"workspace_path"`
	Stopped       bool   `json:"stopped"`
}

func assignmentWorkspace(a RuntimeAssignment) string {
	var request map[string]json.RawMessage
	if json.Unmarshal(a.Request, &request) != nil {
		return ""
	}
	for key, value := range request {
		if strings.EqualFold(key, "workspace_path") || strings.EqualFold(key, "WorkspacePath") {
			var workspace string
			if json.Unmarshal(value, &workspace) == nil {
				return workspace
			}
		}
	}
	return ""
}

func retainStoppedSourceAudit(cm *corev1.ConfigMap, a RuntimeAssignment) error {
	if (cm.Data["container_stop_intent"] != "" || cm.Data["container_termination_proof"] != "") && (cm.Data["container_termination_proof"] == "" || cm.Data["container_stop_released"] != "true") {
		return fmt.Errorf("source generation %d has unfinalized container stop proof", a.Generation)
	}
	workspace := assignmentWorkspace(a)
	// Historical unscoped legacy assignments keep their existing transition path;
	// their missing identity will block later no-binding cleanup, never be guessed.
	if a.PodUID == "" || workspace == "" {
		return nil
	}
	proof := RuntimeStoppedSourceAudit{InstanceID: a.InstanceID, UserID: a.UserID, Generation: a.Generation, RuntimeType: a.RuntimeType, PodUID: a.PodUID, Endpoint: a.Endpoint, GatewayID: a.GatewayID, WorkspacePath: workspace, Stopped: true}
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	name := RuntimeStoppedSourceAnnotationPrefix + strconv.Itoa(a.Generation)
	raw, err := json.Marshal(proof)
	if err != nil {
		return err
	}
	if previous := cm.Annotations[name]; previous != "" {
		var prior RuntimeStoppedSourceAudit
		if json.Unmarshal([]byte(previous), &prior) != nil || prior != proof {
			return fmt.Errorf("stopped source generation %d audit conflicts", a.Generation)
		}
	}
	cm.Annotations[name] = string(raw)
	return nil
}

func ValidateRuntimeStoppedSourceAudit(a RuntimeStoppedSourceAudit, id, user, generation int, kind string) error {
	if !a.Stopped || a.InstanceID != id || a.UserID != user || a.Generation <= 0 || a.Generation > generation || a.RuntimeType != kind || a.PodUID == "" || a.Endpoint == "" || a.GatewayID != fmt.Sprintf("gw-%d-%d", id, a.Generation) || a.WorkspacePath == "" {
		return fmt.Errorf("stopped source generation %d identity is incomplete or changed", a.Generation)
	}
	return nil
}

// CheckUnboundInstanceStopped inventories every publication generation. Missing
// SQL bindings or a missing ConfigMap alone never authorize workspace deletion.
func (s *RuntimeAssignmentService) CheckUnboundInstanceStopped(ctx context.Context, id, user, generation int, kind string, sources []models.RuntimeCleanupSource) error {
	if err := s.validService(); err != nil {
		return err
	}
	if id <= 0 || user <= 0 || generation < 0 || kind == "" {
		return fmt.Errorf("unbound cleanup instance identity is incomplete")
	}
	proofs := []RuntimeStoppedSourceAudit{}
	for _, source := range sources {
		if source.Blocker != "" {
			return fmt.Errorf("instance %d source generation %d Pod %s: %s", id, source.Generation, source.PodUID, source.Blocker)
		}
		if source.StoppedProof {
			gateway := source.GatewayID
			if strings.HasPrefix(gateway, "pending-") {
				gateway = "gw-" + strings.TrimPrefix(gateway, "pending-")
			}
			proof := RuntimeStoppedSourceAudit{InstanceID: id, UserID: user, Generation: source.Generation, RuntimeType: kind, PodUID: source.PodUID, Endpoint: source.Endpoint, GatewayID: gateway, WorkspacePath: source.WorkspacePath, Stopped: true}
			if err := ValidateRuntimeStoppedSourceAudit(proof, id, user, generation, kind); err != nil {
				return err
			}
			proofs = append(proofs, proof)
		}
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
	missing := apierrors.IsNotFound(err)
	if err != nil && !missing {
		return err
	}
	if !missing {
		current, state, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		if current.InstanceID != id || current.UserID != user || current.RuntimeType != kind || current.Generation > generation || cm.DeletionTimestamp != nil {
			return fmt.Errorf("unbound assignment owner or generation changed")
		}
		if state != "stopped" {
			return fmt.Errorf("source generation %d Pod %s still has %s publication intent", current.Generation, current.PodUID, state)
		}
		if (cm.Data["container_stop_intent"] != "" || cm.Data["container_termination_proof"] != "") && (cm.Data["container_termination_proof"] == "" || cm.Data["container_stop_released"] != "true") {
			return fmt.Errorf("source generation %d Pod %s has unfinalized container teardown", current.Generation, current.PodUID)
		}
		proof := RuntimeStoppedSourceAudit{InstanceID: id, UserID: user, Generation: current.Generation, RuntimeType: kind, PodUID: current.PodUID, Endpoint: current.Endpoint, GatewayID: current.GatewayID, WorkspacePath: assignmentWorkspace(current), Stopped: true}
		if err := ValidateRuntimeStoppedSourceAudit(proof, id, user, generation, kind); err != nil {
			return err
		}
		proofs = append(proofs, proof)
		for key, raw := range cm.Annotations {
			if strings.HasPrefix(key, RuntimeStoppedSourceAnnotationPrefix) {
				var proof RuntimeStoppedSourceAudit
				if json.Unmarshal([]byte(raw), &proof) != nil {
					return fmt.Errorf("malformed historical stopped-source audit")
				}
				if key != RuntimeStoppedSourceAnnotationPrefix+strconv.Itoa(proof.Generation) {
					return fmt.Errorf("historical stopped-source generation key mismatch")
				}
				if err := ValidateRuntimeStoppedSourceAudit(proof, id, user, generation, kind); err != nil {
					return err
				}
				proofs = append(proofs, proof)
			}
			if strings.HasPrefix(key, "clawmanager.io/fault-eviction-receipt-") {
				var audit struct {
					Scope   runtimeeviction.Scope    `json:"scope"`
					Receipt *runtimeeviction.Receipt `json:"receipt"`
				}
				if json.Unmarshal([]byte(raw), &audit) != nil {
					return fmt.Errorf("malformed historical fault stop receipt")
				}
				if err := runtimeeviction.ValidateReceipt(audit.Scope, audit.Receipt); err != nil {
					return err
				}
				if audit.Scope.InstanceID != id || audit.Scope.UserID != user || audit.Scope.RuntimeType != kind || audit.Scope.Generation > generation || key != "clawmanager.io/fault-eviction-receipt-"+strconv.Itoa(audit.Scope.Generation) {
					return fmt.Errorf("historical fault stop receipt owner changed")
				}
				// Endpoint is not part of helper scope; retain the exact Pod/generation identity.
				proofs = append(proofs, RuntimeStoppedSourceAudit{InstanceID: id, UserID: user, Generation: audit.Scope.Generation, RuntimeType: kind, PodUID: audit.Scope.PodUID, GatewayID: audit.Scope.GatewayID, WorkspacePath: fmt.Sprintf("%s/%s/user-%d/instance-%d", audit.Scope.WorkspaceRoot, kind, user, id), Stopped: true})
			}
		}
	}

	secrets, err := s.client.CoreV1().Secrets(s.namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
	}
	publications := 0
	prefix := fmt.Sprintf("lite-instance-%d-g", id)
	for _, secret := range secrets.Items {
		if secret.Labels["clawmanager.io/instance-id"] != strconv.Itoa(id) && !strings.HasPrefix(secret.Name, prefix) {
			continue
		}
		publications++
		gen, parseErr := strconv.Atoi(secret.Labels["clawmanager.io/generation"])
		if parseErr != nil || gen <= 0 || gen > generation || secret.Name != runtimeAssignmentSecretName(id, gen) || secret.Labels["clawmanager.io/instance-id"] != strconv.Itoa(id) || secret.Labels["clawmanager.io/user-id"] != strconv.Itoa(user) || secret.Labels["clawmanager.io/runtime-type"] != kind || secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque {
			return fmt.Errorf("publication credential %s has uncertain owner or generation", secret.Name)
		}
		if raw := secret.Annotations[RuntimeStoppedSourceAnnotationPrefix+strconv.Itoa(gen)]; raw != "" {
			var proof RuntimeStoppedSourceAudit
			if json.Unmarshal([]byte(raw), &proof) != nil {
				return fmt.Errorf("publication generation %d has malformed stopped-source audit", gen)
			}
			if proof.Generation != gen {
				return fmt.Errorf("publication generation %d stop audit names another generation", gen)
			}
			if err := ValidateRuntimeStoppedSourceAudit(proof, id, user, generation, kind); err != nil {
				return err
			}
			proofs = append(proofs, proof)
		}
	}
	for _, source := range sources {
		covered := false
		gateway := source.GatewayID
		if strings.HasPrefix(gateway, "pending-") {
			gateway = "gw-" + strings.TrimPrefix(gateway, "pending-")
		}
		for _, proof := range proofs {
			if proof.Generation == source.Generation && proof.PodUID == source.PodUID && proof.GatewayID == gateway && (source.WorkspacePath == "" || proof.WorkspacePath == source.WorkspacePath) && (source.Endpoint == "" || proof.Endpoint == "" || source.Endpoint == proof.Endpoint) {
				covered = true
			}
		}
		if !covered {
			return fmt.Errorf("historical source generation %d Pod %s gateway %s has no retained matching stop proof", source.Generation, source.PodUID, source.GatewayID)
		}
	}
	for _, secret := range secrets.Items {
		if secret.Labels["clawmanager.io/instance-id"] != strconv.Itoa(id) && !strings.HasPrefix(secret.Name, prefix) {
			continue
		}
		gen, _ := strconv.Atoi(secret.Labels["clawmanager.io/generation"])

		covered := false
		for _, proof := range proofs {
			if proof.Generation == gen {
				covered = true
			}
		}
		if !covered {
			return fmt.Errorf("published generation %d has credentials but no retained stop proof", gen)
		}
	}
	if missing && publications == 0 && len(sources) == 0 && generation > 1 {
		return fmt.Errorf("generation %d has no retained publication history; never-published state cannot be proved", generation)
	}
	return nil
}
