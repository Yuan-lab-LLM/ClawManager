package k8s

import (
	"clawreef/internal/config"
	"context"
	"fmt"
	"strconv"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const runtimeSlotSocketVolume = "runtime-slot-sockets"
const runtimeSlotSocketDirectory = "/run/clawmanager-slots"
const runtimeSlotServiceAccountName = "clawmanager-lite-runtime"
const RuntimeNetworkSecurityProfile = "isolated-network-v1"

func runtimeSlotPortBlock(runtimeType string) int {
	switch runtimeType {
	case "openclaw", "opencode":
		return 3
	default:
		return 1
	}
}

func configureRuntimeSlots(deployment *appsv1.Deployment, spec RuntimeDeploymentSpec, portStart int) {
	pod := &deployment.Spec.Template.Spec
	no := false
	pod.AutomountServiceAccountToken = &no
	pod.ServiceAccountName = runtimeSlotServiceAccountName
	if spec.PodResources != nil {
		pod.Resources = spec.PodResources.DeepCopy()
	}
	if spec.PodUserNamespaces || spec.NetworkIsolation {
		pod.HostUsers = &no
	}
	if pod.SecurityContext == nil {
		pod.SecurityContext = &corev1.PodSecurityContext{}
	}
	pod.SecurityContext.SeccompProfile = &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	if deployment.Spec.Template.Labels == nil {
		deployment.Spec.Template.Labels = map[string]string{}
	}
	deployment.Spec.Template.Labels["clawmanager.io/runtime-protocol"] = "container-slots-v1"
	deployment.Labels["clawmanager.io/runtime-protocol"] = "container-slots-v1"
	if spec.NetworkIsolation {
		deployment.Spec.Template.Labels["clawmanager.io/runtime-security"] = RuntimeNetworkSecurityProfile
		deployment.Labels["clawmanager.io/runtime-security"] = RuntimeNetworkSecurityProfile
	}
	// This shared volume contains only trusted coordinator/slot control sockets.
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: runtimeSlotSocketVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}})
	coordinator := &pod.Containers[0]
	coordinator.SecurityContext = &corev1.SecurityContext{Privileged: &no, AllowPrivilegeEscalation: &no, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"CHOWN", "DAC_OVERRIDE", "FOWNER"}}}
	coordinator.Command = []string{"/usr/bin/tini", "--", "/usr/local/bin/clawmanager-agent"}
	coordinator.Env = append(coordinator.Env, fieldRefEnv("POD_UID", "metadata.uid"))
	upsertEnvVar(coordinator, "RUNTIME_AGENT_ROLE", "pool")
	upsertEnvVar(coordinator, "RUNTIME_AGENT_LISTEN_ADDR", ":19090")
	upsertEnvVar(coordinator, "RUNTIME_SLOT_COUNT", strconv.Itoa(spec.SlotCount))
	upsertEnvVar(coordinator, "RUNTIME_SLOT_SOCKET_DIR", runtimeSlotSocketDirectory)
	upsertEnvVar(coordinator, "RUNTIME_GATEWAY_CAPACITY", strconv.Itoa(spec.SlotCount))
	block := runtimeSlotPortBlock(spec.RuntimeType)
	upsertEnvVar(coordinator, "RUNTIME_GATEWAY_PORT_BLOCK_SIZE", strconv.Itoa(block))
	coordinator.VolumeMounts = append(coordinator.VolumeMounts, corev1.VolumeMount{Name: runtimeSlotSocketVolume, MountPath: runtimeSlotSocketDirectory})
	const tlsVolume = "runtime-agent-tls"
	mode := int32(0400)
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: tlsVolume, VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: runtimeSlotTLSSecretName, DefaultMode: &mode, Items: []corev1.KeyToPath{{Key: corev1.TLSCertKey, Path: corev1.TLSCertKey}, {Key: corev1.TLSPrivateKeyKey, Path: corev1.TLSPrivateKeyKey}}}}})
	coordinator.VolumeMounts = append(coordinator.VolumeMounts, corev1.VolumeMount{Name: tlsVolume, MountPath: runtimeSlotTLSDirectory, ReadOnly: true})
	upsertEnvVar(coordinator, "RUNTIME_AGENT_TLS_CERT_FILE", runtimeSlotTLSDirectory+"/tls.crt")
	upsertEnvVar(coordinator, "RUNTIME_AGENT_TLS_KEY_FILE", runtimeSlotTLSDirectory+"/tls.key")
	setRuntimeAgentSecret(coordinator, spec.AgentTokenSecretName, true)
	// Slots inherit business/runtime configuration, never backend report credentials.
	template := coordinator.DeepCopy()
	coordinator.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromString("agent"), Scheme: corev1.URISchemeHTTPS}}, PeriodSeconds: 2, TimeoutSeconds: 3, FailureThreshold: 3}
	coordinator.Resources = runtimeCoordinatorResources(spec.PodResources != nil)
	for i := 0; i < spec.SlotCount; i++ {
		slot := template.DeepCopy()
		slot.Name = fmt.Sprintf("slot-%d", i)
		slot.Ports = nil
		slot.ReadinessProbe = nil
		slot.Resources = *spec.SlotResources.DeepCopy()
		if slot.Resources.Requests == nil {
			slot.Resources.Requests = corev1.ResourceList{}
		}
		if slot.Resources.Limits == nil {
			slot.Resources.Limits = corev1.ResourceList{}
		}
		for key, value := range (corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("256Mi")}) {
			if _, ok := slot.Resources.Requests[key]; !ok {
				slot.Resources.Requests[key] = value
			}
		}
		for key, value := range (corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")}) {
			if _, ok := slot.Resources.Limits[key]; !ok {
				slot.Resources.Limits[key] = value
			}
		}

		slot.Env = filterRuntimeSlotEnv(slot.Env)
		mounts := slot.VolumeMounts[:0]
		for _, mount := range slot.VolumeMounts {
			if mount.Name != tlsVolume {
				mounts = append(mounts, mount)
			}
		}
		slot.VolumeMounts = mounts
		upsertEnvVar(slot, "RUNTIME_AGENT_ROLE", "slot")
		upsertEnvVar(slot, "RUNTIME_SLOT_INDEX", strconv.Itoa(i))
		upsertEnvVar(slot, "RUNTIME_SLOT_SOCKET", fmt.Sprintf("%s/slot-%d.sock", runtimeSlotSocketDirectory, i))
		upsertEnvVar(slot, "RUNTIME_AGENT_DATA_DIR", "/var/lib/clawmanager-slot")
		upsertEnvVar(slot, "RUNTIME_GATEWAY_CAPACITY", "1")
		start := portStart + i*block
		for _, prefix := range []string{"RUNTIME_GATEWAY_PORT_", "CLAWMANAGER_GATEWAY_PORT_"} {
			upsertEnvVar(slot, prefix+"START", strconv.Itoa(start))
			upsertEnvVar(slot, prefix+"END", strconv.Itoa(start+block-1))
		}
		// Docker's default AppArmor/proc masks forbid bubblewrap's user-namespace
		// mount setup. Only the trusted supervisor receives this outer context;
		// the gateway gets a new PID proc, no capabilities and its own seccomp.
		procMount := corev1.UnmaskedProcMount
		deployment.Spec.Template.Annotations["container.apparmor.security.beta.kubernetes.io/"+slot.Name] = "unconfined"
		slot.SecurityContext = &corev1.SecurityContext{Privileged: &no, AllowPrivilegeEscalation: &no, ProcMount: &procMount, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"SETUID", "SETGID", "CHOWN", "DAC_OVERRIDE", "FOWNER", "KILL", "SYS_PTRACE"}}, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeUnconfined}}
		if spec.NetworkIsolation {
			slot.SecurityContext.Capabilities.Add = append(slot.SecurityContext.Capabilities.Add, "SYS_ADMIN", "NET_ADMIN")
			upsertEnvVar(slot, "RUNTIME_NETWORK_SECURITY_PROFILE", RuntimeNetworkSecurityProfile)
			upsertEnvVar(slot, "RUNTIME_NETWORK_SLOT_COUNT", strconv.Itoa(spec.SlotCount))
		}
		stateVolume := slot.Name + "-state"
		pod.Volumes = append(pod.Volumes, corev1.Volume{Name: stateVolume, VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
		slot.VolumeMounts = append(slot.VolumeMounts, corev1.VolumeMount{Name: stateVolume, MountPath: "/var/lib/clawmanager-slot"})
		pod.Containers = append(pod.Containers, *slot)
	}
}

