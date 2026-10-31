package systembackup

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"
	"regexp"
	"strconv"
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/client-go/kubernetes"
)

// VerifyJobRequest contains identities only, never credential bytes. The
// immutable bootstrap Secret contains config.json, key and credential files.
// Its configured paths must all be under /run/backup-bootstrap.
type VerifyJobRequest struct {
	Namespace        string    `json:"namespace"`
	NamespaceUID     string    `json:"namespace_uid"`
	Image            string    `json:"image"`
	ServiceAccount   string    `json:"service_account"`
	BootstrapSecret  string    `json:"bootstrap_secret"`
	Origin           string    `json:"origin"`
	BackupID         string    `json:"backup_id"`
	MaxBytes         int64     `json:"max_bytes"`
	DeadlineSeconds  int64     `json:"deadline_seconds"`
	DeadlineAt       time.Time `json:"deadline_at,omitempty"`
	ValidateSnapshot bool      `json:"validate_snapshot,omitempty"`
	BootstrapUID     string    `json:"bootstrap_uid,omitempty"`
	MaxItems         int       `json:"max_items,omitempty"`
}
type IntegrityReport struct {
	BackupID           string `json:"backup_id"`
	Origin             string `json:"origin_installation_id"`
	Purpose            string `json:"purpose"`
	Consistency        string `json:"consistency"`
	IntegrityVerified  bool   `json:"integrity_verified"`
	AcceptanceEligible bool   `json:"acceptance_eligible"`
	PartsVerified      int    `json:"parts_verified"`
	SnapshotValidated  bool   `json:"snapshot_validated,omitempty"`
	ManifestSHA256     string `json:"manifest_sha256,omitempty"`
	IndexSHA256        string `json:"index_sha256,omitempty"`
	CommitSHA256       string `json:"commit_sha256,omitempty"`
}
type VerifyJobResult struct {
	State   string           `json:"state"`
	JobUID  string           `json:"job_uid"`
	Report  *IntegrityReport `json:"report,omitempty"`
	Failure string           `json:"failure_category,omitempty"`
}
type VerifyJobs struct{ Client kubernetes.Interface }

func (j VerifyJobs) Existing(ctx context.Context, a Attempt) (*batchv1.Job, error) {
	want, r, err := verifyJob(a)
	if err != nil {
		return nil, err
	}
	if err = j.namespace(ctx, r); err != nil {
		return nil, err
	}
	job, err := j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, want.Name, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if !matchesVerifyJob(want, job) {
		return nil, errors.New("artifact_conflict")
	}
	return job, nil
}

var imageDigestPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9./:_-]+@sha256:[a-f0-9]{64}$`)
var attemptIDPattern = regexp.MustCompile(`^sat_[a-f0-9]{32}$`)

func (r VerifyJobRequest) Validate() error {
	if r.ValidateSnapshot && (r.MaxItems < 1 || r.MaxItems > 100000000) {
		return errors.New("validation_failed")
	}
	if r.BootstrapUID != "" && !refPattern.MatchString(r.BootstrapUID) || r.ValidateSnapshot && r.BootstrapUID == "" {
		return errors.New("validation_failed")
	}
	for _, v := range []string{r.Namespace, r.ServiceAccount, r.BootstrapSecret} {
		if len(validation.IsDNS1123Subdomain(v)) != 0 || v == "" {
			return errors.New("validation_failed")
		}
	}
	if r.NamespaceUID == "" || !imageDigestPattern.MatchString(r.Image) || !refPattern.MatchString(r.Origin) || !publicIDPattern.MatchString(r.BackupID) || r.BackupID[:4] != "sbk_" || r.MaxBytes < 1<<20 || r.MaxBytes > 900<<30 || r.DeadlineSeconds < 1 || r.DeadlineSeconds > 43200 {
		return errors.New("validation_failed")
	}
	return nil
}
func verifyJob(a Attempt) (*batchv1.Job, VerifyJobRequest, error) {
	var r VerifyJobRequest
	if !attemptIDPattern.MatchString(a.ID) || !publicIDPattern.MatchString(a.TaskID) || strictDecode(a.Request, &r) != nil || r.Validate() != nil {
		return nil, r, errors.New("validation_failed")
	}
	zero := int32(0)
	one := int32(1)
	yes := true
	no := false
	uid := int64(65532)
	h := sha256.Sum256(a.Request)
	labels := map[string]string{"backup.clawmanager.io/attempt": a.ID, "backup.clawmanager.io/task": a.TaskID}
	job := &batchv1.Job{ObjectMeta: metav1.ObjectMeta{Name: "backup-verify-" + a.ID[4:], Namespace: r.Namespace, Labels: labels, Annotations: map[string]string{"backup.clawmanager.io/request-sha256": hex.EncodeToString(h[:])}}, Spec: batchv1.JobSpec{BackoffLimit: &zero, Completions: &one, Parallelism: &one, ActiveDeadlineSeconds: &r.DeadlineSeconds, Template: corev1.PodTemplateSpec{ObjectMeta: metav1.ObjectMeta{Labels: labels}, Spec: corev1.PodSpec{RestartPolicy: corev1.RestartPolicyNever, ServiceAccountName: r.ServiceAccount, AutomountServiceAccountToken: &no, SecurityContext: &corev1.PodSecurityContext{RunAsNonRoot: &yes, RunAsUser: &uid, RunAsGroup: &uid, FSGroup: &uid, SeccompProfile: &corev1.SeccompProfile{Type: corev1.SeccompProfileTypeRuntimeDefault}}, Containers: []corev1.Container{{Name: "verify", Image: r.Image, ImagePullPolicy: corev1.PullIfNotPresent, Command: []string{"/usr/local/bin/system-backup-artifact"}, Args: []string{"--operation", "verify", "--bootstrap", "/run/backup-bootstrap/config.json", "--origin", r.Origin, "--backup", r.BackupID, "--max-bytes", strconv.FormatInt(r.MaxBytes, 10), "--timeout", strconv.FormatInt(r.DeadlineSeconds, 10) + "s", "--report-file", "/dev/termination-log"}, TerminationMessagePath: "/dev/termination-log", TerminationMessagePolicy: corev1.TerminationMessageReadFile, SecurityContext: &corev1.SecurityContext{AllowPrivilegeEscalation: &no, ReadOnlyRootFilesystem: &yes, Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}}}, Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("512Mi")}}, VolumeMounts: []corev1.VolumeMount{{Name: "bootstrap", MountPath: "/run/backup-bootstrap", ReadOnly: true}}}}, Volumes: []corev1.Volume{{Name: "bootstrap", VolumeSource: corev1.VolumeSource{Secret: &corev1.SecretVolumeSource{SecretName: r.BootstrapSecret, DefaultMode: func() *int32 { v := int32(0440); return &v }()}}}}}}}}
	// Newly frozen attempts bind an absolute process deadline as well as the
	// Kubernetes duration. Recreating a lost Job cannot restart its time budget.
	// Zero remains readable only for legacy frozen requests from older versions.
	if !r.DeadlineAt.IsZero() {
		job.Spec.Template.Spec.Containers[0].Args = append(job.Spec.Template.Spec.Containers[0].Args, "--deadline-at", r.DeadlineAt.UTC().Format(time.RFC3339Nano))
	}
	if r.ValidateSnapshot {
		job.Spec.Template.Spec.Containers[0].Args[1] = "verify-snapshot"
		job.Spec.Template.Spec.Containers[0].Args = append(job.Spec.Template.Spec.Containers[0].Args, "--max-items", strconv.Itoa(r.MaxItems))
	}
	return job, r, nil
}
func (j VerifyJobs) namespace(ctx context.Context, r VerifyJobRequest) error {
	n, e := j.Client.CoreV1().Namespaces().Get(ctx, r.Namespace, metav1.GetOptions{})
	if e != nil {
		return e
	}
	if string(n.UID) != r.NamespaceUID {
		return errors.New("isolation_invalid")
	}
	return nil
}

// Exact name + frozen request allows adoption after a lost Create response.
// A colliding or mutated Job is rejected, never overwritten or deleted.
func (j VerifyJobs) Ensure(ctx context.Context, a Attempt) (*batchv1.Job, error) {
	return j.EnsureBound(ctx, a, "", true)
}
func (j VerifyJobs) EnsureBound(ctx context.Context, a Attempt, uid string, create bool) (*batchv1.Job, error) {
	expected, r, e := verifyJob(a)
	if e != nil {
		return nil, e
	}
	if e = j.namespace(ctx, r); e != nil {
		return nil, e
	}
	job, e := j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, expected.Name, metav1.GetOptions{})
	if uid == "" && !create {
		return nil, errors.New("coordination_invalid")
	}
	if r.BootstrapUID != "" {
		secret, err := j.Client.CoreV1().Secrets(r.Namespace).Get(ctx, r.BootstrapSecret, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		if string(secret.UID) != r.BootstrapUID || secret.Immutable == nil || !*secret.Immutable || secret.DeletionTimestamp != nil {
			return nil, errors.New("isolation_invalid")
		}
	}
	if apierrors.IsNotFound(e) && uid == "" && create {
		job, e = j.Client.BatchV1().Jobs(r.Namespace).Create(ctx, expected, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(e) {
			job, e = j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, expected.Name, metav1.GetOptions{})
		}
	}
	if e != nil {
		return nil, e
	}
	if !matchesVerifyJob(expected, job) || uid != "" && string(job.UID) != uid {
		return nil, errors.New("artifact_conflict")
	}
	return job, nil
}
func matchesVerifyJob(want, got *batchv1.Job) bool {
	w, g := want.Spec.Template.Spec, got.Spec.Template.Spec
	// These labels select the verifier-only NetworkPolicy. A matching request
	// annotation is insufficient when isolation or the fixed budget changed.
	for _, key := range []string{"backup.clawmanager.io/attempt", "backup.clawmanager.io/task"} {
		if got.Labels[key] != want.Labels[key] || got.Spec.Template.Labels[key] != want.Spec.Template.Labels[key] {
			return false
		}
	}
	if got.Spec.Parallelism == nil || *got.Spec.Parallelism != 1 || got.Spec.Completions == nil || *got.Spec.Completions != 1 || len(g.EphemeralContainers) != 0 || (g.ShareProcessNamespace != nil && *g.ShareProcessNamespace) {
		return false
	}
	if got.Name != want.Name || got.Namespace != want.Namespace || got.Annotations["backup.clawmanager.io/request-sha256"] != want.Annotations["backup.clawmanager.io/request-sha256"] || got.Labels["backup.clawmanager.io/attempt"] != want.Labels["backup.clawmanager.io/attempt"] || len(g.Containers) != 1 || len(g.InitContainers) != 0 || g.HostNetwork || g.HostPID || g.HostIPC || g.ServiceAccountName != w.ServiceAccountName || g.AutomountServiceAccountToken == nil || *g.AutomountServiceAccountToken || g.RestartPolicy != w.RestartPolicy || !reflect.DeepEqual(g.Volumes, w.Volumes) || !reflect.DeepEqual(g.SecurityContext, w.SecurityContext) {
		return false
	}
	a, b := w.Containers[0], g.Containers[0]
	if !reflect.DeepEqual(a.Resources, b.Resources) || !reflect.DeepEqual(a.Lifecycle, b.Lifecycle) || !reflect.DeepEqual(a.LivenessProbe, b.LivenessProbe) || !reflect.DeepEqual(a.ReadinessProbe, b.ReadinessProbe) || !reflect.DeepEqual(a.StartupProbe, b.StartupProbe) {
		return false
	}
	return a.Name == b.Name && a.Image == b.Image && reflect.DeepEqual(a.Command, b.Command) && reflect.DeepEqual(a.Args, b.Args) && reflect.DeepEqual(a.VolumeMounts, b.VolumeMounts) && reflect.DeepEqual(a.SecurityContext, b.SecurityContext) && len(b.Env) == 0 && len(b.EnvFrom) == 0 && b.TerminationMessagePath == a.TerminationMessagePath && b.TerminationMessagePolicy == a.TerminationMessagePolicy && got.Spec.BackoffLimit != nil && *got.Spec.BackoffLimit == 0 && got.Spec.ActiveDeadlineSeconds != nil && *got.Spec.ActiveDeadlineSeconds == *want.Spec.ActiveDeadlineSeconds
}
func (j VerifyJobs) Observe(ctx context.Context, a Attempt) (VerifyJobResult, error) {
	job, e := j.Ensure(ctx, a)
	if e != nil {
		return VerifyJobResult{}, e
	}
	return j.observeJob(ctx, a, job)
}
func (j VerifyJobs) ObserveBound(ctx context.Context, a Attempt, uid string) (VerifyJobResult, error) {
	job, err := j.EnsureBound(ctx, a, uid, false)
	if err != nil {
		return VerifyJobResult{}, err
	}
	return j.observeJob(ctx, a, job)
}
func (j VerifyJobs) observeJob(ctx context.Context, a Attempt, job *batchv1.Job) (VerifyJobResult, error) {
	var out VerifyJobResult
	out.JobUID = string(job.UID)
	out.State = "running"
	complete, failed := false, false
	for _, c := range job.Status.Conditions {
		if c.Status == corev1.ConditionTrue {
			complete = complete || c.Type == batchv1.JobComplete
			failed = failed || c.Type == batchv1.JobFailed
		}
	}
	if failed {
		out.State = "failed"
		return out, nil
	}
	if !complete {
		return out, nil
	}
	_, r, _ := verifyJob(a)
	pods, e := j.Client.CoreV1().Pods(job.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "backup.clawmanager.io/attempt=" + a.ID})
	if e != nil {
		return out, e
	}
	var report *IntegrityReport
	for _, p := range pods.Items {
		owned := false
		for _, o := range p.OwnerReferences {
			owned = owned || (o.UID == job.UID && o.Kind == "Job" && o.Controller != nil && *o.Controller)
		}
		if !owned {
			continue
		}
		// Pod admission may change a template after Job creation. Check the
		// actual execution, not just the desired Job or its completion status.
		actual := job.DeepCopy()
		actual.Spec.Template.Spec = *p.Spec.DeepCopy()
		actual.Spec.Template.Labels = p.Labels
		if !matchesVerifyJob(job, actual) {
			return out, errors.New("isolation_invalid")
		}
		if len(p.Status.ContainerStatuses) != 1 {
			return out, errors.New("evidence_incomplete")
		}
		s := p.Status.ContainerStatuses[0]
		term := s.State.Terminated
		if s.Name != "verify" || term == nil || term.ExitCode != 0 || len(term.Message) > 4096 {
			return out, errors.New("evidence_incomplete")
		}
		var v IntegrityReport
		if strictDecode([]byte(term.Message), &v) != nil || v.BackupID != r.BackupID || v.Origin != r.Origin || !v.IntegrityVerified || v.AcceptanceEligible || v.PartsVerified != len(RequiredParts) || r.ValidateSnapshot && !v.SnapshotValidated || report != nil {
			return out, errors.New("evidence_incomplete")
		}
		report = &v
	}
	if report == nil {
		return out, errors.New("evidence_incomplete")
	}
	out.State = "succeeded"
	out.Report = report
	return out, nil
}

func (j VerifyJobs) StopBound(ctx context.Context, a Attempt, uid string) (bool, error) {
	if uid == "" {
		return false, errors.New("cleanup_unsafe_target")
	}
	a.Status = "succeeded"
	a.Result, _ = json.Marshal(VerifyJobResult{State: "succeeded", JobUID: uid})
	return j.Stop(ctx, a)
}

// Stop never creates an absent Job and only deletes the matching identity.
// Foreground deletion must complete before cancellation can be settled.
func (j VerifyJobs) Stop(ctx context.Context, a Attempt) (bool, error) {
	w, r, e := verifyJob(a)
	if e != nil {
		return false, e
	}
	if e = j.namespace(ctx, r); e != nil {
		return false, e
	}
	g, e := j.Client.BatchV1().Jobs(r.Namespace).Get(ctx, w.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(e) {
		var uid types.UID
		if a.Status == "succeeded" || a.Status == "failed" {
			var result VerifyJobResult
			if strictDecode(a.Result, &result) != nil || result.State != a.Status || result.JobUID == "" {
				return false, errors.New("cleanup_unsafe_target")
			}
			uid = types.UID(result.JobUID)
		}
		pods, err := j.Client.CoreV1().Pods(r.Namespace).List(ctx, metav1.ListOptions{LabelSelector: "backup.clawmanager.io/attempt=" + a.ID})
		if err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if uid != "" && ownedByJob(pod, uid) {
				return false, nil
			}
			if uid == "" {
				for _, owner := range pod.OwnerReferences {
					if owner.Kind == "Job" && owner.Name == w.Name && owner.Controller != nil && *owner.Controller {
						return false, nil
					}
				}
			}
		}
		return true, nil
	}
	if e != nil {
		return false, e
	}
	if !matchesVerifyJob(w, g) {
		return false, errors.New("cleanup_unsafe_target")
	}
	// A completed observation binds the original execution UID. A new object
	// with identical name/spec is not the execution whose result was persisted.
	if a.Status == "succeeded" || a.Status == "failed" {
		var result VerifyJobResult
		if strictDecode(a.Result, &result) != nil || result.State != a.Status || result.JobUID == "" || result.JobUID != string(g.UID) {
			return false, errors.New("cleanup_unsafe_target")
		}
	}
	uid := types.UID(g.UID)
	policy := metav1.DeletePropagationForeground
	e = j.Client.BatchV1().Jobs(r.Namespace).Delete(ctx, g.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}, PropagationPolicy: &policy})
	return false, e
}
