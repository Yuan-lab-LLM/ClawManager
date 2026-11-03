package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/netip"
	"net/url"
	"reflect"
	"strconv"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
)

const browserNetworkAnnotation = "clawmanager.io/browser-network-policy"
const browserRuntimeAnnotation = "clawmanager.io/browser-runtime-scope"

// ProtectedGUITarget shares the same pinning boundary as managed native CDP.
func (s *BrowserWorkerService) ProtectedGUITarget(ctx context.Context, instance *models.Instance) (*url.URL, error) {
	if instance == nil || instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
		return nil, errRuntimeBrowser
	}
	pod, err := s.protectedNetworkSnapshot(ctx, instance)
	if err != nil {
		return nil, err
	}
	return &url.URL{Scheme: "http", Host: net.JoinHostPort(pod.Status.PodIP, "5801")}, nil
}

// Configure once before publishing the worker service to reconciliation loops.
func ConfigureBrowserRuntimeSecurity(worker *BrowserWorkerService, current runtimeauth.Lookup) error {
	if worker == nil || current == nil || worker.networkIdentity != nil {
		return errRuntimeBrowser
	}
	worker.networkIdentity = current
	return nil
}

func (s *BrowserWorkerService) protectedBrowserIdentity(ctx context.Context, instance *models.Instance) (runtimeauth.Identity, error) {
	if s.networkIdentity == nil {
		return runtimeauth.Identity{}, errRuntimeBrowser
	}
	id, err := s.networkIdentity(ctx, instance.ID)
	if err != nil || id.InstanceID != instance.ID || id.UserID != instance.UserID || id.Generation != instance.RuntimeGeneration || id.PodUID == "" {
		return runtimeauth.Identity{}, errRuntimeBrowser
	}
	return id, ctx.Err()
}

type runtimeBrowserScope struct {
	InstanceID int    `json:"instance_id"`
	UserID     int    `json:"user_id"`
	Generation int    `json:"generation"`
	PodUID     string `json:"pod_uid"`
}

func browserRuntimeScope(id runtimeauth.Identity) string {
	encoded, _ := json.Marshal(runtimeBrowserScope{id.InstanceID, id.UserID, id.Generation, id.PodUID})
	return string(encoded)
}

func (s *BrowserWorkerService) protectedNetworkPolicy(ctx context.Context, instance *models.Instance) (*networkingv1.NetworkPolicy, error) {
	if _, err := s.protectedBrowserIdentity(ctx, instance); err != nil {
		return nil, err
	}
	backendNamespace := envOr("POD_NAMESPACE", s.client.GetSystemNamespace())
	policy := desiredBrowserWorkerNetworkPolicy(s.name(instance.ID), s.namespace(), backendNamespace, "", browserWorkerLabels(instance))
	policy.Spec.PolicyTypes = []networkingv1.PolicyType{networkingv1.PolicyTypeIngress, networkingv1.PolicyTypeEgress}
	policy.Spec.Ingress = policy.Spec.Ingress[:1]
	policy.Spec.Ingress[0].Ports = append(policy.Spec.Ingress[0].Ports, networkingv1.NetworkPolicyPort{Protocol: protocolPtr(corev1.ProtocolTCP), Port: intstrPtr(9222)})
	// Chromium can reach only its trusted local proxy. That proxy authenticates
	// to backend:9004, which rechecks the assignment and each literal destination.
	// No public IPBlock remains usable by background pages after revocation.
	policy.Spec.Egress = []networkingv1.NetworkPolicyEgressRule{
		{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": backendNamespace}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": "clawmanager-app"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolTCP), Port: intstrPtr(9004)}}},
		{To: []networkingv1.NetworkPolicyPeer{{NamespaceSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"kubernetes.io/metadata.name": "kube-system"}}, PodSelector: &metav1.LabelSelector{MatchLabels: map[string]string{"k8s-app": "kube-dns"}}}}, Ports: []networkingv1.NetworkPolicyPort{{Protocol: protocolPtr(corev1.ProtocolUDP), Port: intstrPtr(53)}, {Protocol: protocolPtr(corev1.ProtocolTCP), Port: intstrPtr(53)}}},
	}
	return policy, nil
}

