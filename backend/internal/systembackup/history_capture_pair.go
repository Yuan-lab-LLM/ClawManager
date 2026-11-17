package systembackup

import (
	"context"
	"encoding/json"
	"errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var historyRequiredSecretNames = []string{"clawmanager-observability-key-v1", "clawmanager-history-restore-v1"}

func hasObservabilitySchema(ctx context.Context, q SQLQueryer) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='observability_admissions'`).Scan(&n)
	return n == 1, err
}
func validateHistoryPlanCoverage(ordinary, secrets CaptureJobRequest) error {
	found := false
	for _, volume := range ordinary.SourceVolumes {
		if volume.ID == "observability" && volume.Name != "" && volume.UID != "" {
			found = true
		}
	}
	if !found {
		return errors.New("backup plan requires the frozen observability journal PVC")
	}
	if len(ordinary.RequiredSecrets) != 0 {
		return errors.New("secret requirements belong to the separate secret collector")
	}
	for _, name := range historyRequiredSecretNames {
		found = false
		for _, selection := range secrets.RequiredSecrets {
			if selection.Group == "" && selection.Version == "v1" && selection.Resource == "secrets" && selection.Name == name && refPattern.MatchString(selection.UID) {
				found = true
			}
		}
		if !found {
			return errors.New("backup plan requires frozen observability key and history restore credential identities")
		}
	}
	return nil
}
func validateRequiredCaptureSecrets(required, actual []KubernetesSelection) error {
	for _, want := range required {
		found := false
		for _, have := range actual {
			if want == have {
				found = true
			}
		}
		if !found {
			return errors.New("secret collector omitted or changed a required history encryption/restore credential")
		}
	}
	return nil
}
func freezeHistorySecretPair(ctx context.Context, client kubernetes.Interface, ordinary CaptureJobRequest, secrets *CaptureJobRequest) error {
	if client == nil || ordinary.Namespace != secrets.Namespace || ordinary.NamespaceUID != secrets.NamespaceUID {
		return errors.New("history backup secret isolation unavailable")
	}
	operation, err := client.CoreV1().Secrets(secrets.Namespace).Get(ctx, secrets.OperationSecret, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(operation.UID) != secrets.OperationSecretUID || operation.Immutable == nil || !*operation.Immutable || operation.DeletionTimestamp != nil {
		return errors.New("history secret operation configuration changed")
	}
	var config struct {
		Source struct {
			Namespace    string                `json:"namespace"`
			NamespaceUID string                `json:"namespace_uid"`
			Secrets      []KubernetesSelection `json:"secrets"`
		} `json:"source"`
	}
	raw := operation.Data["config.json"]
	if len(raw) == 0 || len(raw) > 1<<20 || json.Unmarshal(raw, &config) != nil || config.Source.Namespace != secrets.Namespace || config.Source.NamespaceUID != secrets.NamespaceUID {
		return errors.New("invalid frozen history secret source configuration")
	}
	required := []KubernetesSelection{}
	for _, name := range historyRequiredSecretNames {
		found := false
		for _, selection := range config.Source.Secrets {
			if selection.Group != "" || selection.Version != "v1" || selection.Resource != "secrets" || selection.Name != name {
				continue
			}
			object, err := client.CoreV1().Secrets(secrets.Namespace).Get(ctx, name, metav1.GetOptions{})
			if err != nil {
				return err
			}
			if string(object.UID) != selection.UID || object.Immutable == nil || !*object.Immutable || object.DeletionTimestamp != nil {
				return errors.New("required history encryption/restore credential changed or mutable")
			}
			required = append(required, selection)
			found = true
			break
		}
		if !found {
			return errors.New("history key or restore credential omitted from encrypted secret bundle")
		}
	}
	secrets.RequiredSecrets = required
	return validateHistoryPlanCoverage(ordinary, *secrets)
}
