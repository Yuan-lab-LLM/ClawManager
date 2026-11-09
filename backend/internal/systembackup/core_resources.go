package systembackup

import (
	"bytes"
	"context"
	"errors"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type CoreResourceDriver interface {
	PrepareUpload(context.Context, Store, Leadership, Task, CapturePlan, CorePublication) (UploadJobRequest, error)
	Cleanup(context.Context, Store, Leadership, Task, CorePublication) (bool, error)
}
type KubernetesCoreResources struct {
	Client         kubernetes.Interface
	UploadTemplate UploadJobRequest
}

func coreConfigName(taskID string) string { return "backup-upload-config-" + taskID[4:] }
func coreConfigData(p CorePublication) ([]byte, error) {
	return canonicalJSON(struct {
		Manifest SignedManifest `json:"manifest"`
	}{p.Manifest})
}
func coreConfigMatches(secret *corev1.Secret, p CorePublication, namespace string) bool {
	raw, err := coreConfigData(p)
	return err == nil && secret.Name == coreConfigName(p.TaskID) && secret.Namespace == namespace && secret.Labels["backup.clawmanager.io/task"] == p.TaskID && secret.Labels["backup.clawmanager.io/role"] == "upload-config" && secret.Annotations["backup.clawmanager.io/manifest-sha256"] == p.ManifestHash && secret.Immutable != nil && *secret.Immutable && len(secret.Data) == 1 && len(secret.StringData) == 0 && bytes.Equal(secret.Data["config.json"], raw) && secret.Type == corev1.SecretTypeOpaque
}
func (r KubernetesCoreResources) PrepareUpload(ctx context.Context, s Store, l Leadership, t Task, capture CapturePlan, p CorePublication) (UploadJobRequest, error) {
	var zero UploadJobRequest
	if r.Client == nil || capture.SourceResumedAt == nil || capture.ForcedResumeAt != nil {
		return zero, errors.New("not_ready")
	}
	request := r.UploadTemplate
	request.Origin = s.InstallationID
	request.BackupID = t.ID
	request.MaxBytes = capture.Request.Ordinary.MaxBytes + capture.Request.Secrets.MaxBytes
	request.NodeName = capture.Request.Ordinary.NodeName
	request.OrdinaryStage = capture.Request.Ordinary.Stage
	request.SecretStage = capture.Request.Secrets.Stage
	request.Namespace = capture.Request.Ordinary.Namespace
	request.NamespaceUID = capture.Request.Ordinary.NamespaceUID
	if request.BootstrapSecret == capture.Request.Ordinary.BootstrapSecret || request.BootstrapSecret == capture.Request.Secrets.BootstrapSecret || request.BootstrapSecret == capture.Request.Ordinary.OperationSecret || request.BootstrapSecret == capture.Request.Secrets.OperationSecret {
		return zero, errors.New("isolation_invalid")
	}
	if err := (VerifyJobs{Client: r.Client}).namespace(ctx, request.VerifyJobRequest); err != nil {
		return zero, err
	}
	name := coreConfigName(t.ID)
	secret, err := r.Client.CoreV1().Secrets(request.Namespace).Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) && p.UploadConfigUID == "" {
		if err = s.captureConfigMutation(ctx, l, t, "", true); err != nil {
			return zero, err
		}
		raw, e := coreConfigData(p)
		if e != nil {
			return zero, e
		}
		yes := true
		want := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: request.Namespace, Labels: map[string]string{"backup.clawmanager.io/task": t.ID, "backup.clawmanager.io/role": "upload-config"}, Annotations: map[string]string{"backup.clawmanager.io/manifest-sha256": p.ManifestHash}}, Immutable: &yes, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{"config.json": raw}}
		secret, err = r.Client.CoreV1().Secrets(request.Namespace).Create(ctx, want, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			secret, err = r.Client.CoreV1().Secrets(request.Namespace).Get(ctx, name, metav1.GetOptions{})
		}
	}
	if err != nil {
		return zero, err
	}
	if secret.DeletionTimestamp != nil || !coreConfigMatches(secret, p, request.Namespace) || secret.UID == "" || p.UploadConfigUID != "" && p.UploadConfigUID != string(secret.UID) {
		return zero, errors.New("artifact_conflict")
	}
	if err = s.captureConfigMutation(ctx, l, t, string(secret.UID), false); err != nil {
		return zero, err
	}
	request.OperationSecret = name
	request.OperationSecretUID = string(secret.UID)
	var now time.Time
	if err = s.DB.QueryRowContext(ctx, "SELECT UTC_TIMESTAMP(6)").Scan(&now); err != nil {
		return zero, err
	}
	request.DeadlineAt = t.DeadlineAt
	request.DeadlineSeconds = int64(t.DeadlineAt.Sub(now) / time.Second)
	if request.DeadlineSeconds < 1 {
		return zero, errors.New("deadline_exceeded")
	}
	return request, nil
}
func (r KubernetesCoreResources) Cleanup(ctx context.Context, s Store, l Leadership, t Task, p CorePublication) (bool, error) {
	if r.Client == nil {
		return false, errors.New("not_ready")
	}
	plan, err := s.GetCapturePlan(ctx, t.ID)
	if err != nil {
		return false, err
	}
	namespace := plan.Request.Ordinary.Namespace
	if err = (VerifyJobs{Client: r.Client}).namespace(ctx, plan.Request.Ordinary.VerifyJobRequest); err != nil {
		return false, err
	}
	secret, err := r.Client.CoreV1().Secrets(namespace).Get(ctx, coreConfigName(t.ID), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		if p.UploadConfigUID == "" && p.UploadConfigSent {
			return false, nil
		}
		return true, s.MarkCoreConfigDeleted(ctx, l, t, p.UploadConfigUID)
	}
	if err != nil {
		return false, err
	}
	if !coreConfigMatches(secret, p, namespace) || secret.UID == "" || p.UploadConfigUID != "" && p.UploadConfigUID != string(secret.UID) {
		return false, errors.New("cleanup_unsafe_target")
	}
	if p.UploadConfigUID == "" {
		if err = s.captureConfigMutation(ctx, l, t, string(secret.UID), false); err != nil {
			return false, err
		}
		return false, nil
	}
	if secret.DeletionTimestamp != nil {
		return false, nil
	}
	uid := types.UID(p.UploadConfigUID)
	err = r.Client.CoreV1().Secrets(namespace).Delete(ctx, secret.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
	return false, err
}
