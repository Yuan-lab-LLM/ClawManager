package k8s

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

// RuntimeAssignment is written by the trusted controller, never by a gateway.
// Environment is stored exclusively in a generation-specific business Secret.
type RuntimeAssignment struct {
	InstanceID, UserID, Generation   int
	RuntimeType, Endpoint, GatewayID string
	PodUID                           string
	Request                          json.RawMessage
	Environment                      map[string]string
}

type RuntimeAssignmentService struct {
	client    kubernetes.Interface
	namespace string
}

func NewRuntimeAssignmentService(client kubernetes.Interface, namespace string) *RuntimeAssignmentService {
	return &RuntimeAssignmentService{client: client, namespace: namespace}
}

// ResolvePendingGatewayID recovers the published identity when SQL still has
// its pre-create reservation after an uncertain response. It grants no stop
// authority: callers must still obtain the normal matching teardown receipt.
func (s *RuntimeAssignmentService) ResolvePendingGatewayID(ctx context.Context, endpoint, id string) (string, error) {
	if !strings.HasPrefix(id, "pending-") {
		return id, nil
	}
	if err := s.validService(); err != nil {
		return "", err
	}
	canonical := "gw-" + strings.TrimPrefix(id, "pending-")
	instanceID, generation, err := parseRuntimeAssignmentGatewayID(canonical)
	if err != nil {
		return "", err
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(instanceID), metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	current, _, err := storedRuntimeAssignment(cm)
	if err != nil {
		return "", err
	}
	if current.InstanceID != instanceID || current.Generation != generation || current.Endpoint != endpoint || current.GatewayID != canonical {
		return "", fmt.Errorf("pending reservation does not match published runtime assignment")
	}
	return canonical, nil
}

// EnsureDesired publishes one current owner using Kubernetes resourceVersion CAS.
// A conflicting update is reread and revalidated; a lease or elapsed time never
// authorizes handover. A failed CAS can leave an unreferenced immutable Secret.
func (s *RuntimeAssignmentService) EnsureDesired(ctx context.Context, a RuntimeAssignment) error {
	if err := s.validService(); err != nil {
		return err
	}
	if err := validateRuntimeAssignment(a); err != nil {
		return err
	}
	request, err := runtimeAssignmentRequest(a.Request, true)
	if err != nil {
		return err
	}
	name := runtimeAssignmentName(a.InstanceID)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, name, metav1.GetOptions{})
		creating := apierrors.IsNotFound(err)
		if err != nil && !creating {
			return err
		}
		if !creating {
			current, state, err := storedRuntimeAssignment(cm)
			if err != nil {
				return err
			}
			if current.UserID != a.UserID || current.RuntimeType != a.RuntimeType {
				return fmt.Errorf("runtime assignment owner or runtime cannot change")
			}
			if a.Generation < current.Generation {
				return fmt.Errorf("stale runtime assignment generation")
			}
			if a.Generation == current.Generation {
				previousRequest, err := runtimeAssignmentRequest(current.Request, false)
				if err != nil {
					return err
				}
				if current.Endpoint != a.Endpoint || current.PodUID != a.PodUID || current.GatewayID != a.GatewayID || previousRequest != request {
					return fmt.Errorf("same-generation runtime assignment changed")
				}
				// Do not resurrect draining/stopped assignments on a delayed create retry.
				return s.ensureRuntimeAssignmentSecret(ctx, a)
			}
			if state != "stopped" {
				return fmt.Errorf("new runtime assignment generation requires confirmed source stop")
			}
		}
		// Validate current identity and migration authority before creating a Secret.
		if err := s.ensureRuntimeAssignmentSecret(ctx, a); err != nil {
			return err
		}
		if creating {
			cm = &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: s.namespace}}
		}
		if cm.Labels == nil {
			cm.Labels = map[string]string{}
		}
		cm.Labels["clawmanager.io/purpose"] = "lite-instance"
		cm.Labels["clawmanager.io/needs-reconcile"] = "true"
		cm.Data = map[string]string{
			"instance_id": strconv.Itoa(a.InstanceID), "user_id": strconv.Itoa(a.UserID),
			"generation": strconv.Itoa(a.Generation), "runtime_type": a.RuntimeType,
			"endpoint": a.Endpoint, "gateway_id": a.GatewayID, "desired_state": "running",
			"pod_uid": a.PodUID,
			"request": request, "secret_ref": runtimeAssignmentSecretName(a.InstanceID, a.Generation),
		}
		if creating {
			_, err = s.client.CoreV1().ConfigMaps(s.namespace).Create(ctx, cm, metav1.CreateOptions{})
			if apierrors.IsAlreadyExists(err) {
				return apierrors.NewConflict(schema.GroupResource{Resource: "configmaps"}, name, err)
			}
			return err
		}
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

