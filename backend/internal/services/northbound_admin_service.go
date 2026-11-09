package services

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sort"
	"strings"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	k8ssvc "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var ErrNorthboundSettingsConflict = errors.New("northbound settings were changed by another administrator")

var allowedNorthboundScopes = map[string]struct{}{
	"teams:read": {}, "teams:create": {}, "teams:manage": {}, "teams:delete": {},
	"teams:tasks:read": {}, "teams:tasks:write": {}, "teams:files:read": {}, "teams:files:write": {},
	"team-templates:read": {}, "team-templates:write": {}, "team-templates:generate": {},
	"lite-instances:create": {}, "lite-instances:read": {}, "pro-instances:create": {}, "pro-instances:read": {},
	"lite-instances:restart": {}, "lite-instances:reset": {}, "pro-instances:restart": {}, "pro-instances:reset": {},
	"lite-instances:delete": {}, "pro-instances:delete": {},
	"owners:usage:read":                {},
	"lite-instances:share-link:manage": {}, "lite-instances:share-link:reset": {},
}

var allowedLiteRuntimeTypes = map[string]struct{}{
	"openclaw": {}, "hermes": {}, "opencode": {}, "deepseek-harness": {}, "workbuddy": {},
}

var allowedProRuntimeTypes = map[string]struct{}{
	"openclaw": {}, "hermes": {}, "opencode": {}, "deepseek-harness": {}, "workbuddy": {},
}

type NorthboundCertificateStatus struct {
	Available        bool      `json:"available"`
	Managed          bool      `json:"managed"`
	Subject          string    `json:"subject,omitempty"`
	Issuer           string    `json:"issuer,omitempty"`
	DNSNames         []string  `json:"dns_names"`
	IPAddresses      []string  `json:"ip_addresses"`
	NotBefore        time.Time `json:"not_before,omitempty"`
	NotAfter         time.Time `json:"not_after,omitempty"`
	DaysRemaining    int       `json:"days_remaining,omitempty"`
	SHA256           string    `json:"sha256,omitempty"`
	Error            string    `json:"error,omitempty"`
	ManagementNote   string    `json:"management_note"`
	Prepared         bool      `json:"prepared"`
	PreparedNotAfter time.Time `json:"prepared_not_after,omitempty"`
}

type NorthboundClusterStatus struct {
	Available        bool                        `json:"available"`
	Namespace        string                      `json:"namespace,omitempty"`
	ServiceName      string                      `json:"service_name,omitempty"`
	ExternalNodePort int                         `json:"external_node_port,omitempty"`
	GatewayPort      int                         `json:"gateway_port"`
	CorePort         int                         `json:"core_port"`
	Error            string                      `json:"error,omitempty"`
	Certificate      NorthboundCertificateStatus `json:"certificate"`
}

type NorthboundAdminOverview struct {
	Settings *models.NorthboundAdminSettings    `json:"settings"`
	Callers  []models.NorthboundCallerPolicy    `json:"callers"`
	Cluster  NorthboundClusterStatus            `json:"cluster"`
	Stats    *models.NorthboundOperationalStats `json:"stats"`
	Audit    []models.NorthboundSettingsAudit   `json:"audit"`
}

type NorthboundClusterController interface {
	Status(context.Context) NorthboundClusterStatus
	SetExternalNodePort(context.Context, int) (int, error)
	PublicCA(context.Context) ([]byte, error)
	PreparedCA(context.Context) ([]byte, error)
	PrepareManagedCertificate(context.Context, CertificatePrepareRequest) error
	ActivateManagedCertificate(context.Context) error
}

type CertificatePrepareRequest struct {
	DNSNames    []string `json:"dns_names"`
	IPAddresses []string `json:"ip_addresses"`
	ValidDays   int      `json:"valid_days"`
}

type NorthboundAdminService struct {
	repo       *repository.NorthboundRepository
	users      repository.UserRepository
	controller NorthboundClusterController
}

