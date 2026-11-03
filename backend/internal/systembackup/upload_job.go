package systembackup

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type UploadJobRequest struct {
	VerifyJobRequest
	BootstrapUID       string      `json:"bootstrap_uid"`
	OperationSecret    string      `json:"operation_secret"`
	OperationSecretUID string      `json:"operation_secret_uid"`
	OrdinaryStage      PVCIdentity `json:"ordinary_stage"`
	SecretStage        PVCIdentity `json:"secret_stage"`
	NodeName           string      `json:"node_name,omitempty"`
}
type UploadJobResult struct {
	State  string            `json:"state"`
	JobUID string            `json:"job_uid"`
	Report *PartUploadReport `json:"report,omitempty"`
}
type UploadJobs struct{ Client kubernetes.Interface }

func (j UploadJobs) Existing(ctx context.Context, a Attempt) (*batchv1.Job, error) {
	want, r, err := uploadJob(a)
	if err != nil {
		return nil, err
	}
	if err = (VerifyJobs{Client: j.Client}).namespace(ctx, r.VerifyJobRequest); err != nil {
		return nil, err
	}
	job, err := j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, want.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if !matchesCaptureJob(want, job) {
		return nil, errors.New("artifact_conflict")
	}
	return job, nil
}

func uploadJob(a Attempt) (*batchv1.Job, UploadJobRequest, error) {
	var r UploadJobRequest
	if strictDecode(a.Request, &r) != nil || a.Phase != "upload" || r.BackupID != a.TaskID || r.DeadlineAt.IsZero() || r.OrdinaryStage.UID == r.SecretStage.UID {
		return nil, r, errors.New("validation_failed")
	}
	// Reuse the capture input validator, including immutable Secret/PVC IDs,
	// without giving this Job any live source volume or source credential.
	input := CaptureJobRequest{VerifyJobRequest: r.VerifyJobRequest, Phase: "secret_capture", Gate: GateOwnership{Scope: "upload", Owner: a.TaskID, Generation: 1, Token: 1}, BootstrapUID: r.BootstrapUID, OperationSecret: r.OperationSecret, OperationSecretUID: r.OperationSecretUID, Stage: r.OrdinaryStage, NodeName: r.NodeName, MaxItems: 1}
	inputRaw, _ := json.Marshal(input)
	_, _, err := captureJob(Attempt{ID: a.ID, TaskID: a.TaskID, Phase: "secret_capture", Request: inputRaw})
	if err != nil {
		return nil, r, err
	}
	input.Stage = r.SecretStage
	inputRaw, _ = json.Marshal(input)
	if _, _, err = captureJob(Attempt{ID: a.ID, TaskID: a.TaskID, Phase: "secret_capture", Request: inputRaw}); err != nil {
		return nil, r, err
	}
	base := a
	base.Request, _ = json.Marshal(r.VerifyJobRequest)
	job, _, err := verifyJob(base)
	if err != nil {
		return nil, r, err
	}
	job.Name = "backup-upload-" + a.ID[4:]
	h := sha256.Sum256(a.Request)
	job.Annotations["backup.clawmanager.io/request-sha256"] = hex.EncodeToString(h[:])
	job.Labels["backup.clawmanager.io/role"] = "upload"
	job.Spec.Template.Labels["backup.clawmanager.io/role"] = "upload"
	pod := &job.Spec.Template.Spec
	pod.NodeName = r.NodeName
	no := false
	pod.EnableServiceLinks = &no
	grace := int64(5)
	pod.TerminationGracePeriodSeconds = &grace
	zero := int64(0)
	pod.SecurityContext.RunAsNonRoot = &no
	pod.SecurityContext.RunAsUser = &zero
	pod.SecurityContext.RunAsGroup = &zero
	pod.SecurityContext.FSGroup = nil
	c := &pod.Containers[0]
	c.Name = "uploader"
	c.SecurityContext.Capabilities.Add = []corev1.Capability{"DAC_OVERRIDE"}
	c.Args = []string{"--operation", "upload-parts", "--bootstrap", "/run/backup-bootstrap/config.json", "--operation-config", "/run/backup-upload-config/config.json", "--origin", r.Origin, "--backup", r.BackupID, "--max-bytes", strconv.FormatInt(r.MaxBytes, 10), "--timeout", strconv.FormatInt(r.DeadlineSeconds, 10) + "s", "--deadline-at", r.DeadlineAt.UTC().Format(time.RFC3339Nano), "--report-file", "/dev/termination-log"}
	requestRaw, _ := canonicalJSON(PartUploadRequest{Origin: r.Origin, BackupID: r.BackupID, AttemptID: a.ID, OrdinaryStageUID: r.OrdinaryStage.UID, SecretStageUID: r.SecretStage.UID})
	c.Args = append(c.Args, "--upload-request", base64.RawURLEncoding.EncodeToString(requestRaw))
	mode := int32(0440)
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "operation", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: r.OperationSecret, DefaultMode: &mode}}})
	c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{Name: "operation", MountPath: "/run/backup-upload-config", ReadOnly: true})
	for _, v := range []struct {
		name, path string
		ref        PVCIdentity
	}{{"ordinary", "/run/backup-ordinary", r.OrdinaryStage}, {"secrets", "/run/backup-secrets", r.SecretStage}} {
		pod.Volumes = append(pod.Volumes, corev1.Volume{Name: v.name, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: v.ref.Name, ReadOnly: true}}})
		c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{Name: v.name, MountPath: v.path, ReadOnly: true})
	}
	return job, r, nil
}