type RuntimeDesiredAssignment struct {
	RuntimeAssignment
	State string
}

// CanRetryUnpublished permits rebuilding a pending SQL reservation only before
// its generation has ever published business credentials. Published generations
// are recovered using their original immutable request by ListPending/reports.
func (s *RuntimeAssignmentService) CanRetryUnpublished(ctx context.Context, a RuntimeAssignment) error {
	if err := s.validService(); err != nil {
		return err
	}
	if err := validateRuntimeAssignment(a); err != nil {
		return err
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(a.InstanceID), metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("read prior runtime assignment: %w", err)
	}
	current, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return err
	}
	if cm.Labels["clawmanager.io/purpose"] != "lite-instance" || current.InstanceID != a.InstanceID || current.UserID != a.UserID || current.RuntimeType != a.RuntimeType {
		return fmt.Errorf("pending runtime assignment owner or runtime changed")
	}
	if current.Generation >= a.Generation || state != "stopped" {
		return fmt.Errorf("pending retry requires an older confirmed stopped assignment")
	}
	_, err = s.client.CoreV1().Secrets(s.namespace).Get(ctx, runtimeAssignmentSecretName(a.InstanceID, a.Generation), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read pending generation business Secret: %w", err)
	}
	return fmt.Errorf("pending generation business Secret already exists; await authoritative reconciliation")
}

// ListPending recovers controller work lost between persisting desired state and
// the runtime HTTP response. Every entry and its generation Secret is validated;
// no missing identity or credential is replaced with an ambient default.
func (s *RuntimeAssignmentService) ListPending(ctx context.Context) ([]RuntimeDesiredAssignment, error) {
	if err := s.validService(); err != nil {
		return nil, err
	}
	list, err := s.client.CoreV1().ConfigMaps(s.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-instance,clawmanager.io/needs-reconcile=true"})
	if err != nil {
		return nil, err
	}
	var result []RuntimeDesiredAssignment
	var failures []error
	for _, cm := range list.Items {
		if cm.Labels["clawmanager.io/purpose"] != "lite-instance" || cm.Labels["clawmanager.io/needs-reconcile"] != "true" {
			continue
		}
		a, state, err := storedRuntimeAssignment(&cm)
		if err != nil {
			failures = append(failures, fmt.Errorf("invalid desired assignment %s: %w", cm.Name, err))
			continue
		}
		if state == "stopped" {
			continue
		}
		if state == "running" {
			if cm.Data["applied_generation"] == strconv.Itoa(a.Generation) && cm.Data["applied_pod_uid"] == a.PodUID {
				continue
			}
			secret, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, cm.Data["secret_ref"], metav1.GetOptions{})
			if err != nil {
				failures = append(failures, fmt.Errorf("load business Secret for %s: %w", cm.Name, err))
				continue
			}
			a.Environment = map[string]string{}
			for key, value := range secret.Data {
				a.Environment[key] = string(value)
			}
			if err := s.ensureRuntimeAssignmentSecret(ctx, a); err != nil {
				failures = append(failures, fmt.Errorf("invalid business Secret for %s: %w", cm.Name, err))
				continue
			}
		}
		result = append(result, RuntimeDesiredAssignment{RuntimeAssignment: a, State: state})
	}
	return result, errors.Join(failures...)
}

