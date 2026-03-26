package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// NetworkPolicyService handles default network policy management for instances.
type NetworkPolicyService struct {
	client           *Client
	namespaceService *NamespaceService
}

// NewNetworkPolicyService creates a new network policy service.
func NewNetworkPolicyService() *NetworkPolicyService {
	return &NetworkPolicyService{
		client:           globalClient,
		namespaceService: NewNamespaceService(),
	}
}

// EnsureDefaultPolicy creates or updates the default egress restriction policy for an instance.
func (s *NetworkPolicyService) EnsureDefaultPolicy(ctx context.Context, userID, instanceID int, instanceName string) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	if _, err := s.namespaceService.EnsureNamespace(ctx, userID); err != nil {
		return fmt.Errorf("failed to ensure namespace: %w", err)
	}

	namespace := s.client.GetNamespace(userID)
	policyName := s.client.GetNetworkPolicyName(instanceID, instanceName)
	instanceLabel := fmt.Sprintf("%d", instanceID)

	policy := &networkingv1.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":           "clawreef",
				"instance-id":   instanceLabel,
				"instance-name": instanceName,
				"user-id":       fmt.Sprintf("%d", userID),
				"managed-by":    "clawreef",
				"policy-role":   "instance-default-egress",
			},
		},
		Spec: networkingv1.NetworkPolicySpec{
			PodSelector: metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app":         "clawreef",
					"instance-id": instanceLabel,
					"managed-by":  "clawreef",
				},
			},
			PolicyTypes: []networkingv1.PolicyType{
				networkingv1.PolicyTypeEgress,
			},
			Egress: []networkingv1.NetworkPolicyEgressRule{
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": "kube-system",
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: protocolPtr(corev1.ProtocolUDP),
							Port:     intstrPtr(53),
						},
						{
							Protocol: protocolPtr(corev1.ProtocolTCP),
							Port:     intstrPtr(53),
						},
					},
				},
				{
					To: []networkingv1.NetworkPolicyPeer{
						{
							NamespaceSelector: &metav1.LabelSelector{
								MatchLabels: map[string]string{
									"kubernetes.io/metadata.name": s.client.GetSystemNamespace(),
								},
							},
						},
					},
					Ports: []networkingv1.NetworkPolicyPort{
						{
							Protocol: protocolPtr(corev1.ProtocolTCP),
							Port:     intstrPtr(80),
						},
						{
							Protocol: protocolPtr(corev1.ProtocolTCP),
							Port:     intstrPtr(443),
						},
						{
							Protocol: protocolPtr(corev1.ProtocolTCP),
							Port:     intstrPtr(8443),
						},
						{
							Protocol: protocolPtr(corev1.ProtocolTCP),
							Port:     intstrPtr(9001),
						},
						{
							Protocol: protocolPtr(corev1.ProtocolTCP),
							Port:     intstrPtr(3128),
						},
					},
				},
			},
		},
	}

	existing, err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).Get(ctx, policyName, metav1.GetOptions{})
	if err == nil && existing != nil {
		policy.ResourceVersion = existing.ResourceVersion
		if _, err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).Update(ctx, policy, metav1.UpdateOptions{}); err != nil {
			return fmt.Errorf("failed to update network policy %s: %w", policyName, err)
		}
		return nil
	}
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to inspect network policy %s: %w", policyName, err)
	}

	if _, err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).Create(ctx, policy, metav1.CreateOptions{}); err != nil {
		return fmt.Errorf("failed to create network policy %s: %w", policyName, err)
	}

	return nil
}

// DeletePolicy deletes the default instance network policy if it exists.
func (s *NetworkPolicyService) DeletePolicy(ctx context.Context, userID, instanceID int, instanceName string) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	namespace := s.client.GetNamespace(userID)
	policyName := s.client.GetNetworkPolicyName(instanceID, instanceName)
	if err := s.client.Clientset.NetworkingV1().NetworkPolicies(namespace).Delete(ctx, policyName, metav1.DeleteOptions{}); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to delete network policy %s: %w", policyName, err)
	}
	return nil
}

func protocolPtr(protocol corev1.Protocol) *corev1.Protocol {
	return &protocol
}

func intstrPtr(port int) *intstr.IntOrString {
	value := intstr.FromInt(port)
	return &value
}