func (j UploadJobs) Ensure(ctx context.Context, a Attempt, uid string, create bool) (*batchv1.Job, error) {
	want, r, err := uploadJob(a)
	if err != nil {
		return nil, err
	}
	if uid == "" && !create {
		return nil, errors.New("coordination_invalid")
	}
	inputs := CaptureJobRequest{VerifyJobRequest: r.VerifyJobRequest, BootstrapUID: r.BootstrapUID, OperationSecret: r.OperationSecret, OperationSecretUID: r.OperationSecretUID, Stage: r.OrdinaryStage, SourceVolumes: []CaptureSourceVolume{{PVCIdentity: r.SecretStage, ID: "sealed-secrets"}}}
	if err = (CaptureJobs{Client: j.Client}).inputs(ctx, inputs); err != nil {
		return nil, err
	}
	job, err := j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) && uid == "" && create {
		job, err = j.Client.BatchV1().Jobs(r.Namespace).Create(ctx, want, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			job, err = j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, want.Name, metav1.GetOptions{})
		}
	}
	if err != nil {
		return nil, err
	}
	if !matchesCaptureJob(want, job) || uid != "" && string(job.UID) != uid {
		return nil, errors.New("artifact_conflict")
	}
	return job, nil
}
func (j UploadJobs) Observe(ctx context.Context, a Attempt, uid string) (UploadJobResult, error) {
	out := UploadJobResult{State: "running", JobUID: uid}
	job, err := j.Ensure(ctx, a, uid, false)
	if err != nil {
		return out, err
	}
	_, r, _ := uploadJob(a)
	complete := false
	for _, c := range job.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			if c.Type == batchv1.JobFailed {
				out.State = "failed"
				return out, nil
			}
			complete = complete || c.Type == batchv1.JobComplete
		}
	}
	if !complete {
		return out, nil
	}
	pods, err := j.Client.CoreV1().Pods(r.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "backup.clawmanager.io/attempt=" + a.ID})
	if err != nil {
		return out, err
	}
	count := 0
	for _, p := range pods.Items {
		if !ownedByJob(p, job.UID) {
			continue
		}
		count++
		actual := job.DeepCopy()
		actual.Spec.Template.Spec = *p.Spec.DeepCopy()
		actual.Spec.Template.Labels = p.Labels
		if !matchesCaptureJob(job, actual) || count > 1 || len(p.Status.ContainerStatuses) != 1 {
			return out, errors.New("isolation_invalid")
		}
		c := p.Status.ContainerStatuses[0]
		term := c.State.Terminated
		if c.Name != "uploader" || term == nil || term.ExitCode != 0 || len(term.Message) > 4096 {
			return out, errors.New("evidence_incomplete")
		}
		var report PartUploadReport
		expected := PartUploadRequest{Origin: r.Origin, BackupID: r.BackupID, AttemptID: a.ID, OrdinaryStageUID: r.OrdinaryStage.UID, SecretStageUID: r.SecretStage.UID}
		if strictDecode([]byte(term.Message), &report) != nil || report.Schema != "system-backup-upload.v1" || report.PartUploadRequest != expected || report.Parts != len(RequiredParts) || report.Index.Key != expected.indexKey() || report.Index.VersionID == "" || report.Index.VersionID == "null" || report.Index.Bytes <= 0 || report.Index.Bytes > MaxImmutableObjectBytes || !validHash(report.Index.SHA256) {
			return out, errors.New("evidence_incomplete")
		}
		out.State = "succeeded"
		out.Report = &report
	}
	if count != 1 {
		return out, errors.New("evidence_incomplete")
	}
	return out, nil
}
func (j UploadJobs) Stop(ctx context.Context, a Attempt, uid string) (bool, error) {
	want, r, err := uploadJob(a)
	if err != nil {
		return false, err
	}
	if uid == "" {
		return false, errors.New("cleanup_unsafe_target")
	}
	if err = (VerifyJobs{Client: j.Client}).namespace(ctx, r.VerifyJobRequest); err != nil {
		return false, err
	}
	job, err := j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		pods, e := j.Client.CoreV1().Pods(r.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "backup.clawmanager.io/attempt=" + a.ID})
		if e != nil {
			return false, e
		}
		for _, p := range pods.Items {
			if ownedByJob(p, types.UID(uid)) {
				return false, nil
			}
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if string(job.UID) != uid || !matchesCaptureJob(want, job) {
		return false, errors.New("cleanup_unsafe_target")
	}
	identity := types.UID(uid)
	policy := metav1.DeletePropagationForeground
	err = j.Client.BatchV1().Jobs(r.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &identity}, PropagationPolicy: &policy})
	return false, err
}
