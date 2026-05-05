package k8s

import (
	"context"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func newTestServiceService(objects ...runtime.Object) *ServiceService {
	client := &Client{
		Clientset: fake.NewSimpleClientset(objects...),
		Namespace: "clawmanager",
	}
	return &ServiceService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}
}

func testNamespace(userID int) string {
	return (&Client{Namespace: "clawmanager"}).GetNamespace(userID)
}

func testService(userID, instanceID int, ports []corev1.ServicePort) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1ObjectMeta(instanceID, userID),
		Spec: corev1.ServiceSpec{
			Type:      corev1.ServiceTypeClusterIP,
			ClusterIP: "10.0.0.42",
			Selector: map[string]string{
				"app":         "clawreef",
				"instance-id": "42",
			},
			Ports: ports,
		},
	}
}

func metav1ObjectMeta(instanceID, userID int) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      "clawreef-42-instance-42-svc",
		Namespace: testNamespace(userID),
		Labels: map[string]string{
			"app":           "clawreef",
			"instance-id":   "42",
			"instance-name": "instance-42",
			"user-id":       "7",
			"managed-by":    "clawreef",
		},
	}
}

func testPodForNamedPort(userID int, portName string, port int32) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clawreef-42-instance-42",
			Namespace: testNamespace(userID),
			Labels: map[string]string{
				"app":         "clawreef",
				"instance-id": "42",
			},
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "desktop",
					Ports: []corev1.ContainerPort{
						{Name: portName, ContainerPort: port, Protocol: corev1.ProtocolTCP},
					},
				},
			},
		},
	}
}

func TestGetServiceInfoRejectsDesktopOnlyServiceForControlUI(t *testing.T) {
	ctx := context.Background()
	service := testService(7, 42, []corev1.ServicePort{
		{Name: "http", Port: 3001, TargetPort: intstr.FromInt(3001), Protocol: corev1.ProtocolTCP},
	})
	serviceService := newTestServiceService(service)

	if _, err := serviceService.GetServiceInfo(ctx, 7, 42, 18789); err == nil {
		t.Fatalf("GetServiceInfo() error = nil, want control-ui target port rejection")
	}

	info, err := serviceService.GetServiceInfo(ctx, 7, 42, 3001)
	if err != nil {
		t.Fatalf("GetServiceInfo() desktop error = %v", err)
	}
	if info.ServicePort != 3001 || info.TargetPort != 3001 {
		t.Fatalf("ServiceInfo ports = service:%d target:%d, want 3001/3001", info.ServicePort, info.TargetPort)
	}
}

func TestGetServiceInfoRejectsServicePortMatchWhenTargetPortDiffers(t *testing.T) {
	ctx := context.Background()
	service := testService(7, 42, []corev1.ServicePort{
		{Name: "control-ui", Port: 18789, TargetPort: intstr.FromInt(3001), Protocol: corev1.ProtocolTCP},
	})
	serviceService := newTestServiceService(service)

	_, err := serviceService.GetServiceInfo(ctx, 7, 42, 18789)
	if err == nil {
		t.Fatalf("GetServiceInfo() error = nil, want port 18789 targetPort 3001 rejection for control-ui")
	}
}

func TestGetServiceInfoDistinguishesDialPortFromPodTargetPort(t *testing.T) {
	ctx := context.Background()
	service := testService(7, 42, []corev1.ServicePort{
		{Name: "control-ui", Port: 3001, TargetPort: intstr.FromInt(18789), Protocol: corev1.ProtocolTCP},
	})
	serviceService := newTestServiceService(service)

	if _, err := serviceService.GetServiceInfo(ctx, 7, 42, 3001); err == nil {
		t.Fatalf("GetServiceInfo() desktop error = nil, want targetPort 18789 not to validate desktop 3001")
	}

	info, err := serviceService.GetServiceInfo(ctx, 7, 42, 18789)
	if err != nil {
		t.Fatalf("GetServiceInfo() control-ui error = %v", err)
	}
	if info.ServicePort != 3001 || info.TargetPort != 18789 {
		t.Fatalf("ServiceInfo ports = service:%d target:%d, want 3001/18789", info.ServicePort, info.TargetPort)
	}
}

func TestGetServiceInfoResolvesNamedTargetPortOrRejectsUnverifiableName(t *testing.T) {
	ctx := context.Background()
	service := testService(7, 42, []corev1.ServicePort{
		{Name: "control-ui", Port: 18789, TargetPort: intstr.FromString("control-ui"), Protocol: corev1.ProtocolTCP},
	})

	t.Run("resolves to selected pod container port", func(t *testing.T) {
		serviceService := newTestServiceService(service, testPodForNamedPort(7, "control-ui", 18789))
		info, err := serviceService.GetServiceInfo(ctx, 7, 42, 18789)
		if err != nil {
			t.Fatalf("GetServiceInfo() error = %v", err)
		}
		if info.ServicePort != 18789 || info.TargetPort != 18789 {
			t.Fatalf("ServiceInfo ports = service:%d target:%d, want 18789/18789", info.ServicePort, info.TargetPort)
		}
	})

	t.Run("rejects when name cannot be resolved", func(t *testing.T) {
		serviceService := newTestServiceService(service)
		if _, err := serviceService.GetServiceInfo(ctx, 7, 42, 18789); err == nil {
			t.Fatalf("GetServiceInfo() error = nil, want unresolved named targetPort rejection")
		}
	})
}