func NewNorthboundAdminService(repo *repository.NorthboundRepository, users repository.UserRepository, controller NorthboundClusterController) *NorthboundAdminService {
	return &NorthboundAdminService{repo: repo, users: users, controller: controller}
}

func (s *NorthboundAdminService) Overview(ctx context.Context) (*NorthboundAdminOverview, error) {
	settings, err := s.repo.GetAdminSettings()
	if err != nil {
		return nil, err
	}
	callers, err := s.repo.ListCallerPolicies()
	if err != nil {
		return nil, err
	}
	cluster := NorthboundClusterStatus{GatewayPort: 9443, CorePort: 9002, Error: "Kubernetes control is unavailable"}
	if s.controller != nil {
		cluster = s.controller.Status(ctx)
	}
	stats, err := s.repo.GetOperationalStats()
	if err != nil {
		return nil, err
	}
	audit, err := s.repo.ListSettingsAudit(30)
	if err != nil {
		return nil, err
	}
	return &NorthboundAdminOverview{Settings: settings, Callers: callers, Cluster: cluster, Stats: stats, Audit: audit}, nil
}

func (s *NorthboundAdminService) SaveSettings(ctx context.Context, actorUserID int, item *models.NorthboundAdminSettings) (*models.NorthboundAdminSettings, error) {
	if item.OwnerTeamCreationUserID < 0 {
		return nil, fmt.Errorf("invalid Owner Team creation account")
	}
	if item.OwnerTeamCreationUserID > 0 {
		user, err := s.users.GetByID(item.OwnerTeamCreationUserID)
		if err != nil {
			return nil, err
		}
		if user == nil || !user.IsActive {
			return nil, fmt.Errorf("Owner Team creation account must be active")
		}
	}
	if err := validateNorthboundSettings(item); err != nil {
		return nil, err
	}
	before, err := s.repo.GetAdminSettings()
	if err != nil {
		return nil, err
	}
	updated, err := s.repo.UpdateAdminSettings(ctx, item, item.Version, actorUserID)
	if err != nil {
		return nil, err
	}
	if !updated {
		return nil, ErrNorthboundSettingsConflict
	}
	after, err := s.repo.GetAdminSettings()
	if err != nil {
		return nil, err
	}
	_ = s.repo.RecordSettingsAudit(ctx, actorUserID, "settings.update", before, after)
	return after, nil
}

func (s *NorthboundAdminService) SetExternalNodePort(ctx context.Context, actorUserID, nodePort int) (*NorthboundAdminOverview, error) {
	if nodePort < 30000 || nodePort > 32767 {
		return nil, errors.New("external NodePort must be between 30000 and 32767")
	}
	if s.controller == nil {
		return nil, errors.New("Kubernetes control is unavailable")
	}
	oldPort, err := s.controller.SetExternalNodePort(ctx, nodePort)
	if err != nil {
		return nil, err
	}
	settings, err := s.repo.GetAdminSettings()
	if err != nil {
		_, _ = s.controller.SetExternalNodePort(ctx, oldPort)
		return nil, err
	}
	settings.ExternalNodePort = nodePort
	updated, err := s.repo.UpdateAdminSettings(ctx, settings, settings.Version, actorUserID)
	if err != nil || !updated {
		_, _ = s.controller.SetExternalNodePort(ctx, oldPort)
		if err != nil {
			return nil, err
		}
		return nil, ErrNorthboundSettingsConflict
	}
	_ = s.repo.RecordSettingsAudit(ctx, actorUserID, "external_node_port.update", map[string]int{"port": oldPort}, map[string]int{"port": nodePort})
	return s.Overview(ctx)
}

