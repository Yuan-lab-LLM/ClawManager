package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

const RuntimeSecurityProfile = "isolated-network-v1"

var errRuntimeSecurity = errors.New("runtime security assignment is unavailable")

type runtimeSecurityInstanceSource interface {
	GetByIDContext(context.Context, int) (*models.Instance, error)
}

// RuntimeSecurityAuthority requires SQL's sticky security profile, the current
// ConfigMap desired owner and the live protected Pod to agree. Losing one source
// never permits falling back to a long-lived instance token.
type RuntimeSecurityAuthority struct {
	kube         kubernetes.Interface
	namespace    string
	instances    runtimeSecurityInstanceSource
	capabilities *runtimeauth.Service
	key          []byte
	assignments  RuntimeSecurityAssignmentSource
	teamNetwork  func(context.Context, *models.Instance) error
}

type RuntimeSecurityAssignmentSource interface {
	Assignment(context.Context, int) (*corev1.ConfigMap, []corev1.Pod, error)
}

type RuntimeSecurityOption func(*RuntimeSecurityAuthority) error

// WithRuntimeSecurityAssignments installs the shared freshness-checked source
// before publication. Source failure never falls back to direct API reads.
func WithRuntimeSecurityAssignments(source RuntimeSecurityAssignmentSource) RuntimeSecurityOption {
	return func(a *RuntimeSecurityAuthority) error {
		if source == nil {
			return errors.New("runtime assignment source is required")
		}
		a.assignments = source
		return nil
	}
}

func NewRuntimeSecurityAuthority(kube kubernetes.Interface, namespace string, instances runtimeSecurityInstanceSource, key []byte, options ...RuntimeSecurityOption) (*RuntimeSecurityAuthority, error) {
	if kube == nil || instances == nil || len(validation.IsDNS1123Label(namespace)) != 0 {
		return nil, errors.New("invalid runtime security configuration")
	}
	a := &RuntimeSecurityAuthority{kube: kube, namespace: namespace, instances: instances, key: append([]byte(nil), key...)}
	for _, option := range options {
		if option == nil {
			return nil, errRuntimeSecurity
		}
		if err := option(a); err != nil {
			return nil, err
		}
	}
	service, err := runtimeauth.New(key, a.CurrentIdentity)
	if err != nil {
		return nil, err
	}
	a.capabilities = service
	return a, nil
}

func (a *RuntimeSecurityAuthority) Capabilities() *runtimeauth.Service { return a.capabilities }

// ProtectedInstance reads the durable marker, independently of assignment or
// Pod availability. Unknown nonempty profiles also forbid legacy token access.
func (a *RuntimeSecurityAuthority) ProtectedInstance(ctx context.Context, id int) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	instance, err := a.instances.GetByIDContext(ctx, id)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return false, ctxErr
	}
	if err != nil || instance == nil || instance.ID != id {
		return false, errRuntimeSecurity
	}
	return instance.RuntimeSecurityProfile != "", nil
}

type runtimeSecuritySnapshot struct {
	gatewayID string
	identity  runtimeauth.Identity
	address   netip.Addr
	request   RuntimeAgentCreateGatewayRequest
	pod       *corev1.Pod
}

func (a *RuntimeSecurityAuthority) CurrentIdentity(ctx context.Context, id int) (runtimeauth.Identity, error) {
	snapshot, err := a.validatedSnapshot(ctx, id)
	return snapshot.identity, err
}