func filterRuntimeSlotEnv(env []corev1.EnvVar) []corev1.EnvVar {
	out := make([]corev1.EnvVar, 0, len(env))
	for _, v := range env {
		if strings.HasPrefix(v.Name, "RUNTIME_AGENT_TLS_") {
			continue
		}
		switch v.Name {
		case "RUNTIME_AGENT_REPORT_TOKEN", "CLAWMANAGER_AGENT_REPORT_TOKEN", "RUNTIME_SLOT_COUNT", "RUNTIME_SLOT_SOCKET_DIR":
			continue
		}
		out = append(out, v)
	}
	return out
}

func setRuntimeAgentSecret(container *corev1.Container, name string, report bool) {
	if name == "" {
		return
	}
	for i := range container.Env {
		key := ""
		switch container.Env[i].Name {
		case "RUNTIME_AGENT_CONTROL_TOKEN", "CLAWMANAGER_AGENT_CONTROL_TOKEN":
			key = "control-token"
		case "RUNTIME_AGENT_REPORT_TOKEN", "CLAWMANAGER_AGENT_REPORT_TOKEN":
			if report {
				key = "report-token"
			}
		}
		if key != "" {
			container.Env[i].Value = ""
			container.Env[i].ValueFrom = &corev1.EnvVarSource{SecretKeyRef: &corev1.SecretKeySelector{LocalObjectReference: corev1.LocalObjectReference{Name: name}, Key: key}}
		}
	}
}

