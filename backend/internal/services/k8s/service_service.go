package k8s

import (
	"context"
	"errors"
	"fmt"
	"sort"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
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
	Name        string
	Namespace   string
	ClusterIP   string
	NodePort    int32
	ServicePort int32
	TargetPort  int32
}

var errServicePortNotFound = errors.New("service port not found for target port")

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

	servicePorts := buildServicePorts(targetPort, config.AdditionalPorts)

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
		if apierrors.IsAlreadyExists(err) {
			// Service already exists, patch missing ports while preserving existing
			// selector, ClusterIP, type, and desktop port shape.
			existingService, getErr := s.GetService(ctx, config.UserID, config.InstanceID)
			if getErr == nil && existingService != nil {
				updatedService, updateErr := s.ensureServicePorts(ctx, existingService, servicePorts)
				if updateErr != nil {
					return nil, updateErr
				}
				return s.extractServiceInfo(ctx, updatedService, targetPort)
			}
		}
		return nil, fmt.Errorf("failed to create service %s: %w", serviceName, err)
	}

	return s.extractServiceInfo(ctx, createdService, targetPort)
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

	return s.extractServiceInfo(ctx, service, targetPort)
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

	servicePort, err := s.findServicePortForTarget(ctx, service, targetPort)
	if err != nil {
		return 0, err
	}
	if servicePort.NodePort == 0 {
		return 0, fmt.Errorf("node port not found for target port %d", targetPort)
	}

	return servicePort.NodePort, nil
}

// extractServiceInfo extracts service information from a Kubernetes service
func (s *ServiceService) extractServiceInfo(ctx context.Context, service *corev1.Service, targetPort int32) (*ServiceInfo, error) {
	servicePort, err := s.findServicePortForTarget(ctx, service, targetPort)
	if err != nil {
		return nil, err
	}

	return &ServiceInfo{
		Name:        service.Name,
		Namespace:   service.Namespace,
		ClusterIP:   service.Spec.ClusterIP,
		NodePort:    servicePort.NodePort,
		ServicePort: servicePort.Port,
		TargetPort:  targetPort,
	}, nil
}

func buildServicePorts(primaryPort int32, additionalPorts []int32) []corev1.ServicePort {
	if primaryPort == 0 {
		primaryPort = 3001
	}

	seen := map[int32]bool{primaryPort: true}
	servicePorts := []corev1.ServicePort{
		newServicePort(primaryPort, true),
	}

	for _, additionalPort := range additionalPorts {
		if additionalPort == 0 || seen[additionalPort] {
			continue
		}
		seen[additionalPort] = true
		servicePorts = append(servicePorts, newServicePort(additionalPort, false))
	}

	return servicePorts
}

func newServicePort(port int32, primary bool) corev1.ServicePort {
	name := fmt.Sprintf("tcp-%d", port)
	if port == 18789 {
		name = "control-ui"
	} else if primary {
		name = "http"
	}

	return corev1.ServicePort{
		Name:       name,
		Port:       port,
		TargetPort: intstr.FromInt(int(port)),
		Protocol:   corev1.ProtocolTCP,
	}
}

func (s *ServiceService) ensureServicePorts(ctx context.Context, service *corev1.Service, desiredPorts []corev1.ServicePort) (*corev1.Service, error) {
	updated := service.DeepCopy()
	changed := false

	for _, desiredPort := range desiredPorts {
		desiredTarget := int32(desiredPort.TargetPort.IntVal)
		if desiredTarget == 0 {
			desiredTarget = desiredPort.Port
		}

		if _, err := s.findServicePortForTarget(ctx, updated, desiredTarget); err == nil {
			continue
		} else if !errors.Is(err, errServicePortNotFound) {
			return nil, err
		}

		if err := validateServicePortAppend(updated, desiredPort); err != nil {
			return nil, err
		}

		updated.Spec.Ports = append(updated.Spec.Ports, desiredPort)
		changed = true
	}

	if !changed {
		return service, nil
	}

	patched, err := s.client.Clientset.CoreV1().Services(updated.Namespace).Update(ctx, updated, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to patch service %s: %w", updated.Name, err)
	}

	return patched, nil
}

