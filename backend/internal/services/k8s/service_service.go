package k8s

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// ServiceService handles Kubernetes Service operations
type ServiceService struct {
	client           *Client
	namespaceService *NamespaceService
}

// NewServiceService creates a new Service service
func NewServiceService() *ServiceService {
	return &ServiceService{
		client:           globalClient,
		namespaceService: NewNamespaceService(),
	}
}

// ServiceConfig holds configuration for creating a service
type ServiceConfig struct {
	InstanceID      int
	InstanceName    string
	UserID          int
	ContainerPort   int32
	AdditionalPorts []int32
}

// ServiceInfo holds information about a created service
type ServiceInfo struct {
	Name       string
	Namespace  string
	ClusterIP  string
	NodePort   int32
	TargetPort int32
}

// CreateService creates a service for an instance.
func (s *ServiceService) CreateService(ctx context.Context, config ServiceConfig) (*ServiceInfo, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	serviceName := s.client.GetServiceName(config.InstanceID, config.InstanceName)
	namespace := s.client.GetNamespace(config.UserID)

	// Ensure namespace exists
	if _, err := s.namespaceService.EnsureNamespace(ctx, config.UserID); err != nil {
		return nil, fmt.Errorf("failed to ensure namespace: %w", err)
	}

	// Default container port
	targetPort := config.ContainerPort
	if targetPort == 0 {
		targetPort = 3001
	}

	servicePorts := []corev1.ServicePort{
		{
			Name:       "http",
			Port:       targetPort,
			TargetPort: intstr.FromInt(int(targetPort)),
			Protocol:   corev1.ProtocolTCP,
		},
	}

	for _, additionalPort := range config.AdditionalPorts {
		if additionalPort == 0 || additionalPort == targetPort {
			continue
		}

		servicePorts = append(servicePorts, corev1.ServicePort{
			Name:       fmt.Sprintf("tcp-%d", additionalPort),
			Port:       additionalPort,
			TargetPort: intstr.FromInt(int(additionalPort)),
			Protocol:   corev1.ProtocolTCP,
		})
	}

	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      serviceName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":           "clawreef",
				"instance-id":   fmt.Sprintf("%d", config.InstanceID),
				"instance-name": config.InstanceName,
				"user-id":       fmt.Sprintf("%d", config.UserID),
				"managed-by":    "clawreef",
			},
		},
		Spec: corev1.ServiceSpec{
			Type: corev1.ServiceTypeClusterIP,
			Selector: map[string]string{
				"instance-id": fmt.Sprintf("%d", config.InstanceID),
				"app":         "clawreef",
			},
			Ports: servicePorts,
		},
	}

	createdService, err := s.client.Clientset.CoreV1().Services(namespace).Create(ctx, service, metav1.CreateOptions{})
	if err != nil {
		if errors.IsAlreadyExists(err) {
			// Service already exists, get the existing one
			existingService, getErr := s.GetService(ctx, config.UserID, config.InstanceID)
			if getErr == nil && existingService != nil {
				return s.extractServiceInfo(existingService, targetPort), nil
			}
		}
		return nil, fmt.Errorf("failed to create service %s: %w", serviceName, err)
	}

	return &ServiceInfo{
		Name:       createdService.Name,
		Namespace:  createdService.Namespace,
		ClusterIP:  createdService.Spec.ClusterIP,
		NodePort:   0,
		TargetPort: targetPort,
	}, nil
}

// GetService gets a service by instance ID
func (s *ServiceService) GetService(ctx context.Context, userID, instanceID int) (*corev1.Service, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	namespace := s.client.GetNamespace(userID)
	selector := fmt.Sprintf("instance-id=%d", instanceID)

	services, err := s.client.Clientset.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: selector,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list services: %w", err)
	}

	if len(services.Items) == 0 {
		return nil, fmt.Errorf("service not found for instance %d", instanceID)
	}

	return &services.Items[0], nil
}

