package systembackuptrial

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	batch "k8s.io/api/batch/v1"
	meta "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation"
	batchclient "k8s.io/client-go/kubernetes/typed/batch/v1"
	"k8s.io/client-go/rest"
)

const trialEnvironmentRevision = "clawmanager.io/trial-environment-revision"

var trialImage = regexp.MustCompile(`^[a-z0-9][a-z0-9./:_-]{0,254}@sha256:[a-f0-9]{64}$`)
var trialJobUID = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)

type trialEnvironment struct {
	Revision string         `json:"revision"`
	Side     string         `json:"side"`
	Source   trialPlacement `json:"source"`
	Target   trialPlacement `json:"target"`
	Job      *trialJob      `json:"prepare_job,omitempty"`
}

type trialPlacement struct {
	Cluster               string                `json:"cluster"`
	Namespace             string                `json:"namespace"`
	Image                 string                `json:"image"`
	ServiceAccount        string                `json:"service_account"`
	ArtifactClaim         string                `json:"artifact_claim"`
	ArtifactStorageClass  string                `json:"artifact_storage_class,omitempty"`
	WorkspaceClaim        string                `json:"workspace_claim,omitempty"`
	WorkspaceStorageClass string                `json:"workspace_storage_class,omitempty"`
	InstanceStorageClass  string                `json:"instance_storage_class,omitempty"`
	KeySecret             string                `json:"key_secret"`
	SourceReadSecret      string                `json:"source_read_secret,omitempty"`
	RestoreWriteSecret    string                `json:"restore_write_secret,omitempty"`
	RestoreReadSecret     string                `json:"restore_read_secret,omitempty"`
	RuntimeSecret         string                `json:"runtime_secret,omitempty"`
	HermesHistory         *trialHermesPlacement `json:"hermes_history,omitempty"`
	WorkspaceFiles        *workspaceSource      `json:"workspace_files,omitempty"`
}

type trialHermesPlacement struct {
	historySourceBinding
	Image           string `json:"image"`
	ScriptConfigMap string `json:"script_configmap"`
	RunAsUser       int    `json:"run_as_user"`
}

type trialJob struct {
	Name string    `json:"name"`
	UID  types.UID `json:"uid"`
}

type trialEnvironmentStatus struct {
	Profile *trialEnvironment `json:"profile,omitempty"`
	State   string            `json:"state"`
}

func (p trialEnvironment) local() trialPlacement {
	if p.Side == "source" {
		return p.Source
	}
	return p.Target
}

func (p trialPlacement) valid() bool {
	if len(validation.IsDNS1123Subdomain(p.Cluster)) != 0 || len(validation.IsDNS1123Label(p.Namespace)) != 0 || !trialImage.MatchString(p.Image) {
		return false
	}
	for _, required := range []string{p.ServiceAccount, p.ArtifactClaim, p.KeySecret} {
		if len(validation.IsDNS1123Subdomain(required)) != 0 {
			return false
		}
	}
	for _, optional := range []string{p.ArtifactStorageClass, p.WorkspaceClaim, p.WorkspaceStorageClass, p.InstanceStorageClass,
		p.SourceReadSecret, p.RestoreWriteSecret, p.RestoreReadSecret, p.RuntimeSecret} {
		if optional != "" && len(validation.IsDNS1123Subdomain(optional)) != 0 {
			return false
		}
	}
	return p.WorkspaceClaim != "" || p.WorkspaceStorageClass == ""
}

func (p trialPlacement) validHermesSource() bool {
	h := p.HermesHistory
	return h == nil || (p.WorkspaceClaim != "" && p.WorkspaceClaim != p.ArtifactClaim && h.UserID > 0 && h.InstanceID > 0 &&
		h.RunAsUser > 0 && trialImage.MatchString(h.Image) && len(validation.IsDNS1123Subdomain(h.ScriptConfigMap)) == 0)
}

func (s *Service) environmentProfile() (trialEnvironment, error) {
	var p trialEnvironment
	raw, err := readSetting(s.value("ENVIRONMENT_PROFILE_FILE"), 16<<10)
	if err != nil {
		return p, err
	}
	defer clear(raw)
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&p) != nil || decoder.Decode(new(any)) != io.EOF || !p.Source.valid() || !p.Target.valid() ||
		!p.Source.validHermesSource() || p.Target.HermesHistory != nil ||
		p.Target.WorkspaceFiles != nil || (p.Source.WorkspaceFiles != nil && !p.Source.WorkspaceFiles.valid(p.Source)) ||
		len(validation.IsDNS1123Label(p.Revision)) != 0 || (p.Side != "source" && p.Side != "target") ||
		!strings.HasPrefix(p.Target.Namespace, "cm-drill-") ||
		(p.Source.Cluster == p.Target.Cluster && p.Source.Namespace == p.Target.Namespace) ||
		p.Source.SourceReadSecret == "" || p.Source.RestoreReadSecret != "" || p.Source.RestoreWriteSecret != "" || p.Source.RuntimeSecret != "" ||
		p.Target.SourceReadSecret != "" || p.Target.RestoreReadSecret == "" || p.Target.RestoreWriteSecret == "" ||
		p.Target.RestoreReadSecret == p.Target.RestoreWriteSecret || p.Target.InstanceStorageClass == "" ||
		p.Target.RuntimeSecret == p.Target.RestoreWriteSecret || p.Target.RuntimeSecret == p.Target.RestoreReadSecret {
		return trialEnvironment{}, errConfig
	}
	if p.Side == "target" && (p.Target.Namespace != s.value("RESTORE_NAMESPACE") ||
		p.Target.InstanceStorageClass != s.value("RESTORE_STORAGE_CLASS")) {
		return trialEnvironment{}, errConfig
	}
	if p.Job != nil && (len(validation.IsDNS1123Label(p.Job.Name)) != 0 || !trialJobUID.MatchString(string(p.Job.UID))) {
		return trialEnvironment{}, errConfig
	}
	return p, nil
}