func validateServicePortAppend(service *corev1.Service, desiredPort corev1.ServicePort) error {
	for _, existingPort := range service.Spec.Ports {
		if desiredPort.Name != "" && existingPort.Name == desiredPort.Name {
			return fmt.Errorf("duplicate service port name %q maps to a different target port", desiredPort.Name)
		}
		if existingPort.Protocol == desiredPort.Protocol && existingPort.Port == desiredPort.Port {
			return fmt.Errorf("service port %d/%s already maps to a different target port", desiredPort.Port, desiredPort.Protocol)
		}
	}

	return nil
}

func (s *ServiceService) findServicePortForTarget(ctx context.Context, service *corev1.Service, targetPort int32) (*corev1.ServicePort, error) {
	if targetPort == 0 {
		targetPort = 3001
	}
	if err := validateServicePortNames(service); err != nil {
		return nil, err
	}

	var matches []corev1.ServicePort
	for _, servicePort := range service.Spec.Ports {
		resolvedTargetPort, err := s.resolveServiceTargetPort(ctx, service, servicePort)
		if err != nil {
			return nil, err
		}
		if resolvedTargetPort == targetPort {
			matches = append(matches, servicePort)
		}
	}

	if len(matches) == 0 {
		return nil, fmt.Errorf("%w %d", errServicePortNotFound, targetPort)
	}
	if len(matches) > 1 {
		sort.Slice(matches, func(i, j int) bool {
			if matches[i].Port == matches[j].Port {
				return matches[i].Name < matches[j].Name
			}
			return matches[i].Port < matches[j].Port
		})
		return nil, fmt.Errorf("multiple service ports map to target port %d: %s and %s", targetPort, matches[0].Name, matches[1].Name)
	}

	match := matches[0]
	return &match, nil
}

func validateServicePortNames(service *corev1.Service) error {
	seen := map[string]struct{}{}
	for _, servicePort := range service.Spec.Ports {
		if servicePort.Name == "" {
			continue
		}
		if _, ok := seen[servicePort.Name]; ok {
			return fmt.Errorf("duplicate service port name %q", servicePort.Name)
		}
		seen[servicePort.Name] = struct{}{}
	}

	return nil
}

func (s *ServiceService) resolveServiceTargetPort(ctx context.Context, service *corev1.Service, servicePort corev1.ServicePort) (int32, error) {
	switch servicePort.TargetPort.Type {
	case intstr.Int:
		if servicePort.TargetPort.IntVal == 0 {
			return servicePort.Port, nil
		}
		return int32(servicePort.TargetPort.IntVal), nil
	case intstr.String:
		return s.resolveNamedTargetPort(ctx, service, servicePort.TargetPort.StrVal)
	default:
		return 0, fmt.Errorf("unsupported targetPort type for service port %q", servicePort.Name)
	}
}

func (s *ServiceService) resolveNamedTargetPort(ctx context.Context, service *corev1.Service, name string) (int32, error) {
	if name == "" {
		return 0, fmt.Errorf("empty named targetPort on service port")
	}
	if len(service.Spec.Selector) == 0 {
		return 0, fmt.Errorf("cannot resolve named targetPort %q without service selector", name)
	}

	pods, err := s.client.Clientset.CoreV1().Pods(service.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: labels.SelectorFromSet(labels.Set(service.Spec.Selector)).String(),
	})
	if err != nil {
		return 0, fmt.Errorf("failed to resolve named targetPort %q: %w", name, err)
	}

	resolvedPorts := map[int32]struct{}{}
	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			for _, containerPort := range container.Ports {
				if containerPort.Name == name {
					resolvedPorts[containerPort.ContainerPort] = struct{}{}
				}
			}
		}
	}

	if len(resolvedPorts) == 0 {
		return 0, fmt.Errorf("named targetPort %q did not resolve to a selected pod container port", name)
	}
	if len(resolvedPorts) > 1 {
		values := make([]int, 0, len(resolvedPorts))
		for port := range resolvedPorts {
			values = append(values, int(port))
		}
		sort.Ints(values)
		return 0, fmt.Errorf("named targetPort %q resolved to conflicting selected pod ports %v", name, values)
	}

	for port := range resolvedPorts {
		return port, nil
	}

	return 0, fmt.Errorf("named targetPort %q did not resolve to a selected pod container port", name)
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