// validateRuntimeSlotStorage verifies the live volume, not a StorageClass name.
// Node inventory is deliberately cluster-wide: an unavailable second node must
// not silently turn a multi-node deployment into a local-storage deployment.
func (s *runtimeDeploymentService) validateRuntimeSlotStorage(ctx context.Context, spec RuntimeDeploymentSpec) error {
	switch spec.RuntimeType {
	case "openclaw", "hermes", "opencode", "deepseek-harness":
	default:
		return fmt.Errorf("container slots do not support runtime %q", spec.RuntimeType)
	}
	start := runtimeGatewayPortValue(spec.GatewayPortStart, defaultGatewayPortStart)
	end := runtimeGatewayPortValue(spec.GatewayPortEnd, defaultGatewayPortEnd)
	block := runtimeSlotPortBlock(spec.RuntimeType)
	if start < 1024 || end > 65535 || end < start || spec.SlotCount > (end-start+1)/block {
		return fmt.Errorf("runtime slot count exceeds gateway port budget")
	}
	claim := strings.TrimSpace(spec.WorkspacePVCClaimName)
	if claim == "" {
		return fmt.Errorf("container slots require a bound workspace PVC")
	}
	nodes, err := s.client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("inspect runtime pool node inventory: %w", err)
	}
	if len(nodes.Items) == 0 {
		return fmt.Errorf("runtime pool requires known node inventory")
	}
	pvc, err := s.client.CoreV1().PersistentVolumeClaims(spec.Namespace).Get(ctx, claim, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("inspect runtime workspace PVC: %w", err)
	}
	if pvc.DeletionTimestamp != nil || pvc.Status.Phase != corev1.ClaimBound || pvc.Spec.VolumeName == "" {
		return fmt.Errorf("runtime workspace PVC must be bound and available")
	}
	pv, err := s.client.CoreV1().PersistentVolumes().Get(ctx, pvc.Spec.VolumeName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("inspect runtime workspace PV: %w", err)
	}
	if pv.DeletionTimestamp != nil || pv.Status.Phase != corev1.VolumeBound || pv.Spec.ClaimRef == nil || pv.Spec.ClaimRef.Namespace != spec.Namespace || pv.Spec.ClaimRef.Name != claim || (pvc.UID != "" && pv.Spec.ClaimRef.UID != pvc.UID) {
		return fmt.Errorf("runtime workspace PV binding is unavailable or inconsistent")
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName == "" || *pvc.Spec.StorageClassName != pv.Spec.StorageClassName {
		return fmt.Errorf("runtime workspace storage class is unavailable or inconsistent")
	}
	sc, err := s.client.StorageV1().StorageClasses().Get(ctx, *pvc.Spec.StorageClassName, metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("inspect runtime workspace storage class: %w", err)
	}
	longhorn := sc.Provisioner == "driver.longhorn.io" && pv.Spec.CSI != nil && pv.Spec.CSI.Driver == "driver.longhorn.io"
	if len(nodes.Items) > 1 {
		if !longhorn || !runtimeVolumeHasMode(pvc.Spec.AccessModes, corev1.ReadWriteMany) || !runtimeVolumeHasMode(pv.Spec.AccessModes, corev1.ReadWriteMany) {
			return fmt.Errorf("multi-node container slots require Longhorn RWX workspace storage")
		}
		return nil
	}
	if longhorn {
		return nil
	}
	if (pv.Spec.Local != nil || pv.Spec.HostPath != nil) && pv.Spec.NodeAffinity != nil && pv.Spec.NodeAffinity.Required != nil && len(pv.Spec.NodeAffinity.Required.NodeSelectorTerms) > 0 {
		return nil
	}
	return fmt.Errorf("single-node container slots require Longhorn or a local PV with node affinity")
}