type environmentJobs interface {
	Get(context.Context, string, meta.GetOptions) (*batch.Job, error)
	Patch(context.Context, string, types.PatchType, []byte, meta.PatchOptions, ...string) (*batch.Job, error)
}

func inClusterEnvironmentJobs(namespace string) (environmentJobs, error) {
	local, err := readSetting("/var/run/secrets/kubernetes.io/serviceaccount/namespace", 128)
	if err != nil || strings.TrimSpace(string(local)) != namespace {
		return nil, errConfig
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	config.Timeout = 5 * time.Second
	client, err := batchclient.NewForConfig(config)
	if err != nil {
		return nil, err
	}
	return client.Jobs(namespace), nil
}

func (p trialEnvironment) jobState(job *batch.Job) string {
	if job == nil || job.Name != p.Job.Name || job.UID != p.Job.UID || job.Namespace != p.local().Namespace ||
		job.ResourceVersion == "" || job.Annotations[trialEnvironmentRevision] != p.Revision || job.DeletionTimestamp != nil {
		return "unavailable"
	}
	for _, condition := range job.Status.Conditions {
		if condition.Status != "True" {
			continue
		}
		switch condition.Type {
		case batch.JobFailed:
			return "failed"
		case batch.JobComplete:
			return "prepared"
		}
	}
	if job.Spec.Suspend != nil && *job.Spec.Suspend {
		return "waiting"
	}
	return "running"
}

func (s *Service) environment(ctx context.Context, prepare bool) trialEnvironmentStatus {
	if s.value("ENVIRONMENT_PROFILE_FILE") == "" {
		return trialEnvironmentStatus{State: "unregistered"}
	}
	p, err := s.environmentProfile()
	if err != nil {
		return trialEnvironmentStatus{State: "invalid"}
	}
	result := trialEnvironmentStatus{Profile: &p, State: "registered"}
	if p.Job == nil {
		return result
	}
	result.State = "unavailable"
	openJobs := s.environmentJobs
	if openJobs == nil {
		openJobs = inClusterEnvironmentJobs
	}
	jobs, err := openJobs(p.local().Namespace)
	if err != nil {
		return result
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	job, err := jobs.Get(ctx, p.Job.Name, meta.GetOptions{})
	if err != nil {
		return result
	}
	result.State = p.jobState(job)
	if !prepare || result.State != "waiting" {
		return result
	}
	patch, _ := json.Marshal([]struct {
		Op    string `json:"op"`
		Path  string `json:"path"`
		Value any    `json:"value"`
	}{
		{"test", "/metadata/uid", job.UID},
		{"test", "/metadata/resourceVersion", job.ResourceVersion},
		{"test", "/spec/suspend", true},
		{"replace", "/spec/suspend", false},
	})
	updated, err := jobs.Patch(ctx, p.Job.Name, types.JSONPatchType, patch, meta.PatchOptions{})
	if err != nil {
		// An overlapping click or a lost response may have already started the Job.
		updated, err = jobs.Get(ctx, p.Job.Name, meta.GetOptions{})
		if err != nil || p.jobState(updated) == "waiting" {
			return trialEnvironmentStatus{Profile: &p, State: "unavailable"}
		}
	}
	result.State = p.jobState(updated)
	return result
}

func (e trialEnvironmentStatus) missing(side string) string {
	if e.State == "unregistered" {
		return ""
	}
	if e.Profile != nil && e.Profile.Side != side {
		return "trial_environment_" + side
	}
	if e.State != "registered" && e.State != "prepared" {
		return "trial_environment_preparation"
	}
	return ""
}

func (s *Service) handlePrepareEnvironment(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	if c.Request.ContentLength != 0 || len(c.Request.URL.RawQuery) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": "invalid_trial_preparation"})
		return
	}
	s.mu.Lock()
	if s.checking || (s.task != nil && s.task.Status == "running") {
		s.mu.Unlock()
		c.JSON(http.StatusConflict, gin.H{"code": "trial_task_running"})
		return
	}
	s.checking = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.checking = false
		s.mu.Unlock()
	}()
	result := s.environment(c.Request.Context(), true)
	c.JSON(http.StatusOK, result)
}