// MarkApplied records only a matching successful create acknowledgement. It
// prevents periodic replay of every healthy instance in a large pool. Gateway
// health and routing are still driven by the existing runtime state reports.
func (s *RuntimeAssignmentService) MarkApplied(ctx context.Context, endpoint, gatewayID string) error {
	id, generation, err := parseRuntimeAssignmentGatewayID(gatewayID)
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
		if err != nil {
			return err
		}
		a, state, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		if a.Generation != generation || a.Endpoint != endpoint || state != "running" {
			return fmt.Errorf("create acknowledgement is no longer current")
		}
		if cm.Data["applied_generation"] == strconv.Itoa(generation) && cm.Data["applied_pod_uid"] == a.PodUID {
			return nil
		}
		cm.Data["applied_generation"], cm.Data["applied_pod_uid"] = strconv.Itoa(generation), a.PodUID
		if cm.Labels == nil {
			cm.Labels = map[string]string{}
		}
		cm.Labels["clawmanager.io/needs-reconcile"] = "false"
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

func (s *RuntimeAssignmentService) MarkDraining(ctx context.Context, endpoint, gatewayID string) error {
	return s.transition(ctx, endpoint, gatewayID, false)
}

// CanStart checks current authority after an idempotent EnsureDesired. A retry
// for an already draining/stopped generation must never start its old writer.
func (s *RuntimeAssignmentService) CanStart(ctx context.Context, endpoint, gatewayID string) error {
	if err := s.validService(); err != nil {
		return err
	}
	id, generation, err := parseRuntimeAssignmentGatewayID(gatewayID)
	if err != nil {
		return err
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
	if err != nil {
		return err
	}
	current, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return err
	}
	if current.InstanceID != id || current.Generation != generation || current.GatewayID != gatewayID || current.Endpoint != endpoint || state != "running" {
		return fmt.Errorf("runtime assignment does not authorize startup")
	}
	return nil
}

// ConfirmStopped records the trusted caller's successful stop confirmation.
// The caller must obtain actual process/unmount evidence before invoking it.
func (s *RuntimeAssignmentService) ConfirmStopped(ctx context.Context, endpoint, gatewayID string) error {
	return s.transition(ctx, endpoint, gatewayID, true)
}

func (s *RuntimeAssignmentService) transition(ctx context.Context, endpoint, gatewayID string, stopped bool) error {
	if err := s.validService(); err != nil {
		return err
	}
	id, generation, err := parseRuntimeAssignmentGatewayID(gatewayID)
	if err != nil {
		return err
	}
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
		if err != nil {
			return err
		} // Unknown deletes must not authorize future writers.
		current, state, err := storedRuntimeAssignment(cm)
		if err != nil {
			return err
		}
		if current.InstanceID != id || current.Generation != generation || current.GatewayID != gatewayID || current.Endpoint != endpoint {
			return fmt.Errorf("runtime stop identity or endpoint does not match current assignment")
		}
		if state == "stopped" {
			return nil
		}
		nextState := "draining"
		if stopped {
			if state != "draining" {
				return fmt.Errorf("runtime stop confirmation requires draining assignment")
			}
			nextState = "stopped"
		} else if state == "draining" {
			return nil
		}
		cm.Data["desired_state"] = nextState
		if cm.Labels == nil {
			cm.Labels = map[string]string{}
		}
		cm.Labels["clawmanager.io/needs-reconcile"] = strconv.FormatBool(nextState != "stopped")
		_, err = s.client.CoreV1().ConfigMaps(s.namespace).Update(ctx, cm, metav1.UpdateOptions{})
		return err
	})
}

func (s *RuntimeAssignmentService) ensureRuntimeAssignmentSecret(ctx context.Context, a RuntimeAssignment) error {
	data := make(map[string][]byte, len(a.Environment))
	for key, value := range a.Environment {
		data[key] = []byte(value)
	}
	labels := map[string]string{
		"clawmanager.io/instance-id": strconv.Itoa(a.InstanceID), "clawmanager.io/user-id": strconv.Itoa(a.UserID),
		"clawmanager.io/generation": strconv.Itoa(a.Generation), "clawmanager.io/runtime-type": a.RuntimeType,
	}
	immutable := true
	want := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: runtimeAssignmentSecretName(a.InstanceID, a.Generation), Namespace: s.namespace, Labels: labels},
		Type: corev1.SecretTypeOpaque, Immutable: &immutable, Data: data}
	_, err := s.client.CoreV1().Secrets(s.namespace).Create(ctx, want, metav1.CreateOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return err
	}
	actual, err := s.client.CoreV1().Secrets(s.namespace).Get(ctx, want.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if actual.Type != want.Type || actual.Immutable == nil || !*actual.Immutable || len(actual.Data) != len(data) {
		return fmt.Errorf("existing generation business Secret does not match assignment")
	}
	for key, value := range labels {
		if actual.Labels[key] != value {
			return fmt.Errorf("existing generation business Secret has different owner")
		}
	}
	for key, value := range data {
		got, exists := actual.Data[key]
		if !exists || !bytes.Equal(got, value) {
			return fmt.Errorf("existing generation business Secret has different contents")
		}
	}
	return nil
}

func (s *RuntimeAssignmentService) validService() error {
	if s == nil || s.client == nil || strings.TrimSpace(s.namespace) == "" {
		return fmt.Errorf("runtime assignment client and namespace are required")
	}
	return nil
}

