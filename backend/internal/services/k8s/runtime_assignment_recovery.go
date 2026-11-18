package k8s

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const StoppedSourceRecoveryAnnotation = "clawmanager.io/stopped-source-recovery"

type RuntimeStoppedRecoverySource struct {
	RuntimeAssignment
	SuccessorGeneration int
	cm                  *corev1.ConfigMap
	secretIdentity      string
}

// Snapshot only an exact unpublished successor's predecessor. Unresolved native
// stop intents keep their existing reconciliation path and cannot use this one.
func (s *RuntimeAssignmentService) StoppedRecoverySource(ctx context.Context, id, successor int) (*RuntimeStoppedRecoverySource, error) {
	if err := s.validService(); err != nil {
		return nil, err
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	a, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return nil, err
	}
	if cm.Labels["clawmanager.io/purpose"] != "lite-instance" || cm.ResourceVersion == "" || a.InstanceID != id || a.PodUID == "" || a.Generation+1 != successor || (state != "running" && state != "draining") || cm.Data["container_stop_intent"] != "" || cm.Data["container_termination_proof"] != "" || cm.Data["container_stop_released"] != "" {
		return nil, fmt.Errorf("stopped recovery requires an exact predecessor without an unresolved stop intent")
	}
	if err = s.requireUnpublishedSuccessor(ctx, id, successor); err != nil {
		return nil, err
	}
	secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if secret.Immutable == nil || !*secret.Immutable || secret.Type != corev1.SecretTypeOpaque || secret.DeletionTimestamp != nil {
		return nil, fmt.Errorf("source generation credentials are not immutable")
	}
	for key, want := range map[string]string{"clawmanager.io/instance-id": strconv.Itoa(a.InstanceID), "clawmanager.io/user-id": strconv.Itoa(a.UserID), "clawmanager.io/generation": strconv.Itoa(a.Generation), "clawmanager.io/runtime-type": a.RuntimeType} {
		if secret.Labels[key] != want {
			return nil, fmt.Errorf("source generation credential identity differs")
		}
	}
	for key := range secret.Data {
		if strings.HasPrefix(strings.ToUpper(key), "CLAWMANAGER_TEAM_") {
			return nil, fmt.Errorf("Team source requires coordinated shared-writer teardown")
		}
	}
	return &RuntimeStoppedRecoverySource{RuntimeAssignment: a, SuccessorGeneration: successor, cm: cm.DeepCopy(), secretIdentity: string(secret.UID) + "/" + secret.ResourceVersion}, nil
}

func (s *RuntimeAssignmentService) requireUnpublishedSuccessor(ctx context.Context, id, generation int) error {
	_, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, runtimeAssignmentSecretName(id, generation), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("successor generation has already published credentials")
}

// Caller must verify the authenticated stopped-writer proof, storage provenance,
// SQL reservation, and external writer fences before committing this exact CAS.
// Audit is part of the same write and survives EnsureDesired's next generation.
func (s *RuntimeAssignmentService) ConfirmRecoveredStopped(ctx context.Context, source *RuntimeStoppedRecoverySource, actor int, observerUID, nonce, digest, storage string) error {
	validHex := func(value string) bool {
		raw, err := hex.DecodeString(value)
		return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == value
	}
	if source == nil || source.cm == nil || actor < 1 || observerUID == "" || observerUID == source.PodUID || !validHex(nonce) || !validHex(digest) || storage == "" {
		return fmt.Errorf("stopped source proof identity is incomplete")
	}
	current, err := s.StoppedRecoverySource(ctx, source.InstanceID, source.SuccessorGeneration)
	if err != nil {
		return err
	}
	if current.cm.ResourceVersion != source.cm.ResourceVersion || current.cm.UID != source.cm.UID || current.secretIdentity != source.secretIdentity || !reflect.DeepEqual(current.cm.Data, source.cm.Data) || !reflect.DeepEqual(current.RuntimeAssignment, source.RuntimeAssignment) {
		return fmt.Errorf("stopped source assignment changed during verification")
	}
	cm := current.cm.DeepCopy()
	if cm.Annotations == nil {
		cm.Annotations = map[string]string{}
	}
	audit, err := json.Marshal(map[string]any{"actor_user_id": actor, "instance_id": source.InstanceID, "source_generation": source.Generation, "source_pod_uid": source.PodUID, "source_gateway_id": source.GatewayID, "successor_generation": source.SuccessorGeneration, "observer_pod_uid": observerUID, "nonce": nonce, "source_record_digest": digest, "storage_identity": storage, "prior_resource_version": source.cm.ResourceVersion, "confirmed_at": time.Now().UTC()})
	if err != nil {
		return err
	}
	cm.Annotations[StoppedSourceRecoveryAnnotation] = string(audit)
	cm.Data["desired_state"] = "stopped"
	cm.Labels["clawmanager.io/needs-reconcile"] = "false"
	_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
	return err
}