func (s *NorthboundAdminService) SaveCallerPolicy(ctx context.Context, actorUserID int, item *models.NorthboundCallerPolicy) (*models.NorthboundCallerPolicy, error) {
	if item.UserID <= 0 {
		return nil, errors.New("caller user is required")
	}
	user, err := s.users.GetByID(item.UserID)
	if err != nil {
		return nil, err
	}
	if user == nil {
		return nil, errors.New("caller user was not found")
	}
	item.Scopes = normalizeValues(item.Scopes)
	for _, scope := range item.Scopes {
		if _, ok := allowedNorthboundScopes[scope]; !ok {
			return nil, fmt.Errorf("unsupported northbound scope %q", scope)
		}
	}
	if item.Enabled && len(item.Scopes) == 0 {
		return nil, errors.New("an enabled caller must have at least one scope")
	}
	before, _ := s.repo.GetCallerPolicy(item.UserID)
	if err := s.repo.UpsertCallerPolicy(ctx, item, actorUserID); err != nil {
		return nil, err
	}
	if before == nil || before.Enabled != item.Enabled || strings.Join(before.Scopes, ",") != strings.Join(item.Scopes, ",") {
		if err := s.repo.RevokeActiveSessionsByUser(ctx, item.UserID); err != nil {
			return nil, err
		}
	}
	after, err := s.repo.GetCallerPolicy(item.UserID)
	if err != nil {
		return nil, err
	}
	_ = s.repo.RecordSettingsAudit(ctx, actorUserID, "caller_policy.update", before, after)
	after.Username, after.Email = user.Username, user.Email
	return after, nil
}

func (s *NorthboundAdminService) PublicCA(ctx context.Context) ([]byte, error) {
	if s.controller == nil {
		return nil, errors.New("Kubernetes control is unavailable")
	}
	return s.controller.PublicCA(ctx)
}

func (s *NorthboundAdminService) PreparedCA(ctx context.Context) ([]byte, error) {
	if s.controller == nil {
		return nil, errors.New("Kubernetes control is unavailable")
	}
	return s.controller.PreparedCA(ctx)
}
func (s *NorthboundAdminService) PrepareManagedCertificate(ctx context.Context, actorUserID int, request CertificatePrepareRequest) (*NorthboundAdminOverview, error) {
	if s.controller == nil {
		return nil, errors.New("Kubernetes control is unavailable")
	}
	if err := s.controller.PrepareManagedCertificate(ctx, request); err != nil {
		return nil, err
	}
	_ = s.repo.RecordSettingsAudit(ctx, actorUserID, "certificate.prepare", nil, map[string]any{"dns_names": request.DNSNames, "ip_addresses": request.IPAddresses, "valid_days": request.ValidDays})
	return s.Overview(ctx)
}
func (s *NorthboundAdminService) ActivateManagedCertificate(ctx context.Context, actorUserID int) (*NorthboundAdminOverview, error) {
	if s.controller == nil {
		return nil, errors.New("Kubernetes control is unavailable")
	}
	if err := s.controller.ActivateManagedCertificate(ctx); err != nil {
		return nil, err
	}
	_ = s.repo.RecordSettingsAudit(ctx, actorUserID, "certificate.activate", nil, map[string]bool{"activated": true})
	return s.Overview(ctx)
}

