package systembackup

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"strings"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

type PVCIdentity struct {
	Name string `json:"name"`
	UID  string `json:"uid"`
}
type CaptureSourceVolume struct {
	PVCIdentity
	ID string `json:"id"`
}
type CaptureJobRequest struct {
	VerifyJobRequest
	Phase              string                `json:"phase"`
	Gate               GateOwnership         `json:"gate"`
	BootstrapUID       string                `json:"bootstrap_uid"`
	OperationSecret    string                `json:"operation_secret"`
	OperationSecretUID string                `json:"operation_secret_uid"`
	Stage              PVCIdentity           `json:"stage"`
	SourceVolumes      []CaptureSourceVolume `json:"source_volumes,omitempty"`
	NodeName           string                `json:"node_name,omitempty"`
	MaxItems           int                   `json:"max_items"`
}
type CaptureJobResult struct {
	State     string            `json:"state"`
	JobUID    string            `json:"job_uid"`
	Candidate *CaptureCandidate `json:"candidate,omitempty"`
}
type CaptureJobs struct{ Client kubernetes.Interface }

func (j CaptureJobs) Existing(ctx context.Context, a Attempt) (*batchv1.Job, error) {
	want, r, err := captureJob(a)
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

func captureJob(a Attempt) (*batchv1.Job, CaptureJobRequest, error) {
	var r CaptureJobRequest
	if !attemptIDPattern.MatchString(a.ID) || strictDecode(a.Request, &r) != nil || r.VerifyJobRequest.Validate() != nil || r.DeadlineAt.IsZero() || r.MaxItems < 1 || r.MaxItems > 100000000 || r.BackupID != a.TaskID || r.Phase != a.Phase || (r.Phase != "capture" && r.Phase != "secret_capture") || r.Gate.Owner != a.TaskID || r.Gate.Generation == 0 || r.Gate.Token == 0 || !refPattern.MatchString(r.BootstrapUID) || !refPattern.MatchString(r.OperationSecretUID) || r.OperationSecret == r.BootstrapSecret || r.NodeName != "" && len(validation.IsDNS1123Subdomain(r.NodeName)) != 0 {
		return nil, r, errors.New("validation_failed")
	}
	for _, name := range []string{r.OperationSecret, r.Stage.Name} {
		if name == "" || len(validation.IsDNS1123Subdomain(name)) != 0 {
			return nil, r, errors.New("validation_failed")
		}
	}
	if !refPattern.MatchString(r.Stage.UID) || r.Phase == "secret_capture" && len(r.SourceVolumes) != 0 || r.Phase == "capture" && len(r.SourceVolumes) == 0 || len(r.SourceVolumes) > 128 {
		return nil, r, errors.New("validation_failed")
	}
	seen := map[string]bool{}
	for _, v := range r.SourceVolumes {
		if v.Name == "" || len(validation.IsDNS1123Subdomain(v.Name)) != 0 || !refPattern.MatchString(v.UID) || v.UID == r.Stage.UID || !refPattern.MatchString(v.ID) || seen[v.ID] {
			return nil, r, errors.New("validation_failed")
		}
		seen[v.ID] = true
	}
	base := a
	base.Request, _ = json.Marshal(r.VerifyJobRequest)
	job, _, err := verifyJob(base)
	if err != nil {
		return nil, r, err
	}
	job.Name = "backup-capture-" + a.ID[4:]
	h := sha256.Sum256(a.Request)
	job.Annotations["backup.clawmanager.io/request-sha256"] = hex.EncodeToString(h[:])
	role := strings.ReplaceAll(r.Phase, "_", "-")
	job.Labels["backup.clawmanager.io/role"] = role
	job.Spec.Template.Labels["backup.clawmanager.io/role"] = role
	pod := &job.Spec.Template.Spec
	pod.NodeName = r.NodeName
	no := false
	pod.EnableServiceLinks = &no
	grace := int64(5)
	pod.TerminationGracePeriodSeconds = &grace
	container := &pod.Containers[0]
	container.Name = "collector"
	request := CapturePhaseRequest{Origin: r.Origin, BackupID: r.BackupID, AttemptID: a.ID, Phase: r.Phase, Gate: r.Gate, StageUID: r.Stage.UID}
	if err = request.Validate(); err != nil {
		return nil, r, err
	}
	raw, _ := canonicalJSON(request)
	container.Args = []string{"--operation", "capture-phase", "--bootstrap", "/run/backup-bootstrap/config.json", "--operation-config", "/run/backup-source-config/config.json", "--capture-request", base64.RawURLEncoding.EncodeToString(raw), "--ready-file", "/run/backup-status/ready", "--origin", r.Origin, "--backup", r.BackupID, "--max-bytes", strconv.FormatInt(r.MaxBytes, 10), "--max-items", strconv.Itoa(r.MaxItems), "--timeout", strconv.FormatInt(r.DeadlineSeconds, 10) + "s", "--report-file", "/dev/termination-log"}
	if !r.DeadlineAt.IsZero() {
		container.Args = append(container.Args, "--deadline-at", r.DeadlineAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"))
	}
	container.ReadinessProbe = &corev1.Probe{ProbeHandler: corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/usr/local/bin/system-backup-artifact", "--operation", "readiness", "--ready-file", "/run/backup-status/ready"}}}, PeriodSeconds: 1, TimeoutSeconds: 1, FailureThreshold: 1, SuccessThreshold: 1}
	mode := int32(0440)
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "source-config", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: r.OperationSecret, DefaultMode: &mode}}}, corev1.Volume{Name: "stage", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: r.Stage.Name}}})
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "source-config", MountPath: "/run/backup-source-config", ReadOnly: true}, corev1.VolumeMount{Name: "stage", MountPath: "/run/backup-stage"})
	for i, v := range r.SourceVolumes {
		name := "source-" + strconv.Itoa(i)
		pod.Volumes = append(pod.Volumes, corev1.Volume{Name: name, VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: v.Name, ReadOnly: true}}})
		container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: name, MountPath: "/run/backup-source/" + v.ID, ReadOnly: true})
	}
	pod.Volumes = append(pod.Volumes, corev1.Volume{Name: "status", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{Medium: corev1.StorageMediumMemory}}})
	container.VolumeMounts = append(container.VolumeMounts, corev1.VolumeMount{Name: "status", MountPath: "/run/backup-status"})
	if r.Phase == "capture" {
		// Private files can be 0600 under different owners. Only this collector
		// gets DAC_OVERRIDE; all source and root mounts remain read-only. No
		// global fsGroup is used, since it could change source volume ownership.
		zero := int64(0)
		pod.SecurityContext.RunAsNonRoot = &no
		pod.SecurityContext.RunAsUser = &zero
		pod.SecurityContext.RunAsGroup = &zero
		pod.SecurityContext.FSGroup = nil
		container.SecurityContext.Capabilities.Add = []corev1.Capability{"DAC_OVERRIDE"}
	}
	return job, r, nil
}