func runtimeVolumeHasMode(modes []corev1.PersistentVolumeAccessMode, want corev1.PersistentVolumeAccessMode) bool {
	for _, mode := range modes {
		if mode == want {
			return true
		}
	}
	return false
}

func syncRuntimeSlotImageAndIdentity(deployment *appsv1.Deployment, coordinator *corev1.Container) {
	if deployment.Spec.Template.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" {
		return
	}
	keys := []string{"CLAWMANAGER_RUNTIME_DEPLOYMENT_NAME", "CLAWMANAGER_DEPLOYMENT_NAME", "CLAWMANAGER_RUNTIME_IMAGE_REF", "CLAWMANAGER_RUNTIME_IMAGE_DIGEST", "CLAWMANAGER_RUNTIME_UPGRADE_ID", "CLAWMANAGER_CONTROL_UI_ORIGIN", "CLAWMANAGER_TRUSTED_PROXY_CIDRS", "CLAWMANAGER_HERMES_PROXY_SOURCE", "CLAWMANAGER_HERMES_DESKTOP_WEB_ENABLED"}
	for i := range deployment.Spec.Template.Spec.Containers {
		slot := &deployment.Spec.Template.Spec.Containers[i]
		if !strings.HasPrefix(slot.Name, "slot-") {
			continue
		}
		slot.Image = coordinator.Image
		for _, key := range keys {
			removeEnvVar(slot, key)
			for _, env := range coordinator.Env {
				if env.Name == key {
					slot.Env = append(slot.Env, *env.DeepCopy())
					break
				}
			}
		}
	}
}