func validateNorthboundSettings(item *models.NorthboundAdminSettings) error {
	if item == nil {
		return errors.New("northbound settings are required")
	}
	checks := []struct {
		name            string
		value, min, max int
	}{
		{"challenge TTL", item.ChallengeTTLSeconds, 15, 600}, {"access token TTL", item.AccessTokenTTLSeconds, 60, 86400},
		{"refresh token TTL", item.RefreshTokenTTLSeconds, 3600, 2592000}, {"Core request timeout", item.CoreRequestTimeoutSeconds, 5, 120},
		{"challenge rate", item.ChallengeRatePerMinute, 1, 10000}, {"login rate", item.LoginRatePerMinute, 1, 10000},
		{"account login rate", item.AccountLoginRatePerMinute, 1, 10000}, {"create rate", item.CreateRatePerMinute, 1, 10000},
		{"query rate", item.QueryRatePerMinute, 1, 100000}, {"share rate", item.ShareRatePerMinute, 1, 10000},
		{"pending operation limit", item.MaxPendingOperations, 1, 1000}, {"operation tick", item.OperationTickMilliseconds, 100, 60000},
		{"operation lease", item.OperationLeaseSeconds, 5, 600}, {"operation attempts", item.OperationMaxAttempts, 1, 20},
		{"Lite memory", item.LiteMemoryGB, 1, 128}, {"Lite disk", item.LiteDiskGB, 1, 2048},
		{"Pro memory", item.ProMemoryGB, 1, 512}, {"Pro disk", item.ProDiskGB, 1, 4096},
		{"WorkBuddy Pro memory", item.WorkBuddyProMemoryGB, 1, 512}, {"WorkBuddy Pro disk", item.WorkBuddyProDiskGB, 1, 4096},
	}
	for _, check := range checks {
		if check.value < check.min || check.value > check.max {
			return fmt.Errorf("%s must be between %d and %d", check.name, check.min, check.max)
		}
	}
	if item.LiteCPUCores < 0.1 || item.LiteCPUCores > 64 || item.ProCPUCores < 0.1 || item.ProCPUCores > 128 || item.WorkBuddyProCPUCores < 0.1 || item.WorkBuddyProCPUCores > 128 {
		return errors.New("runtime CPU value is outside the supported range")
	}
	item.AllowedLiteTypes = normalizeValues(item.AllowedLiteTypes)
	item.AllowedProTypes = normalizeValues(item.AllowedProTypes)
	if len(item.AllowedLiteTypes) == 0 || len(item.AllowedProTypes) == 0 {
		return errors.New("at least one Lite and one Pro runtime must remain enabled")
	}
	for _, value := range item.AllowedLiteTypes {
		if _, ok := allowedLiteRuntimeTypes[value]; !ok {
			return fmt.Errorf("unsupported Lite runtime %q", value)
		}
	}
	for _, value := range item.AllowedProTypes {
		if _, ok := allowedProRuntimeTypes[value]; !ok {
			return fmt.Errorf("unsupported Pro runtime %q", value)
		}
	}
	return nil
}