// GetServiceInfo gets service information for an instance
func (s *ServiceService) GetServiceInfo(ctx context.Context, userID, instanceID int, targetPort int32) (*ServiceInfo, error) {
	service, err := s.GetService(ctx, userID, instanceID)
	if err != nil {
		return nil, err
	}

	return s.extractServiceInfo(service, targetPort), nil
}

// DeleteService deletes a service
func (s *ServiceService) DeleteService(ctx context.Context, userID, instanceID int) error {
	if s.client == nil {
		return fmt.Errorf("k8s client not initialized")
	}

	service, err := s.GetService(ctx, userID, instanceID)
	if err != nil {
		// Service doesn't exist, nothing to delete
		if isNotFoundError(err) {
			return nil
		}
		return err
	}

	err = s.client.Clientset.CoreV1().Services(service.Namespace).Delete(ctx, service.Name, metav1.DeleteOptions{})
	if err != nil {
		return fmt.Errorf("failed to delete service %s: %w", service.Name, err)
	}

	return nil
}

// ServiceExists checks if a service exists
func (s *ServiceService) ServiceExists(ctx context.Context, userID, instanceID int) (bool, error) {
	_, err := s.GetService(ctx, userID, instanceID)
	if err != nil {
		if isNotFoundError(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// GetNodePort gets the NodePort for a service
func (s *ServiceService) GetNodePort(ctx context.Context, userID, instanceID int, targetPort int32) (int32, error) {
	service, err := s.GetService(ctx, userID, instanceID)
	if err != nil {
		return 0, err
	}

	nodePort := s.extractNodePort(service, targetPort)
	if nodePort == 0 {
		return 0, fmt.Errorf("node port not found for target port %d", targetPort)
	}

	return nodePort, nil
}

// extractServiceInfo extracts service information from a Kubernetes service
func (s *ServiceService) extractServiceInfo(service *corev1.Service, targetPort int32) *ServiceInfo {
	return &ServiceInfo{
		Name:       service.Name,
		Namespace:  service.Namespace,
		ClusterIP:  service.Spec.ClusterIP,
		NodePort:   s.extractNodePort(service, targetPort),
		TargetPort: targetPort,
	}
}

// extractNodePort extracts the NodePort for a specific target port
func (s *ServiceService) extractNodePort(service *corev1.Service, targetPort int32) int32 {
	for _, port := range service.Spec.Ports {
		if port.TargetPort.IntVal == targetPort || port.Port == targetPort {
			if port.NodePort != 0 {
				return port.NodePort
			}
		}
	}
	return 0
}

// GetClusterNodes gets all cluster node IPs
func (s *ServiceService) GetClusterNodes(ctx context.Context) ([]string, error) {
	if s.client == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	nodes, err := s.client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	var nodeIPs []string
	for _, node := range nodes.Items {
		// Prefer ExternalIP, fallback to InternalIP
		var externalIP, internalIP string
		for _, addr := range node.Status.Addresses {
			switch addr.Type {
			case corev1.NodeExternalIP:
				externalIP = addr.Address
			case corev1.NodeInternalIP:
				internalIP = addr.Address
			}
		}

		if externalIP != "" {
			nodeIPs = append(nodeIPs, externalIP)
		} else if internalIP != "" {
			nodeIPs = append(nodeIPs, internalIP)
		}
	}

	return nodeIPs, nil
}

// GetAccessEndpoint gets the best access endpoint for a service
// Returns nodeIP:nodePort for accessing the service from outside the cluster
func (s *ServiceService) GetAccessEndpoint(ctx context.Context, userID, instanceID int, targetPort int32) (string, error) {
	nodePort, err := s.GetNodePort(ctx, userID, instanceID, targetPort)
	if err != nil {
		return "", err
	}

	nodes, err := s.GetClusterNodes(ctx)
	if err != nil {
		return "", err
	}

	if len(nodes) == 0 {
		return "", fmt.Errorf("no cluster nodes found")
	}

	// Use the first available node
	return fmt.Sprintf("%s:%d", nodes[0], nodePort), nil
}