func TestGetServiceInfoRejectsDuplicateConflictingPortNames(t *testing.T) {
	ctx := context.Background()
	service := testService(7, 42, []corev1.ServicePort{
		{Name: "http", Port: 3001, TargetPort: intstr.FromInt(3001), Protocol: corev1.ProtocolTCP},
		{Name: "http", Port: 18789, TargetPort: intstr.FromInt(18789), Protocol: corev1.ProtocolTCP},
	})
	serviceService := newTestServiceService(service)

	_, err := serviceService.GetServiceInfo(ctx, 7, 42, 18789)
	if err == nil {
		t.Fatalf("GetServiceInfo() error = nil, want duplicate port name rejection")
	}
	if !strings.Contains(err.Error(), "duplicate service port name") {
		t.Fatalf("GetServiceInfo() error = %v, want duplicate service port name", err)
	}
}

func TestCreateServicePatchesExistingServicePortWithoutChangingDesktopPort(t *testing.T) {
	ctx := context.Background()
	service := testService(7, 42, []corev1.ServicePort{
		{Name: "desktop", Port: 3001, TargetPort: intstr.FromInt(3001), Protocol: corev1.ProtocolTCP},
	})
	serviceService := newTestServiceService(service)

	info, err := serviceService.CreateService(ctx, ServiceConfig{
		InstanceID:    42,
		InstanceName:  "instance-42",
		UserID:        7,
		ContainerPort: 18789,
	})
	if err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}
	if info.ServicePort != 18789 || info.TargetPort != 18789 {
		t.Fatalf("ServiceInfo ports = service:%d target:%d, want 18789/18789", info.ServicePort, info.TargetPort)
	}

	updated, err := serviceService.GetService(ctx, 7, 42)
	if err != nil {
		t.Fatalf("GetService() error = %v", err)
	}
	if updated.Spec.ClusterIP != "10.0.0.42" {
		t.Fatalf("ClusterIP = %q, want preserved 10.0.0.42", updated.Spec.ClusterIP)
	}
	if updated.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("Type = %q, want ClusterIP", updated.Spec.Type)
	}
	if updated.Spec.Selector["instance-id"] != "42" || updated.Spec.Selector["app"] != "clawreef" {
		t.Fatalf("Selector = %#v, want preserved selector", updated.Spec.Selector)
	}

	desktopFound := false
	controlFound := false
	for _, port := range updated.Spec.Ports {
		switch port.Name {
		case "desktop":
			desktopFound = port.Port == 3001 && port.TargetPort.IntVal == 3001
		case "control-ui":
			controlFound = port.Port == 18789 && port.TargetPort.IntVal == 18789
		}
	}
	if !desktopFound {
		t.Fatalf("existing desktop 3001 service port/name/targetPort was not preserved: %#v", updated.Spec.Ports)
	}
	if !controlFound {
		t.Fatalf("control-ui 18789 service port was not added: %#v", updated.Spec.Ports)
	}
}

func TestCreateServiceCreatesDesktopAndControlUIPortsWhenRequested(t *testing.T) {
	ctx := context.Background()
	serviceService := newTestServiceService()

	if _, err := serviceService.CreateService(ctx, ServiceConfig{
		InstanceID:      42,
		InstanceName:    "instance-42",
		UserID:          7,
		ContainerPort:   3001,
		AdditionalPorts: []int32{18789},
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	created, err := serviceService.GetService(ctx, 7, 42)
	if err != nil {
		t.Fatalf("GetService() error = %v", err)
	}
	if created.Spec.Type != corev1.ServiceTypeClusterIP {
		t.Fatalf("Type = %q, want ClusterIP", created.Spec.Type)
	}
	if created.Spec.Selector["instance-id"] != "42" || created.Spec.Selector["app"] != "clawreef" {
		t.Fatalf("Selector = %#v, want instance-id/app selector", created.Spec.Selector)
	}
	if len(created.Spec.Ports) != 2 {
		t.Fatalf("len(Ports) = %d, want 2: %#v", len(created.Spec.Ports), created.Spec.Ports)
	}
	for _, want := range []int32{3001, 18789} {
		if _, err := serviceService.GetServiceInfo(ctx, 7, 42, want); err != nil {
			t.Fatalf("GetServiceInfo(%d) error = %v", want, err)
		}
	}
}

func TestCreateServiceWithoutAdditionalPortsDoesNotExposeControlUI(t *testing.T) {
	ctx := context.Background()
	serviceService := newTestServiceService()

	if _, err := serviceService.CreateService(ctx, ServiceConfig{
		InstanceID:    42,
		InstanceName:  "ubuntu-desktop",
		UserID:        7,
		ContainerPort: 3001,
	}); err != nil {
		t.Fatalf("CreateService() error = %v", err)
	}

	created, err := serviceService.GetService(ctx, 7, 42)
	if err != nil {
		t.Fatalf("GetService() error = %v", err)
	}
	if created.Spec.Selector["instance-id"] != "42" || created.Spec.Selector["app"] != "clawreef" {
		t.Fatalf("Selector = %#v, want instance-id/app selector", created.Spec.Selector)
	}
	if len(created.Spec.Ports) != 1 {
		t.Fatalf("len(Ports) = %d, want only desktop port: %#v", len(created.Spec.Ports), created.Spec.Ports)
	}
	if _, err := serviceService.GetServiceInfo(ctx, 7, 42, 3001); err != nil {
		t.Fatalf("GetServiceInfo(3001) error = %v", err)
	}
	if _, err := serviceService.GetServiceInfo(ctx, 7, 42, 18789); err == nil {
		t.Fatalf("GetServiceInfo(18789) error = nil, want no control-ui exposure without additional port")
	}
}