func validateRuntimeAssignment(a RuntimeAssignment) error {
	if a.InstanceID <= 0 || a.UserID <= 0 || a.Generation <= 0 {
		return fmt.Errorf("runtime assignment identity and generation must be positive")
	}
	if strings.TrimSpace(a.Endpoint) == "" || a.Endpoint != strings.TrimSpace(a.Endpoint) || strings.TrimSpace(a.RuntimeType) == "" {
		return fmt.Errorf("runtime assignment endpoint and runtime are required")
	}
	if a.GatewayID != fmt.Sprintf("gw-%d-%d", a.InstanceID, a.Generation) {
		return fmt.Errorf("runtime assignment gateway identity does not match generation")
	}
	for key := range a.Environment {
		if len(validation.IsConfigMapKey(key)) != 0 {
			return fmt.Errorf("invalid runtime assignment environment key")
		}
	}
	return nil
}

func storedRuntimeAssignment(cm *corev1.ConfigMap) (RuntimeAssignment, string, error) {
	a := RuntimeAssignment{RuntimeType: cm.Data["runtime_type"], Endpoint: cm.Data["endpoint"], PodUID: cm.Data["pod_uid"], GatewayID: cm.Data["gateway_id"], Request: json.RawMessage(cm.Data["request"])}
	for key, field := range map[string]*int{"instance_id": &a.InstanceID, "user_id": &a.UserID, "generation": &a.Generation} {
		number, err := strconv.Atoi(cm.Data[key])
		if err != nil || strconv.Itoa(number) != cm.Data[key] {
			return a, "", fmt.Errorf("invalid stored runtime assignment identity")
		}
		*field = number
	}
	if err := validateRuntimeAssignment(a); err != nil {
		return a, "", err
	}
	if cm.Name != runtimeAssignmentName(a.InstanceID) || cm.Data["secret_ref"] != runtimeAssignmentSecretName(a.InstanceID, a.Generation) {
		return a, "", fmt.Errorf("invalid stored runtime assignment references")
	}
	state := cm.Data["desired_state"]
	if state != "running" && state != "draining" && state != "stopped" {
		return a, "", fmt.Errorf("invalid stored runtime assignment state")
	}
	return a, state, nil
}

func runtimeAssignmentRequest(raw json.RawMessage, stripEnvironment bool) (string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var request map[string]any
	if err := decoder.Decode(&request); err != nil || request == nil {
		return "", fmt.Errorf("runtime assignment request must be a JSON object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return "", fmt.Errorf("runtime assignment request has trailing data")
	}
	if stripEnvironment {
		stripRuntimeAssignmentEnvironment(request)
	}
	encoded, err := json.Marshal(request)
	return string(encoded), err
}

func stripRuntimeAssignmentEnvironment(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, nested := range value {
			if strings.EqualFold(key, "environment") || strings.EqualFold(key, "env") {
				delete(value, key)
			} else {
				stripRuntimeAssignmentEnvironment(nested)
			}
		}
	case []any:
		for _, nested := range value {
			stripRuntimeAssignmentEnvironment(nested)
		}
	}
}

func runtimeAssignmentName(id int) string { return fmt.Sprintf("lite-instance-%d", id) }
func runtimeAssignmentSecretName(id, generation int) string {
	return fmt.Sprintf("lite-instance-%d-g%d", id, generation)
}

func parseRuntimeAssignmentGatewayID(value string) (int, int, error) {
	parts := strings.Split(value, "-")
	if len(parts) == 3 && parts[0] == "gw" {
		id, firstErr := strconv.Atoi(parts[1])
		generation, secondErr := strconv.Atoi(parts[2])
		if firstErr == nil && secondErr == nil && id > 0 && generation > 0 && value == fmt.Sprintf("gw-%d-%d", id, generation) {
			return id, generation, nil
		}
	}
	return 0, 0, fmt.Errorf("invalid runtime gateway identity")
}

// StoppedAssignment returns only the persisted identity backed by the matching
// confirmed teardown receipt. No business Secret or command is read or granted.
func (s *RuntimeAssignmentService) StoppedAssignment(ctx context.Context, id int) (*RuntimeAssignment, error) {
	if err := s.validService(); err != nil {
		return nil, err
	}
	cm, err := s.client.CoreV1().ConfigMaps(s.namespace).Get(ctx, runtimeAssignmentName(id), metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if cm.DeletionTimestamp != nil || cm.Labels["clawmanager.io/purpose"] != "lite-instance" {
		return nil, fmt.Errorf("stopped assignment ownership is unavailable")
	}
	a, state, err := storedRuntimeAssignment(cm)
	if err != nil {
		return nil, err
	}
	if a.InstanceID != id || state != "stopped" {
		return nil, fmt.Errorf("runtime assignment has no confirmed stop")
	}
	a.Request = nil
	a.Environment = nil
	return &a, nil
}