func matchesCaptureJob(want, got *batchv1.Job) bool {
	if !matchesVerifyJob(want, got) {
		return false
	}
	w, g := want.Spec.Template.Spec, got.Spec.Template.Spec
	if got.Labels["backup.clawmanager.io/role"] != want.Labels["backup.clawmanager.io/role"] || got.Spec.Template.Labels["backup.clawmanager.io/role"] != want.Spec.Template.Labels["backup.clawmanager.io/role"] || w.NodeName != "" && g.NodeName != w.NodeName || !reflect.DeepEqual(w.EnableServiceLinks, g.EnableServiceLinks) || !reflect.DeepEqual(w.TerminationGracePeriodSeconds, g.TerminationGracePeriodSeconds) || len(g.HostAliases) > 0 || g.DNSConfig != nil || g.RuntimeClassName != nil {
		return false
	}
	return true
}

func (j CaptureJobs) inputs(ctx context.Context, r CaptureJobRequest) error {
	if err := (VerifyJobs{Client: j.Client}).namespace(ctx, r.VerifyJobRequest); err != nil {
		return err
	}
	for _, ref := range []struct{ name, uid string }{{r.BootstrapSecret, r.BootstrapUID}, {r.OperationSecret, r.OperationSecretUID}} {
		s, err := j.Client.CoreV1().Secrets(r.Namespace).Get(ctx, ref.name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if string(s.UID) != ref.uid || s.Immutable == nil || !*s.Immutable || s.DeletionTimestamp != nil {
			return errors.New("isolation_invalid")
		}
	}
	volumes := []PVCIdentity{r.Stage}
	for _, v := range r.SourceVolumes {
		volumes = append(volumes, v.PVCIdentity)
	}
	for _, ref := range volumes {
		p, err := j.Client.CoreV1().PersistentVolumeClaims(r.Namespace).Get(ctx, ref.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		if string(p.UID) != ref.UID || p.DeletionTimestamp != nil || p.Spec.VolumeMode != nil && *p.Spec.VolumeMode != corev1.PersistentVolumeFilesystem {
			return errors.New("isolation_invalid")
		}
	}
	return nil
}

// allowUnbound is limited to preparation before gate acquisition. Once a UID
// is bound, a missing or replaced Job is an error, never another live-source read.
func (j CaptureJobs) Ensure(ctx context.Context, a Attempt, boundUID string, allowUnbound bool) (*batchv1.Job, error) {
	want, r, err := captureJob(a)
	if err != nil {
		return nil, err
	}
	if boundUID == "" && !allowUnbound {
		return nil, errors.New("coordination_invalid")
	}
	if err = j.inputs(ctx, r); err != nil {
		return nil, err
	}
	job, err := j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, want.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) && boundUID == "" && allowUnbound {
		job, err = j.Client.BatchV1().Jobs(r.Namespace).Create(ctx, want, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			job, err = j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, want.Name, metav1.GetOptions{})
		}
	}
	if err != nil {
		return nil, err
	}
	if !matchesCaptureJob(want, job) || boundUID != "" && string(job.UID) != boundUID {
		return nil, errors.New("artifact_conflict")
	}
	return job, nil
}

