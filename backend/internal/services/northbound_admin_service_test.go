package services

import (
	"bytes"
	"context"
	"net"
	"testing"

	"clawreef/internal/models"
	k8ssvc "clawreef/internal/services/k8s"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/kubernetes/fake"
)

func validNorthboundSettings() *models.NorthboundAdminSettings {
	return &models.NorthboundAdminSettings{ChallengeTTLSeconds: 60, AccessTokenTTLSeconds: 1800, RefreshTokenTTLSeconds: 604800, CoreRequestTimeoutSeconds: 30, ChallengeRatePerMinute: 10, LoginRatePerMinute: 5, AccountLoginRatePerMinute: 5, CreateRatePerMinute: 10, QueryRatePerMinute: 120, ShareRatePerMinute: 10, MaxPendingOperations: 5, OperationTickMilliseconds: 1000, OperationLeaseSeconds: 30, OperationMaxAttempts: 5, AllowedLiteTypes: []string{"openclaw"}, AllowedProTypes: []string{"opencode"}, LiteCPUCores: 2, LiteMemoryGB: 4, LiteDiskGB: 5, ProCPUCores: 4, ProMemoryGB: 8, ProDiskGB: 50, WorkBuddyProCPUCores: 4, WorkBuddyProMemoryGB: 8, WorkBuddyProDiskGB: 40}
}

func TestValidateNorthboundSettingsPreservesSafeRanges(t *testing.T) {
	settings := validNorthboundSettings()
	if err := validateNorthboundSettings(settings); err != nil {
		t.Fatal(err)
	}
	settings.LiteDiskGB = 0
	if err := validateNorthboundSettings(settings); err == nil {
		t.Fatal("expected invalid Lite disk to fail")
	}
}

func TestKubernetesNorthboundControllerUpdatesOnlyExternalNodePort(t *testing.T) {
	service := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-northbound-gateway", Namespace: "clawmanager-system"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Name: "https", Port: 9443, TargetPort: intstr.FromInt(9443), NodePort: 32343}}}}
	clientset := fake.NewSimpleClientset(service)
	controller := NewKubernetesNorthboundController(&k8ssvc.Client{Clientset: clientset, Namespace: "clawmanager"})
	old, err := controller.SetExternalNodePort(context.Background(), 32443)
	if err != nil {
		t.Fatal(err)
	}
	if old != 32343 {
		t.Fatalf("old port=%d", old)
	}
	updated, err := clientset.CoreV1().Services("clawmanager-system").Get(context.Background(), service.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if updated.Spec.Ports[0].NodePort != 32443 || updated.Spec.Ports[0].Port != 9443 || updated.Spec.Ports[0].TargetPort.IntVal != 9443 {
		t.Fatalf("unexpected service ports: %+v", updated.Spec.Ports[0])
	}
}

func TestKubernetesNorthboundControllerRejectsNodePortCollision(t *testing.T) {
	gateway := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-northbound-gateway", Namespace: "clawmanager-system"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Name: "https", Port: 9443, NodePort: 32343}}}}
	other := &corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: "other", Namespace: "default"}, Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeNodePort, Ports: []corev1.ServicePort{{Name: "https", Port: 443, NodePort: 32443}}}}
	controller := NewKubernetesNorthboundController(&k8ssvc.Client{Clientset: fake.NewSimpleClientset(gateway, other), Namespace: "clawmanager"})
	if _, err := controller.SetExternalNodePort(context.Background(), 32443); err == nil {
		t.Fatal("expected NodePort collision")
	}
}

func TestManagedCertificatePreparationDoesNotChangeActiveCertificate(t *testing.T) {
	oldCA, _, oldCert, oldKey, _, err := generateManagedCertificate([]string{"old.example.internal"}, nil, 365)
	if err != nil {
		t.Fatal(err)
	}
	activeTLS := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-northbound-gateway-tls", Namespace: "clawmanager-system"}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{corev1.TLSCertKey: oldCert, corev1.TLSPrivateKeyKey: oldKey}}
	activeCA := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-northbound-gateway-ca", Namespace: "clawmanager-system"}, Data: map[string][]byte{"ca.crt": oldCA}}
	deployment := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-northbound-gateway", Namespace: "clawmanager-system"}}
	clientset := fake.NewSimpleClientset(activeTLS, activeCA, deployment)
	controller := NewKubernetesNorthboundController(&k8ssvc.Client{Clientset: clientset, Namespace: "clawmanager"})
	if err := controller.PrepareManagedCertificate(context.Background(), CertificatePrepareRequest{DNSNames: []string{"new.example.internal"}, IPAddresses: []string{net.ParseIP("10.0.0.8").String()}, ValidDays: 825}); err != nil {
		t.Fatal(err)
	}
	unchanged, err := clientset.CoreV1().Secrets("clawmanager-system").Get(context.Background(), activeTLS.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(unchanged.Data[corev1.TLSCertKey], oldCert) {
		t.Fatal("prepare changed active certificate")
	}
	prepared, err := clientset.CoreV1().Secrets("clawmanager-system").Get(context.Background(), "clawmanager-northbound-gateway-tls-next", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(prepared.Data[corev1.TLSCertKey], oldCert) {
		t.Fatal("prepared certificate was not renewed")
	}
	if err := controller.ActivateManagedCertificate(context.Background()); err != nil {
		t.Fatal(err)
	}
	activated, err := clientset.CoreV1().Secrets("clawmanager-system").Get(context.Background(), activeTLS.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(activated.Data[corev1.TLSCertKey], prepared.Data[corev1.TLSCertKey]) {
		t.Fatal("prepared certificate was not activated")
	}
	restarted, err := clientset.AppsV1().Deployments("clawmanager-system").Get(context.Background(), deployment.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if restarted.Spec.Template.Annotations["clawmanager.io/northbound-certificate-activated-at"] == "" {
		t.Fatal("gateway rollout annotation was not set")
	}
}

func TestManagedCertificateRenewalKeepsManagedCA(t *testing.T) {
	caCert, caKey, _, _, _, err := generateManagedCertificate([]string{"first.example.internal"}, nil, 365)
	if err != nil {
		t.Fatal(err)
	}
	managed := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "clawmanager-northbound-managed-ca", Namespace: "clawmanager-system"}, Type: corev1.SecretTypeTLS, Data: map[string][]byte{corev1.TLSCertKey: caCert, corev1.TLSPrivateKeyKey: caKey}}
	clientset := fake.NewSimpleClientset(managed)
	controller := NewKubernetesNorthboundController(&k8ssvc.Client{Clientset: clientset, Namespace: "clawmanager"})
	if err := controller.PrepareManagedCertificate(context.Background(), CertificatePrepareRequest{DNSNames: []string{"renewed.example.internal"}, ValidDays: 365}); err != nil {
		t.Fatal(err)
	}
	preparedCA, err := controller.PreparedCA(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(preparedCA, caCert) {
		t.Fatal("renewal replaced the managed CA")
	}
}