func normalizeValues(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

type KubernetesNorthboundController struct{ client *k8ssvc.Client }

func NewKubernetesNorthboundController(client *k8ssvc.Client) *KubernetesNorthboundController {
	return &KubernetesNorthboundController{client: client}
}

func (c *KubernetesNorthboundController) namespace() (string, error) {
	if c == nil || c.client == nil || c.client.Clientset == nil {
		return "", errors.New("Kubernetes client is not initialized")
	}
	return c.client.GetSystemNamespace(), nil
}

func (c *KubernetesNorthboundController) Status(ctx context.Context) NorthboundClusterStatus {
	status := NorthboundClusterStatus{GatewayPort: 9443, CorePort: 9002, ServiceName: "clawmanager-northbound-gateway"}
	ns, err := c.namespace()
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Namespace = ns
	service, err := c.client.Clientset.CoreV1().Services(ns).Get(ctx, status.ServiceName, metav1.GetOptions{})
	if err != nil {
		status.Error = err.Error()
		return status
	}
	status.Available = true
	for _, port := range service.Spec.Ports {
		if port.Name == "https" || port.Port == 9443 {
			status.ExternalNodePort = int(port.NodePort)
			break
		}
	}
	status.Certificate = c.certificateStatus(ctx, ns)
	return status
}

func (c *KubernetesNorthboundController) SetExternalNodePort(ctx context.Context, nodePort int) (int, error) {
	ns, err := c.namespace()
	if err != nil {
		return 0, err
	}
	if services, listErr := c.client.Clientset.CoreV1().Services("").List(ctx, metav1.ListOptions{}); listErr == nil {
		for _, svc := range services.Items {
			for _, port := range svc.Spec.Ports {
				if int(port.NodePort) == nodePort && !(svc.Namespace == ns && svc.Name == "clawmanager-northbound-gateway") {
					return 0, fmt.Errorf("NodePort %d is already used by %s/%s", nodePort, svc.Namespace, svc.Name)
				}
			}
		}
	}
	api := c.client.Clientset.CoreV1().Services(ns)
	service, err := api.Get(ctx, "clawmanager-northbound-gateway", metav1.GetOptions{})
	if err != nil {
		return 0, err
	}
	index, old := -1, 0
	for i := range service.Spec.Ports {
		if service.Spec.Ports[i].Name == "https" || service.Spec.Ports[i].Port == 9443 {
			index = i
			old = int(service.Spec.Ports[i].NodePort)
			break
		}
	}
	if index < 0 {
		return 0, errors.New("northbound Gateway HTTPS service port was not found")
	}
	if old == nodePort {
		return old, nil
	}
	service.Spec.Ports[index].NodePort = int32(nodePort)
	if _, err := api.Update(ctx, service, metav1.UpdateOptions{}); err != nil {
		return old, fmt.Errorf("update northbound NodePort: %w", err)
	}
	verified, err := api.Get(ctx, service.Name, metav1.GetOptions{})
	if err != nil {
		return old, err
	}
	if int(verified.Spec.Ports[index].NodePort) != nodePort {
		return old, errors.New("Kubernetes did not retain the requested NodePort")
	}
	return old, nil
}

func (c *KubernetesNorthboundController) PublicCA(ctx context.Context) ([]byte, error) {
	ns, err := c.namespace()
	if err != nil {
		return nil, err
	}
	secret, err := c.client.Clientset.CoreV1().Secrets(ns).Get(ctx, "clawmanager-northbound-gateway-ca", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	value := secret.Data["ca.crt"]
	if len(value) == 0 {
		return nil, errors.New("gateway public CA is empty")
	}
	return append([]byte(nil), value...), nil
}

func (c *KubernetesNorthboundController) PreparedCA(ctx context.Context) ([]byte, error) {
	ns, err := c.namespace()
	if err != nil {
		return nil, err
	}
	secret, err := c.client.Clientset.CoreV1().Secrets(ns).Get(ctx, "clawmanager-northbound-gateway-ca-next", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	value := secret.Data["ca.crt"]
	if len(value) == 0 {
		return nil, errors.New("prepared gateway public CA is empty")
	}
	return append([]byte(nil), value...), nil
}

func (c *KubernetesNorthboundController) PrepareManagedCertificate(ctx context.Context, request CertificatePrepareRequest) error {
	ns, err := c.namespace()
	if err != nil {
		return err
	}
	request.DNSNames = normalizeValues(request.DNSNames)
	if request.ValidDays == 0 {
		request.ValidDays = 825
	}
	if request.ValidDays < 30 || request.ValidDays > 3650 {
		return errors.New("certificate validity must be between 30 and 3650 days")
	}
	var ips []net.IP
	for _, raw := range request.IPAddresses {
		ip := net.ParseIP(strings.TrimSpace(raw))
		if ip == nil {
			return fmt.Errorf("invalid certificate IP address %q", raw)
		}
		ips = append(ips, ip)
	}
	if len(request.DNSNames) == 0 && len(ips) == 0 {
		return errors.New("at least one DNS name or IP address is required")
	}
	for _, name := range request.DNSNames {
		if strings.ContainsAny(name, " /\\") || len(name) > 253 {
			return fmt.Errorf("invalid certificate DNS name %q", name)
		}
	}
	var caCertPEM, caKeyPEM, leafCertPEM, leafKeyPEM []byte
	managed, managedErr := c.client.Clientset.CoreV1().Secrets(ns).Get(ctx, "clawmanager-northbound-managed-ca", metav1.GetOptions{})
	if managedErr == nil {
		caCertPEM = append([]byte(nil), managed.Data[corev1.TLSCertKey]...)
		caKeyPEM = append([]byte(nil), managed.Data[corev1.TLSPrivateKeyKey]...)
		leafCertPEM, leafKeyPEM, err = issueManagedLeaf(caCertPEM, caKeyPEM, request.DNSNames, ips, request.ValidDays)
	} else if apierrors.IsNotFound(managedErr) {
		caCertPEM, caKeyPEM, leafCertPEM, leafKeyPEM, _, err = generateManagedCertificate(request.DNSNames, ips, request.ValidDays)
	} else {
		return managedErr
	}
	if err != nil {
		return err
	}
	labels := map[string]string{"app.kubernetes.io/managed-by": "clawmanager", "clawmanager.io/northbound-certificate": "staged"}
	if err := upsertSystemSecret(ctx, c.client, ns, "clawmanager-northbound-managed-ca", corev1.SecretTypeTLS, map[string][]byte{corev1.TLSCertKey: caCertPEM, corev1.TLSPrivateKeyKey: caKeyPEM}, labels); err != nil {
		return err
	}
	if err := upsertSystemSecret(ctx, c.client, ns, "clawmanager-northbound-gateway-tls-next", corev1.SecretTypeTLS, map[string][]byte{corev1.TLSCertKey: leafCertPEM, corev1.TLSPrivateKeyKey: leafKeyPEM}, labels); err != nil {
		return err
	}
	if err := upsertSystemSecret(ctx, c.client, ns, "clawmanager-northbound-gateway-ca-next", corev1.SecretTypeOpaque, map[string][]byte{"ca.crt": caCertPEM}, labels); err != nil {
		return err
	}
	return nil
}

func (c *KubernetesNorthboundController) ActivateManagedCertificate(ctx context.Context) error {
	ns, err := c.namespace()
	if err != nil {
		return err
	}
	secrets := c.client.Clientset.CoreV1().Secrets(ns)
	nextTLS, err := secrets.Get(ctx, "clawmanager-northbound-gateway-tls-next", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("prepared gateway certificate not found: %w", err)
	}
	nextCA, err := secrets.Get(ctx, "clawmanager-northbound-gateway-ca-next", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("prepared gateway CA not found: %w", err)
	}
	if !matchingTLSKeyPair(nextTLS.Data[corev1.TLSCertKey], nextTLS.Data[corev1.TLSPrivateKeyKey]) {
		return errors.New("prepared gateway certificate and private key do not match")
	}
	activeTLS, err := secrets.Get(ctx, "clawmanager-northbound-gateway-tls", metav1.GetOptions{})
	if err != nil {
		return err
	}
	activeCA, err := secrets.Get(ctx, "clawmanager-northbound-gateway-ca", metav1.GetOptions{})
	if err != nil {
		return err
	}
	oldTLS := activeTLS.DeepCopy()
	oldCA := activeCA.DeepCopy()
	activeTLS.Data = cloneSecretData(nextTLS.Data)
	activeCA.Data = cloneSecretData(nextCA.Data)
	if _, err = secrets.Update(ctx, activeTLS, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("activate gateway certificate: %w", err)
	}
	rollback := func() {
		current, _ := secrets.Get(context.Background(), oldTLS.Name, metav1.GetOptions{})
		if current != nil {
			oldTLS.ResourceVersion = current.ResourceVersion
			_, _ = secrets.Update(context.Background(), oldTLS, metav1.UpdateOptions{})
		}
		currentCA, _ := secrets.Get(context.Background(), oldCA.Name, metav1.GetOptions{})
		if currentCA != nil {
			oldCA.ResourceVersion = currentCA.ResourceVersion
			_, _ = secrets.Update(context.Background(), oldCA, metav1.UpdateOptions{})
		}
	}
	if _, err = secrets.Update(ctx, activeCA, metav1.UpdateOptions{}); err != nil {
		rollback()
		return fmt.Errorf("activate gateway CA: %w", err)
	}
	deployments := c.client.Clientset.AppsV1().Deployments(ns)
	deployment, err := deployments.Get(ctx, "clawmanager-northbound-gateway", metav1.GetOptions{})
	if err != nil {
		rollback()
		return fmt.Errorf("restart northbound Gateway: %w", err)
	}
	if deployment.Spec.Template.Annotations == nil {
		deployment.Spec.Template.Annotations = map[string]string{}
	}
	deployment.Spec.Template.Annotations["clawmanager.io/northbound-certificate-activated-at"] = time.Now().UTC().Format(time.RFC3339Nano)
	if _, err = deployments.Update(ctx, deployment, metav1.UpdateOptions{}); err != nil {
		rollback()
		return fmt.Errorf("restart northbound Gateway: %w", err)
	}
	_ = secrets.Delete(ctx, "clawmanager-northbound-gateway-tls-next", metav1.DeleteOptions{})
	_ = secrets.Delete(ctx, "clawmanager-northbound-gateway-ca-next", metav1.DeleteOptions{})
	return nil
}

func upsertSystemSecret(ctx context.Context, client *k8ssvc.Client, namespace, name string, secretType corev1.SecretType, data map[string][]byte, labels map[string]string) error {
	api := client.Clientset.CoreV1().Secrets(namespace)
	existing, err := api.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		existing.Type = secretType
		existing.Data = cloneSecretData(data)
		if existing.Labels == nil {
			existing.Labels = map[string]string{}
		}
		for key, value := range labels {
			existing.Labels[key] = value
		}
		_, err = api.Update(ctx, existing, metav1.UpdateOptions{})
		return err
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	_, err = api.Create(ctx, &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace, Labels: labels}, Type: secretType, Data: cloneSecretData(data)}, metav1.CreateOptions{})
	return err
}
func cloneSecretData(input map[string][]byte) map[string][]byte {
	result := map[string][]byte{}
	for key, value := range input {
		result[key] = append([]byte(nil), value...)
	}
	return result
}
func matchingTLSKeyPair(cert, key []byte) bool { _, err := tlsKeyPair(cert, key); return err == nil }
func tlsKeyPair(cert, key []byte) (any, error) {
	certBlock, _ := pem.Decode(cert)
	keyBlock, _ := pem.Decode(key)
	if certBlock == nil || keyBlock == nil {
		return nil, errors.New("invalid PEM")
	}
	parsedCert, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil {
		return nil, err
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, err
	}
	rsaKey, ok := parsedKey.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("private key is not RSA")
	}
	certKey, ok := parsedCert.PublicKey.(*rsa.PublicKey)
	if !ok || !certKey.Equal(&rsaKey.PublicKey) {
		return nil, errors.New("key pair mismatch")
	}
	return parsedKey, nil
}

