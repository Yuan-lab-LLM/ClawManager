package k8s

import (
	"bytes"
	"clawreef/internal/config"
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// EnsureRuntimeSlotPools bootstraps explicitly enabled slot pools without
// restarting or scaling down serving pools on a backend restart. Changes to
// active images, slot counts or credentials require the drain/rollout workflow.
func EnsureRuntimeSlotPools(ctx context.Context, client kubernetes.Interface, cfg config.RuntimePoolConfig) error {
	if cfg.ContainerSlots == 0 {
		return nil
	}
	if client == nil {
		return fmt.Errorf("container slots require Kubernetes connectivity")
	}
	if cfg.AgentControlToken == "" || cfg.AgentReportToken == "" {
		return fmt.Errorf("container slots require control and report credentials")
	}
	const secretName = "clawmanager-lite-agent"
	specs, err := RuntimeSlotDeploymentSpecs(cfg, secretName)
	if err != nil {
		return err
	}
	service := &runtimeDeploymentService{client: client}
	if err := service.validateRuntimePodCapabilities(specs[0]); err != nil {
		return err
	}
	missing := map[string]bool{}
	for _, spec := range specs {
		if err = service.validateRuntimeSlotStorage(ctx, spec); err != nil {
			return err
		}
		existing, e := client.AppsV1().Deployments(spec.Namespace).Get(ctx, spec.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(e) {
			missing[spec.Name] = true
			continue
		}
		if e != nil {
			return e
		}
		if existing.Spec.Template.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" {
			return fmt.Errorf("existing %s requires explicit process-pool migration before enabling container slots", spec.Name)
		}
		profile := ""
		if spec.NetworkIsolation {
			profile = RuntimeNetworkSecurityProfile
		}
		if existing.Spec.Template.Labels["clawmanager.io/runtime-security"] != profile {
			return fmt.Errorf("serving %s network security change requires a coordinated rollout", spec.Name)
		}
		if err := validateRuntimeSlotTLSDeployment(existing); err != nil {
			return err
		}
		if len(existing.Spec.Template.Spec.Containers) != spec.SlotCount+1 || runtimeContainerImage(existing.Spec.Template.Spec.Containers) != spec.Image {
			return fmt.Errorf("serving %s slot/image change requires a coordinated rollout", spec.Name)
		}
		pod := existing.Spec.Template.Spec
		usesUserNamespaces := pod.HostUsers != nil && !*pod.HostUsers
		if !apiequality.Semantic.DeepEqual(pod.Resources, spec.PodResources) || usesUserNamespaces != spec.PodUserNamespaces {
			return fmt.Errorf("serving %s Pod resource/user namespace change requires a coordinated rollout", spec.Name)
		}
		if spec.PodResources != nil {
			desired := BuildRuntimeDeployment(spec).Spec.Template.Spec
			for i, container := range pod.Containers {
				if container.Name != desired.Containers[i].Name || !apiequality.Semantic.DeepEqual(container.Resources, desired.Containers[i].Resources) {
					return fmt.Errorf("serving %s container resource change requires a coordinated rollout", spec.Name)
				}
			}
		}
	}
	if err := EnsureRuntimeSlotTLS(ctx, client, specs[0].Namespace); err != nil {
		return fmt.Errorf("prepare runtime slot TLS: %w", err)
	}
	namespace := specs[0].Namespace
	accounts := client.CoreV1().ServiceAccounts(namespace)
	account, err := accounts.Get(ctx, runtimeSlotServiceAccountName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		no := false
		account, err = accounts.Create(ctx, &corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: runtimeSlotServiceAccountName, Namespace: namespace}, AutomountServiceAccountToken: &no}, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			account, err = accounts.Get(ctx, runtimeSlotServiceAccountName, metav1.GetOptions{})
		}
	}
	if err != nil {
		return fmt.Errorf("prepare runtime slot service account: %w", err)
	}
	if account.DeletionTimestamp != nil || account.AutomountServiceAccountToken == nil || *account.AutomountServiceAccountToken {
		return fmt.Errorf("runtime slot service account must disable API credential mounts")
	}
	desired := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace, Labels: map[string]string{"clawmanager.io/purpose": "lite-agent"}}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"control-token": []byte(cfg.AgentControlToken), "report-token": []byte(cfg.AgentReportToken)}}
	secrets := client.CoreV1().Secrets(namespace)
	existing, err := secrets.Get(ctx, secretName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		existing, err = secrets.Create(ctx, desired, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			existing, err = secrets.Get(ctx, secretName, metav1.GetOptions{})
		}
	} else if err == nil && existing.DeletionTimestamp != nil {
		return fmt.Errorf("runtime agent Secret is being deleted")
	}
	if err != nil {
		return err
	}
	if existing != nil && (!bytes.Equal(existing.Data["control-token"], desired.Data["control-token"]) || !bytes.Equal(existing.Data["report-token"], desired.Data["report-token"])) {
		return fmt.Errorf("runtime agent credential rotation requires a coordinated rollout")
	}
	for _, spec := range specs {
		if missing[spec.Name] {
			if err = service.Ensure(ctx, spec); err != nil {
				return err
			}
		}
	}
	return nil
}