// RuntimeSlotDeploymentSpecs builds only explicitly opted-in container pools.
// Limits are configurable budgets, not measured density guarantees.
func RuntimeSlotDeploymentSpecs(cfg config.RuntimePoolConfig, agentSecretName string) ([]RuntimeDeploymentSpec, error) {
	if cfg.NetworkIsolation && (cfg.ContainerSlots < 1 || !cfg.PodUserNamespaces) {
		return nil, fmt.Errorf("network-isolated slots require Pod user namespaces and configured container slots")
	}
	if cfg.ContainerSlots == 0 {
		return nil, nil
	}
	if cfg.ContainerSlots < 1 || cfg.ContainerSlots > 256 {
		return nil, fmt.Errorf("runtime containerSlots must be between 1 and 256")
	}
	if strings.TrimSpace(agentSecretName) == "" {
		return nil, fmt.Errorf("container slots require an agent credential Secret")
	}
	budgets := cfg.SlotResources
	defaults := config.RuntimeSlotResources{CPURequest: "100m", MemoryRequest: "256Mi", CPULimit: "2", MemoryLimit: "4Gi"}
	if budgets.CPURequest == "" {
		budgets.CPURequest = defaults.CPURequest
	}
	if budgets.MemoryRequest == "" {
		budgets.MemoryRequest = defaults.MemoryRequest
	}
	if budgets.CPULimit == "" {
		budgets.CPULimit = defaults.CPULimit
	}
	if budgets.MemoryLimit == "" {
		budgets.MemoryLimit = defaults.MemoryLimit
	}
	resources, err := parseRuntimeResourceBudget(budgets, "slot")
	if err != nil {
		return nil, err
	}
	var podResources *corev1.ResourceRequirements
	if cfg.PodResources != (config.RuntimeSlotResources{}) {
		parsed, err := parseRuntimeResourceBudget(cfg.PodResources, "Pod")
		if err != nil {
			return nil, err
		}
		podResources = &parsed
		coordinator := runtimeCoordinatorResources(true)
		for key, request := range resources.Requests {
			total := request.DeepCopy()
			total.Mul(int64(cfg.ContainerSlots))
			total.Add(coordinator.Requests[key])
			if total.Cmp(parsed.Requests[key]) > 0 {
				return nil, fmt.Errorf("aggregate runtime %s requests exceed Pod request", key)
			}
			slotLimit, coordinatorLimit := resources.Limits[key], coordinator.Limits[key]
			if slotLimit.Cmp(parsed.Limits[key]) > 0 || coordinatorLimit.Cmp(parsed.Limits[key]) > 0 {
				return nil, fmt.Errorf("runtime container %s limit exceeds Pod limit", key)
			}
		}
	}
	namespace := strings.TrimSpace(cfg.Namespace)
	if namespace == "" {
		namespace = "clawmanager-system"
	}
	var specs []RuntimeDeploymentSpec
	for _, runtime := range []struct{ kind, image string }{{"openclaw", cfg.OpenClawImage}, {"hermes", cfg.HermesImage}, {"opencode", cfg.OpenCodeImage}, {"deepseek-harness", cfg.DeepSeekHarnessImage}} {
		if strings.TrimSpace(runtime.image) == "" {
			return nil, fmt.Errorf("container slots require %s image", runtime.kind)
		}
		specs = append(specs, RuntimeDeploymentSpec{Name: runtime.kind + "-runtime", Namespace: namespace, RuntimeType: runtime.kind, Image: runtime.image, Replicas: 1, WorkspacePVCClaimName: cfg.WorkspacePVCClaimName, WorkspaceMountPath: cfg.WorkspaceRoot, GatewayPortStart: cfg.GatewayPortStart, GatewayPortEnd: cfg.GatewayPortEnd, SlotCount: cfg.ContainerSlots, SlotResources: *resources.DeepCopy(), PodResources: podResources.DeepCopy(), PodUserNamespaces: cfg.PodUserNamespaces, NetworkIsolation: cfg.NetworkIsolation, AgentTokenSecretName: agentSecretName, HermesDesktopWeb: runtime.kind == "hermes"})
	}
	return specs, nil
}

func runtimeCoordinatorResources(shared bool) corev1.ResourceRequirements {
	memoryRequest := "128Mi"
	if shared {
		memoryRequest = "512Mi"
	}
	return corev1.ResourceRequirements{
		Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse(memoryRequest)},
		Limits:   corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")},
	}
}

func parseRuntimeResourceBudget(budgets config.RuntimeSlotResources, label string) (corev1.ResourceRequirements, error) {
	resources := corev1.ResourceRequirements{Requests: corev1.ResourceList{}, Limits: corev1.ResourceList{}}
	for _, entry := range []struct {
		value  string
		key    corev1.ResourceName
		target corev1.ResourceList
	}{{budgets.CPURequest, corev1.ResourceCPU, resources.Requests}, {budgets.MemoryRequest, corev1.ResourceMemory, resources.Requests}, {budgets.CPULimit, corev1.ResourceCPU, resources.Limits}, {budgets.MemoryLimit, corev1.ResourceMemory, resources.Limits}} {
		q, err := resource.ParseQuantity(entry.value)
		if err != nil || q.Sign() <= 0 {
			return resources, fmt.Errorf("invalid runtime %s %s resource budget %q", label, entry.key, entry.value)
		}
		entry.target[entry.key] = q
	}
	for key, request := range resources.Requests {
		if request.Cmp(resources.Limits[key]) > 0 {
			return resources, fmt.Errorf("runtime %s %s request exceeds limit", label, key)
		}
	}
	return resources, nil
}

// Slot coordinator readiness remains TLS through standby, activation and restore.
func setRuntimeSlotTLSReadiness(container *corev1.Container) {
	for _, env := range container.Env {
		if env.Name == "RUNTIME_AGENT_ROLE" && env.Value == "pool" {
			container.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{HTTPGet: &corev1.HTTPGetAction{Path: "/readyz", Port: intstr.FromString("agent"), Scheme: corev1.URISchemeHTTPS}}, PeriodSeconds: 2, TimeoutSeconds: 3, FailureThreshold: 3}
			return
		}
	}
}