func generateManagedCertificate(dnsNames []string, ips []net.IP, validDays int) ([]byte, []byte, []byte, []byte, *x509.Certificate, error) {
	now := time.Now().UTC()
	caKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	caSerial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	ca := &x509.Certificate{SerialNumber: caSerial, Subject: pkix.Name{CommonName: "ClawManager Northbound Managed Internal CA"}, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature}
	caDER, err := x509.CreateCertificate(rand.Reader, ca, ca, &caKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	leafKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	commonName := "clawmanager-northbound-gateway"
	if len(dnsNames) > 0 {
		commonName = dnsNames[0]
	} else if len(ips) > 0 {
		commonName = ips[0].String()
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: commonName}, DNSNames: dnsNames, IPAddresses: ips, NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(0, 0, validDays), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	caKeyDER, err := x509.MarshalPKCS8PrivateKey(caKey)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	leafKeyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return nil, nil, nil, nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: caKeyDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: leafDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: leafKeyDER}), leaf, nil
}

func issueManagedLeaf(caCertPEM, caKeyPEM []byte, dnsNames []string, ips []net.IP, validDays int) ([]byte, []byte, error) {
	certBlock, _ := pem.Decode(caCertPEM)
	keyBlock, _ := pem.Decode(caKeyPEM)
	if certBlock == nil || keyBlock == nil {
		return nil, nil, errors.New("managed CA data is invalid")
	}
	ca, err := x509.ParseCertificate(certBlock.Bytes)
	if err != nil || !ca.IsCA {
		return nil, nil, errors.New("managed CA certificate is invalid")
	}
	parsedKey, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, nil, err
	}
	caKey, ok := parsedKey.(*rsa.PrivateKey)
	if !ok {
		return nil, nil, errors.New("managed CA private key is not RSA")
	}
	now := time.Now().UTC()
	serial, _ := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	commonName := "clawmanager-northbound-gateway"
	if len(dnsNames) > 0 {
		commonName = dnsNames[0]
	} else if len(ips) > 0 {
		commonName = ips[0].String()
	}
	notAfter := now.AddDate(0, 0, validDays)
	if notAfter.After(ca.NotAfter) {
		notAfter = ca.NotAfter.Add(-time.Hour)
	}
	leaf := &x509.Certificate{SerialNumber: serial, Subject: pkix.Name{CommonName: commonName}, DNSNames: dnsNames, IPAddresses: ips, NotBefore: now.Add(-5 * time.Minute), NotAfter: notAfter, KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafKey, err := rsa.GenerateKey(rand.Reader, 3072)
	if err != nil {
		return nil, nil, err
	}
	der, err := x509.CreateCertificate(rand.Reader, leaf, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(leafKey)
	if err != nil {
		return nil, nil, err
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func (c *KubernetesNorthboundController) certificateStatus(ctx context.Context, ns string) NorthboundCertificateStatus {
	status := NorthboundCertificateStatus{ManagementNote: "Current CA signing key is not managed; inspect/download is safe, renewal requires one-time managed CA initialization."}
	secret, err := c.client.Clientset.CoreV1().Secrets(ns).Get(ctx, "clawmanager-northbound-gateway-tls", metav1.GetOptions{})
	if err != nil {
		status.Error = err.Error()
		return status
	}
	block, _ := pem.Decode(secret.Data[corev1.TLSCertKey])
	if block == nil {
		status.Error = "gateway certificate is not valid PEM"
		return status
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		status.Error = err.Error()
		return status
	}
	sum := sha256.Sum256(cert.Raw)
	status.Available = true
	status.Subject = cert.Subject.String()
	status.Issuer = cert.Issuer.String()
	status.DNSNames = cert.DNSNames
	status.NotBefore = cert.NotBefore
	status.NotAfter = cert.NotAfter
	status.DaysRemaining = int(time.Until(cert.NotAfter).Hours() / 24)
	status.SHA256 = strings.ToUpper(hex.EncodeToString(sum[:]))
	for _, ip := range cert.IPAddresses {
		status.IPAddresses = append(status.IPAddresses, ip.String())
	}
	if signer, err := c.client.Clientset.CoreV1().Secrets(ns).Get(ctx, "clawmanager-northbound-managed-ca", metav1.GetOptions{}); err == nil && len(signer.Data[corev1.TLSPrivateKeyKey]) > 0 {
		status.Managed = true
		status.ManagementNote = "Managed internal CA signing material is present; renewal can be enabled without exposing the private key."
	}
	if staged, err := c.client.Clientset.CoreV1().Secrets(ns).Get(ctx, "clawmanager-northbound-gateway-tls-next", metav1.GetOptions{}); err == nil {
		if block, _ := pem.Decode(staged.Data[corev1.TLSCertKey]); block != nil {
			if parsed, parseErr := x509.ParseCertificate(block.Bytes); parseErr == nil {
				status.Prepared = true
				status.PreparedNotAfter = parsed.NotAfter
			}
		}
	}
	return status
}