// Acquire one assignment observation, then validate it without further I/O.
// Lease peers and native targets must use values from this same snapshot.
func (a *RuntimeSecurityAuthority) validatedSnapshot(ctx context.Context, id int) (runtimeSecuritySnapshot, error) {
	if err := ctx.Err(); err != nil {
		return runtimeSecuritySnapshot{}, err
	}
	instance, err := a.instances.GetByIDContext(ctx, id)
	if err != nil || instance == nil {
		return runtimeSecuritySnapshot{}, errRuntimeSecurity
	}
	instanceCopy := *instance
	if a.checkTeamNetwork(ctx, &instanceCopy) != nil {
		return runtimeSecuritySnapshot{}, errRuntimeSecurity
	}
	if a.assignments != nil {
		cm, pods, err := a.assignments.Assignment(ctx, id)
		if err != nil {
			return runtimeSecuritySnapshot{}, errRuntimeSecurity
		}
		return validateRuntimeSecuritySnapshot(ctx, id, &instanceCopy, cm, pods)
	}
	cm, err := a.kube.CoreV1().ConfigMaps(a.namespace).Get(ctx, fmt.Sprintf("lite-instance-%d", id), metav1.GetOptions{})
	if err != nil {
		return runtimeSecuritySnapshot{}, errRuntimeSecurity
	}
	pods, err := a.kube.CoreV1().Pods(a.namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/runtime-protocol=container-slots-v1"})
	if err != nil || pods == nil {
		return runtimeSecuritySnapshot{}, errRuntimeSecurity
	}
	return validateRuntimeSecuritySnapshot(ctx, id, &instanceCopy, cm, pods.Items)
}

func validateRuntimeSecuritySnapshot(ctx context.Context, id int, instance *models.Instance, cm *corev1.ConfigMap, pods []corev1.Pod) (runtimeSecuritySnapshot, error) {
	fail := func() (runtimeSecuritySnapshot, error) { return runtimeSecuritySnapshot{}, errRuntimeSecurity }
	if instance == nil || instance.ID != id || instance.RuntimeSecurityProfile != RuntimeSecurityProfile || instance.InstanceMode != "lite" || instance.RuntimeType != "gateway" || (instance.Status != "running" && instance.Status != "creating") {
		return fail()
	}
	if cm == nil || cm.DeletionTimestamp != nil || cm.Data["desired_state"] != "running" || cm.Data["instance_id"] != strconv.Itoa(id) || cm.Data["user_id"] != strconv.Itoa(instance.UserID) || cm.Data["generation"] != strconv.Itoa(instance.RuntimeGeneration) || cm.Data["runtime_type"] != instance.Type {
		return fail()
	}
	var request RuntimeAgentCreateGatewayRequest
	if len(cm.Data["request"]) > 1<<20 || json.Unmarshal([]byte(cm.Data["request"]), &request) != nil || request.InstanceID != id || request.UserID != instance.UserID || request.Generation != instance.RuntimeGeneration || request.PodUID == "" || request.PodUID != cm.Data["pod_uid"] || request.AgentType != instance.Type || request.SecurityProfile != RuntimeSecurityProfile {
		return fail()
	}
	endpoint, err := url.Parse(cm.Data["endpoint"])
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Port() != "19090" || endpoint.RawQuery != "" || endpoint.Fragment != "" || (endpoint.Path != "" && endpoint.Path != "/") {
		return fail()
	}
	address, err := netip.ParseAddr(endpoint.Hostname())
	if err != nil || address.Zone() != "" {
		return fail()
	}
	matches := 0
	var matchedPod *corev1.Pod
	for _, pod := range pods {
		if pod.Status.PodIP != address.String() {
			continue
		}
		matches++
		if string(pod.UID) != request.PodUID || pod.DeletionTimestamp != nil || pod.Status.Phase != corev1.PodRunning || pod.Labels["clawmanager.io/runtime-security"] != RuntimeSecurityProfile {
			return fail()
		}
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = true
			}
		}
		if !ready {
			return fail()
		}
		matchedPod = pod.DeepCopy()
	}
	if matches != 1 {
		return fail()
	}
	if err := ctx.Err(); err != nil {
		return runtimeSecuritySnapshot{}, err
	}
	return runtimeSecuritySnapshot{gatewayID: cm.Data["gateway_id"], identity: runtimeauth.Identity{InstanceID: id, UserID: instance.UserID, Generation: instance.RuntimeGeneration, PodUID: request.PodUID}, address: address, request: request, pod: matchedPod}, nil
}

// NativeGatewayCredential is a per-generation UI password. It is deliberately
// not a model/egress capability or the long-lived instance identity.
func (a *RuntimeSecurityAuthority) NativeGatewayCredential(ctx context.Context, id int) (string, error) {
	identity, err := a.CurrentIdentity(ctx, id)
	if err != nil {
		return "", err
	}
	return a.nativeCredential(identity), nil
}

// NativeGatewayCredentialForTarget binds the credential to the same validated
// assignment's literal Pod address and allocated native main/tool/browser block.
func (a *RuntimeSecurityAuthority) NativeGatewayCredentialForTarget(ctx context.Context, id int, target *url.URL) (string, error) {
	snapshot, err := a.validatedSnapshot(ctx, id)
	if err != nil {
		return "", err
	}
	if target == nil || target.User != nil || target.Opaque != "" || (target.Scheme != "http" && target.Scheme != "ws") {
		return "", errRuntimeSecurity
	}
	address, err := netip.ParseAddr(target.Hostname())
	if err != nil || address.Zone() != "" || address.Unmap() != snapshot.address.Unmap() {
		return "", errRuntimeSecurity
	}
	port, err := strconv.Atoi(target.Port())
	_, supported := NormalizeV2RuntimeType(snapshot.request.AgentType)
	start := snapshot.request.GatewayPort
	block := RuntimeGatewayPortBlockSize(snapshot.request.AgentType)
	if err != nil || !supported || start < 1024 || start > 65535-block+1 || port < start || port >= start+block {
		return "", errRuntimeSecurity
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return a.nativeCredential(snapshot.identity), nil
}

func (a *RuntimeSecurityAuthority) nativeCredential(identity runtimeauth.Identity) string {
	mac := hmac.New(sha256.New, a.key)
	fmt.Fprintf(mac, "clawmanager/native-ui/v1\x00%d\x00%d\x00%d\x00%s", identity.UserID, identity.InstanceID, identity.Generation, identity.PodUID)
	return "ui1_" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
