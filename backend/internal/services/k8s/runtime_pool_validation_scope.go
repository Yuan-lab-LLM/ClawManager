package k8s

import (
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"net"
	"net/url"
	"path"
	"reflect"
	"strings"
)

// RuntimePoolValidationScope is constructed by the rollout controller from its
// frozen source and fresh Kubernetes observation; it is never an HTTP DTO.
type RuntimePoolValidationScope struct {
	Namespace, DeploymentName, DeploymentUID, SourceName, SourceUID, Operation, PodUID, Endpoint, ImageRef, WorkspaceRoot string
}

// VerifyPoolValidationScope proves that the exception to business admission can
// reach only this operation's ephemeral, unschedulable native supervisor.
func VerifyPoolValidationScope(ctx context.Context, client kubernetes.Interface, scope RuntimePoolValidationScope) (*corev1.Pod, error) {
	if client == nil || scope.Namespace == "" || scope.DeploymentName == "" || scope.DeploymentUID == "" || scope.SourceUID == "" || scope.SourceName == "" || scope.Operation == "" || scope.PodUID == "" || runtimeImageDigest(scope.ImageRef) == "" || !path.IsAbs(scope.WorkspaceRoot) || path.Clean(scope.WorkspaceRoot) != scope.WorkspaceRoot || scope.WorkspaceRoot == "/" {
		return nil, fmt.Errorf("complete isolated validation scope required")
	}
	endpoint, err := url.Parse(scope.Endpoint)
	if err != nil || endpoint.Scheme != "https" || endpoint.User != nil || endpoint.Path != "" || endpoint.RawQuery != "" || endpoint.Fragment != "" || net.ParseIP(endpoint.Hostname()) == nil || endpoint.Port() != "19090" {
		return nil, fmt.Errorf("validation requires direct authenticated Pod TLS")
	}
	d, err := client.AppsV1().Deployments(scope.Namespace).Get(ctx, scope.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(d.UID) != scope.DeploymentUID || d.DeletionTimestamp != nil || d.Spec.Paused || d.Spec.Replicas == nil || *d.Spec.Replicas != 1 || d.Annotations["clawmanager.io/source-uid"] != scope.SourceUID {
		return nil, fmt.Errorf("validation controller identity changed")
	}
	source, err := client.AppsV1().Deployments(scope.Namespace).Get(ctx, scope.SourceName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if string(source.UID) != scope.SourceUID {
		return nil, fmt.Errorf("validation source UID changed")
	}
	expected, err := buildPoolValidationDeployment(source, RuntimeRolloutTarget{Namespace: scope.Namespace, Name: scope.SourceName, UID: scope.SourceUID}, scope.DeploymentName, scope.ImageRef, scope.Operation)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(d.Spec.Template, expected.Spec.Template) || !reflect.DeepEqual(d.Spec.Selector, expected.Spec.Selector) {
		return nil, fmt.Errorf("validation template differs from its frozen source transformation")
	}
	for _, labels := range []map[string]string{d.Labels, d.Spec.Template.Labels} {
		if labels[runtimePoolRoleLabel] != poolValidationRole || labels[runtimeSchedulingLabel] != "false" || labels[runtimeUpgradeIDLabel] != scope.Operation || labels["clawmanager.io/runtime-type"] != "openclaw" || labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || labels["clawmanager.io/runtime-security"] != RuntimeNetworkSecurityProfile {
			return nil, fmt.Errorf("validation native isolation labels changed")
		}
	}
	pods, err := client.CoreV1().Pods(scope.Namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set{"app": scope.DeploymentName}.String()})
	if err != nil {
		return nil, err
	}
	for i := range pods.Items {
		p := &pods.Items[i]
		if string(p.UID) != scope.PodUID {
			continue
		}
		if p.DeletionTimestamp != nil || p.Status.Phase != corev1.PodRunning || !net.ParseIP(endpoint.Hostname()).Equal(net.ParseIP(p.Status.PodIP)) {
			return nil, fmt.Errorf("validation Pod identity or address changed")
		}
		owner := metav1.GetControllerOf(p)
		if owner == nil || owner.Kind != "ReplicaSet" {
			return nil, fmt.Errorf("validation Pod controller unavailable")
		}
		rs, err := client.AppsV1().ReplicaSets(scope.Namespace).Get(ctx, owner.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		parent := metav1.GetControllerOf(rs)
		if rs.UID != owner.UID || parent == nil || parent.Kind != "Deployment" || parent.UID != d.UID {
			return nil, fmt.Errorf("validation ReplicaSet ownership changed")
		}
		original, current := rs.Spec.Template.DeepCopy(), d.Spec.Template.DeepCopy()
		delete(original.Labels, "pod-template-hash")
		delete(current.Labels, "pod-template-hash")
		if !reflect.DeepEqual(original, current) {
			return nil, fmt.Errorf("validation ReplicaSet template differs from controller")
		}
		for key, value := range d.Spec.Template.Annotations {
			if p.Annotations[key] != value {
				return nil, fmt.Errorf("validation Pod security annotation changed")
			}
		}
		for key, value := range p.Annotations {
			if (strings.Contains(key, "apparmor") || strings.Contains(key, "seccomp")) && d.Spec.Template.Annotations[key] != value {
				return nil, fmt.Errorf("unexpected validation security annotation")
			}
		}
		expectedSpec, actualSpec := d.Spec.Template.Spec.DeepCopy(), p.Spec.DeepCopy()
		if err := canonicalizeValidationAppArmor(expectedSpec, d.Spec.Template.Annotations); err != nil {
			return nil, err
		}
		if err := canonicalizeValidationAppArmor(actualSpec, d.Spec.Template.Annotations); err != nil {
			return nil, err
		}
		if err := normalizeValidationPodSpec(expectedSpec); err != nil {
			return nil, err
		}
		if err := normalizeValidationPodSpec(actualSpec); err != nil {
			return nil, err
		}
		if !reflect.DeepEqual(expectedSpec, actualSpec) {
			return nil, fmt.Errorf("validation Pod execution contract differs from controller")
		}
		for _, spec := range []*corev1.PodSpec{&d.Spec.Template.Spec, &p.Spec} {
			if err := verifyEphemeralNativeValidation(spec, scope, d.Spec.Template.Labels["clawmanager.io/sandbox-profile"] == RuntimeNFSRootlessProfile); err != nil {
				return nil, err
			}
		}
		if len(d.Spec.Template.Spec.Containers) != len(p.Spec.Containers) {
			return nil, fmt.Errorf("validation worker membership changed")
		}
		for index, c := range expectedSpec.Containers {
			actual := actualSpec.Containers[index]
			if c.Name != actual.Name || !reflect.DeepEqual(c.Env, actual.Env) || !reflect.DeepEqual(c.Resources, actual.Resources) || !reflect.DeepEqual(c.SecurityContext, actual.SecurityContext) || !reflect.DeepEqual(c.VolumeMounts, actual.VolumeMounts) {
				return nil, fmt.Errorf("validation worker differs from its frozen native template")
			}
		}
		return p, nil
	}
	return nil, fmt.Errorf("owned validation Pod UID unavailable")
}
func verifyEphemeralNativeValidation(spec *corev1.PodSpec, scope RuntimePoolValidationScope, rootless bool) error {
	if (spec.ShareProcessNamespace != nil && *spec.ShareProcessNamespace) || (!rootless && (spec.HostUsers == nil || *spec.HostUsers)) {
		return fmt.Errorf("validation sandbox namespaces changed")
	}
	if spec.HostNetwork || spec.HostPID || spec.HostIPC || spec.AutomountServiceAccountToken == nil || *spec.AutomountServiceAccountToken || len(spec.Containers) < 2 {
		return fmt.Errorf("validation native Pod isolation changed")
	}
	volumes := map[string]corev1.Volume{}
	for _, v := range spec.Volumes {
		if v.EmptyDir == nil && (v.Secret == nil || v.Secret.SecretName != "clawmanager-lite-agent-tls") {
			return fmt.Errorf("validation contains non-ephemeral business storage")
		}
		volumes[v.Name] = v
	}
	workspace, ok := volumes["workspaces"]
	if !ok || workspace.EmptyDir == nil {
		return fmt.Errorf("validation workspace must be emptyDir")
	}
	for i, c := range append(append([]corev1.Container{}, spec.Containers...), spec.InitContainers...) {
		if len(c.EnvFrom) > 0 || len(c.VolumeDevices) > 0 || (c.SecurityContext != nil && ((c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged) || (c.SecurityContext.AllowPrivilegeEscalation != nil && *c.SecurityContext.AllowPrivilegeEscalation))) {
			return fmt.Errorf("unsupported validation native privilege or environment source")
		}
		for _, m := range c.VolumeMounts {
			v, ok := volumes[m.Name]
			if !ok || (v.Secret != nil && !m.ReadOnly) {
				return fmt.Errorf("validation writable credential mount")
			}
		}
		if i >= len(spec.Containers) {
			continue
		}
		name := "runtime"
		if i > 0 {
			name = fmt.Sprintf("slot-%d", i-1)
		}
		if c.Name != name || c.Image != scope.ImageRef {
			return fmt.Errorf("validation slot binary or index changed")
		}
		env := map[string]corev1.EnvVar{}
		for _, e := range c.Env {
			if _, exists := env[e.Name]; exists {
				return fmt.Errorf("validation has duplicate native environment")
			}
			env[e.Name] = e
		}
		if env["RUNTIME_WORKSPACE_ROOT"].Value != scope.WorkspaceRoot || env["CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME"].Value != scope.DeploymentName || env["CLAWMANAGER_RUNTIME_IMAGE_REF"].Value != scope.ImageRef || env["CLAWMANAGER_RUNTIME_UPGRADE_ID"].Value != "" {
			return fmt.Errorf("validation native identity changed")
		}
		if i > 0 && env["RUNTIME_NETWORK_SECURITY_PROFILE"].Value != RuntimeNetworkSecurityProfile {
			return fmt.Errorf("validation worker private network disabled")
		}
	}
	return nil
}

func normalizeValidationPodSpec(spec *corev1.PodSpec) error {
	if spec.EnableServiceLinks == nil {
		yes := true
		spec.EnableServiceLinks = &yes
	}
	if spec.DeprecatedServiceAccount != "" && spec.DeprecatedServiceAccount != spec.ServiceAccountName {
		return fmt.Errorf("validation deprecated service account differs")
	}
	spec.DeprecatedServiceAccount = ""
	spec.NodeName = ""
	if spec.Priority != nil && *spec.Priority != 0 {
		return fmt.Errorf("validation Pod priority changed")
	}
	spec.Priority = nil
	if spec.PreemptionPolicy != nil && *spec.PreemptionPolicy != corev1.PreemptLowerPriority {
		return fmt.Errorf("validation Pod preemption changed")
	}
	spec.PreemptionPolicy = nil
	var tolerations []corev1.Toleration
	for _, t := range spec.Tolerations {
		if (t.Key == "node.kubernetes.io/not-ready" || t.Key == "node.kubernetes.io/unreachable") && t.Operator == corev1.TolerationOpExists && t.Effect == corev1.TaintEffectNoExecute && t.Value == "" && t.TolerationSeconds != nil && *t.TolerationSeconds == 300 {
			continue
		}
		tolerations = append(tolerations, t)
	}
	spec.Tolerations = tolerations
	return nil
}

// Kubernetes materializes this existing native sandbox annotation on Pods.
// Preserve its exact meaning; an explicit conflicting typed profile is drift.
func canonicalizeValidationAppArmor(spec *corev1.PodSpec, annotations map[string]string) error {
	for _, containers := range [][]corev1.Container{spec.Containers, spec.InitContainers} {
		for i := range containers {
			c := &containers[i]
			if annotations["container.apparmor.security.beta.kubernetes.io/"+c.Name] != "unconfined" {
				continue
			}
			want := &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
			if c.SecurityContext == nil {
				c.SecurityContext = &corev1.SecurityContext{}
			}
			if c.SecurityContext.AppArmorProfile == nil {
				c.SecurityContext.AppArmorProfile = want
			} else if !reflect.DeepEqual(c.SecurityContext.AppArmorProfile, want) {
				return fmt.Errorf("validation typed AppArmor profile conflicts with approved annotation")
			}
		}
	}
	return nil
}