func (j CaptureJobs) Observe(ctx context.Context, a Attempt, boundUID string) (CaptureJobResult, error) {
	out := CaptureJobResult{State: "running", JobUID: boundUID}
	job, err := j.Ensure(ctx, a, boundUID, false)
	if err != nil {
		return out, err
	}
	_, r, _ := captureJob(a)
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
	pods, err := j.Client.CoreV1().Pods(r.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "backup.clawmanager.io/attempt=" + a.ID})
	if err != nil {
		return out, err
	}
	ownedCount := 0
	for _, p := range pods.Items {
		if !ownedByJob(p, job.UID) {
			continue
		}
		ownedCount++
		if ownedCount > 1 {
			return out, errors.New("coordination_invalid")
		}
		actual := job.DeepCopy()
		actual.Spec.Template.Spec = *p.Spec.DeepCopy()
		actual.Spec.Template.Labels = p.Labels
		if !matchesCaptureJob(job, actual) {
			return out, errors.New("isolation_invalid")
		}
		if complete {
			if len(p.Status.ContainerStatuses) != 1 {
				return out, errors.New("evidence_incomplete")
			}
			state := p.Status.ContainerStatuses[0]
			term := state.State.Terminated
			if state.Name != "collector" || term == nil || term.ExitCode != 0 || len(term.Message) > 4096 {
				return out, errors.New("evidence_incomplete")
			}
			var candidate CaptureCandidate
			if strictDecode([]byte(term.Message), &candidate) != nil || candidate.CapturePhaseRequest.Validate() != nil || candidate.AttemptID != a.ID || candidate.BackupID != r.BackupID || candidate.Origin != r.Origin || candidate.Phase != r.Phase || candidate.StageUID != r.Stage.UID || candidate.Gate != r.Gate || candidate.RegistryHash != RegistryHash() {
				return out, errors.New("evidence_incomplete")
			}
			out.State = "sealed"
			out.Candidate = &candidate
		} else {
			for _, condition := range p.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue && p.DeletionTimestamp == nil {
					out.State = "ready"
				}
			}
		}
	}
	if complete && out.Candidate == nil {
		return out, errors.New("evidence_incomplete")
	}
	return out, nil
}

func ownedByJob(p corev1.Pod, uid types.UID) bool {
	for _, o := range p.OwnerReferences {
		if o.UID == uid && o.Kind == "Job" && o.Controller != nil && *o.Controller {
			return true
		}
	}
	return false
}

func (j CaptureJobs) Stop(ctx context.Context, a Attempt, boundUID string) (bool, error) {
	want, r, err := captureJob(a)
	if err != nil {
		return false, err
	}
	if boundUID == "" {
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
			if ownedByJob(p, types.UID(boundUID)) {
				return false, nil
			}
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if string(job.UID) != boundUID || !matchesCaptureJob(want, job) {
		return false, errors.New("cleanup_unsafe_target")
	}
	uid := types.UID(boundUID)
	policy := metav1.DeletePropagationForeground
	err = j.Client.BatchV1().Jobs(r.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &policy})
	return false, err
}