func browserPolicyHash(policy *networkingv1.NetworkPolicy) string {
	encoded, _ := json.Marshal(policy.Spec)
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

// Pin a single ready worker Pod after checking the desired Service, policy,
// runtime scope and completed rollout. Credentials are dialed to its literal IP,
// so a later Service/DNS change cannot redirect this operation's secret.
func (s *BrowserWorkerService) protectedNetworkSnapshot(ctx context.Context, instance *models.Instance) (*corev1.Pod, error) {
	id, err := s.protectedBrowserIdentity(ctx, instance)
	if err != nil {
		return nil, err
	}
	desired, err := s.protectedNetworkPolicy(ctx, instance)
	if err != nil {
		return nil, err
	}
	name, namespace := s.name(instance.ID), s.namespace()
	policy, err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || !reflect.DeepEqual(policy.Spec, desired.Spec) {
		return nil, errRuntimeBrowser
	}
	deployment, err := s.client.Clientset.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil || deployment.DeletionTimestamp != nil || deployment.Spec.Replicas == nil || *deployment.Spec.Replicas != 1 || deployment.Spec.Template.Annotations[browserNetworkAnnotation] != browserPolicyHash(desired) || deployment.Spec.Template.Annotations[browserRuntimeAnnotation] != browserRuntimeScope(id) || deployment.Status.ObservedGeneration != deployment.Generation || deployment.Status.UpdatedReplicas != 1 || deployment.Status.ReadyReplicas != 1 || deployment.Status.Replicas != 1 {
		return nil, errRuntimeBrowser
	}
	service, err := s.client.Clientset.CoreV1().Services(namespace).Get(ctx, name, metav1.GetOptions{})
	expectedSelector := map[string]string{"app": "clawbrowser", "instance-id": strconv.Itoa(instance.ID), "user-id": strconv.Itoa(instance.UserID)}
	if err != nil || service.DeletionTimestamp != nil || service.Spec.Type != corev1.ServiceTypeClusterIP || service.Spec.ExternalName != "" || len(service.Spec.ExternalIPs) != 0 || !reflect.DeepEqual(service.Spec.Selector, expectedSelector) {
		return nil, errRuntimeBrowser
	}
	cdp := false
	for _, port := range service.Spec.Ports {
		if port.Port == 9222 {
			if cdp || port.Protocol != corev1.ProtocolTCP || port.TargetPort != *intstrPtr(9222) && port.TargetPort.StrVal != "cdp" {
				return nil, errRuntimeBrowser
			}
			cdp = true
		}
	}
	if !cdp {
		return nil, errRuntimeBrowser
	}
	pods, err := s.client.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.Set(expectedSelector).String()})
	if err != nil {
		return nil, errRuntimeBrowser
	}
	var selected *corev1.Pod
	for index := range pods.Items {
		pod := &pods.Items[index]
		if pod.DeletionTimestamp != nil {
			continue
		}
		if selected != nil || pod.UID == "" || pod.Status.Phase != corev1.PodRunning || pod.Spec.HostNetwork || pod.Annotations[browserNetworkAnnotation] != browserPolicyHash(desired) || pod.Annotations[browserRuntimeAnnotation] != browserRuntimeScope(id) {
			return nil, errRuntimeBrowser
		}
		address, err := netip.ParseAddr(pod.Status.PodIP)
		if err != nil || address.Zone() != "" || !address.IsGlobalUnicast() || address.IsLinkLocalUnicast() {
			return nil, errRuntimeBrowser
		}
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = true
			}
		}
		if !ready {
			return nil, errRuntimeBrowser
		}
		selected = pod.DeepCopy()
	}
	if selected == nil {
		return nil, errRuntimeBrowser
	}
	current, err := s.protectedBrowserIdentity(ctx, instance)
	if err != nil || current != id {
		return nil, errRuntimeBrowser
	}
	return selected, ctx.Err()
}
