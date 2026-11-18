package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/browserorigin"
	"clawreef/internal/models"
	"clawreef/internal/workbench"
)

var (
	ErrWorkbenchDenied      = errors.New("workbench access denied")
	ErrWorkbenchStale       = errors.New("instance environment changed; refresh the workbench")
	ErrWorkbenchUnavailable = errors.New("start the instance to use the workbench")
)

type WorkbenchRequest struct {
	workbench.PreviewScope
	Operation string          `json:"operation"`
	Data      json.RawMessage `json:"data,omitempty"`
}
type WorkbenchAgent interface {
	Workbench(context.Context, string, WorkbenchRequest) (json.RawMessage, error)
}
type workbenchInstances interface {
	GetByID(int) (*models.Instance, error)
}
type workbenchBindings interface {
	GetRunningByInstanceID(context.Context, int) (*models.InstanceRuntimeBinding, error)
}
type workbenchPods interface {
	GetByID(context.Context, int64) (*models.RuntimePod, error)
}
type InstanceWorkbenchService struct {
	instances              workbenchInstances
	bindings               workbenchBindings
	pods                   workbenchPods
	agent                  WorkbenchAgent
	gitFiles               WorkspaceFileService
	previews               *workbench.Previews
	browserOrigins         *browserorigin.Registry
	previewRecheckInterval time.Duration
	sharedPreviewAccess    interface {
		ResolveShortLink(context.Context, string) (*models.InstanceExternalAccess, error)
	}
}
type workbenchTarget struct {
	scope               workbench.PreviewScope
	endpoint, workspace string
	gatewayPort         int
}

func NewInstanceWorkbenchService(instances workbenchInstances, bindings workbenchBindings, pods workbenchPods, agent WorkbenchAgent, previews *workbench.Previews) *InstanceWorkbenchService {
	return &InstanceWorkbenchService{instances: instances, bindings: bindings, pods: pods, agent: agent, previews: previews}
}
func (s *InstanceWorkbenchService) resolve(ctx context.Context, userID int, admin bool, instanceID, generation int) (workbenchTarget, error) {
	var target workbenchTarget
	if userID <= 0 || instanceID <= 0 {
		return target, ErrWorkbenchDenied
	}
	instance, err := s.instances.GetByID(instanceID)
	if err != nil || instance == nil || (!admin && instance.UserID != userID) {
		return target, ErrWorkbenchDenied
	}
	if instance.Status != "running" {
		return target, ErrWorkbenchUnavailable
	}
	if generation != 0 && generation != instance.RuntimeGeneration {
		return target, ErrWorkbenchStale
	}
	binding, err := s.bindings.GetRunningByInstanceID(ctx, instanceID)
	if err != nil || binding == nil {
		if instance.RuntimeGeneration <= 0 {
			return target, ErrRuntimeAgentUnsupported
		}
		return target, ErrWorkbenchUnavailable
	}
	if binding.InstanceID != instanceID || binding.Generation != instance.RuntimeGeneration {
		return target, ErrWorkbenchStale
	}
	scope := workbench.PreviewScope{InstanceID: instanceID, UserID: instance.UserID, Generation: binding.Generation, GatewayID: binding.GatewayID}
	if !scope.Valid() {
		return target, ErrWorkbenchStale
	}
	pod, err := s.pods.GetByID(ctx, binding.RuntimePodID)
	if err != nil || pod == nil || pod.AgentEndpoint == nil || strings.TrimSpace(*pod.AgentEndpoint) == "" || pod.Draining || pod.State == "offline" || pod.State == "unhealthy" {
		return target, ErrWorkbenchUnavailable
	}
	return workbenchTarget{scope: scope, endpoint: *pod.AgentEndpoint, workspace: binding.WorkspacePath, gatewayPort: binding.GatewayPort}, nil
}
func (s *InstanceWorkbenchService) Execute(ctx context.Context, userID int, admin bool, instanceID, generation int, operation string, data json.RawMessage) (json.RawMessage, error) {
	if generation <= 0 {
		return nil, ErrWorkbenchStale
	}
	target, err := s.resolve(ctx, userID, admin, instanceID, generation)
	if err != nil {
		return nil, err
	}
	return s.execute(ctx, target, operation, data)
}
func (s *InstanceWorkbenchService) execute(ctx context.Context, target workbenchTarget, operation string, data json.RawMessage) (json.RawMessage, error) {
	if operation == "git" {
		return s.executeGit(ctx, target, data)
	}
	return s.executeAgent(ctx, target, operation, data)
}
func (s *InstanceWorkbenchService) executeAgent(ctx context.Context, target workbenchTarget, operation string, data json.RawMessage) (json.RawMessage, error) {
	if s.agent == nil {
		return nil, ErrRuntimeAgentUnsupported
	}
	ctx, cancel := context.WithTimeout(ctx, 125*time.Second)
	defer cancel()
	return s.agent.Workbench(ctx, target.endpoint, WorkbenchRequest{PreviewScope: target.scope, Operation: operation, Data: data})
}
func (s *InstanceWorkbenchService) Capabilities(ctx context.Context, userID int, admin bool, instanceID int) (map[string]any, error) {
	target, err := s.resolve(ctx, userID, admin, instanceID, 0)
	if err != nil {
		return nil, err
	}
	response, err := s.execute(ctx, target, "capabilities", nil)
	if err != nil {
		return nil, err
	}
	var caps map[string]any
	if err = json.Unmarshal(response, &caps); err != nil || caps == nil {
		return nil, ErrWorkbenchUnavailable
	}
	if s.browserOrigins != nil {
		caps["browser_origin_mode"] = "chrome-single-host"
		caps["manager_origin"] = s.browserOrigins.ManagerOrigin()
	}
	caps["generation"] = target.scope.Generation
	caps["workspace"] = target.workspace
	caps["preview"] = caps["preview"] == true && s.previews != nil
	return caps, nil
}
func (s *InstanceWorkbenchService) SetSharedPreviewAccess(access interface {
	ResolveShortLink(context.Context, string) (*models.InstanceExternalAccess, error)
}) { s.sharedPreviewAccess = access }
func (s *InstanceWorkbenchService) CreatePreview(ctx context.Context, userID int, admin bool, instanceID, generation int, rawURL string) (map[string]any, error) {
	return s.createPreview(ctx, userID, admin, instanceID, generation, rawURL, nil)
}
func (s *InstanceWorkbenchService) CreateSharedPreview(ctx context.Context, userID, instanceID, generation int, rawURL string, shared workbench.SharedPreviewAccess) (map[string]any, error) {
	if !s.validSharedPreview(ctx, instanceID, &shared) {
		return nil, ErrWorkbenchDenied
	}
	return s.createPreview(ctx, userID, false, instanceID, generation, rawURL, &shared)
}
func (s *InstanceWorkbenchService) validSharedPreview(ctx context.Context, instanceID int, shared *workbench.SharedPreviewAccess) bool {
	if shared == nil {
		return true
	}
	if s.sharedPreviewAccess == nil || !shared.Valid() || !time.Now().Before(shared.ExpiresAt) {
		return false
	}
	access, err := s.sharedPreviewAccess.ResolveShortLink(ctx, shared.Code)
	if err != nil || access == nil || !access.Enabled || access.InstanceID != instanceID || (access.ExpiresAt != nil && !time.Now().Before(*access.ExpiresAt)) {
		return false
	}
	if access.AuthMode != ExternalAccessModeShareLink && access.AuthMode != ExternalAccessModePassword {
		return false
	}
	permission, err := NormalizeExternalWorkspaceAccess(access.WorkspaceAccess)
	return err == nil && permission == shared.WorkspaceAccess && ExternalAccessSessionBinding(shared.Code, access) == shared.SessionBinding
}
func (s *InstanceWorkbenchService) createPreview(ctx context.Context, userID int, admin bool, instanceID, generation int, rawURL string, shared *workbench.SharedPreviewAccess) (map[string]any, error) {
	if generation <= 0 {
		return nil, ErrWorkbenchStale
	}
	target, err := s.resolve(ctx, userID, admin, instanceID, generation)
	if err != nil {
		return nil, err
	}
	if s.previews == nil {
		return nil, ErrRuntimeAgentUnsupported
	}
	parsed, err := workbench.ParsePreviewURL(rawURL)
	if err != nil {
		return nil, err
	}
	if parsed.Port == target.gatewayPort {
		return nil, workbench.ErrInvalidPreviewURL
	}
	response, err := s.execute(ctx, target, "capabilities", nil)
	if err != nil {
		return nil, err
	}
	var caps struct {
		Preview bool `json:"preview"`
	}
	if json.Unmarshal(response, &caps) != nil || !caps.Preview {
		return nil, ErrRuntimeAgentUnsupported
	}
	var originBinding browserorigin.Binding
	if s.browserOrigins != nil {
		originBinding, err = s.browserOrigins.Bind(ctx, instanceID, browserorigin.Preview, browserPreviewTargetKey(parsed))
		if err != nil {
			return nil, err
		}
	}
	var lease workbench.PreviewLease
	if shared != nil {
		lease, err = s.previews.CreateShared(ctx, target.scope, rawURL, *shared)
	} else {
		lease, err = s.previews.Create(ctx, target.scope, rawURL)
	}
	if err != nil {
		return nil, err
	}
	result := map[string]any{"id": lease.ID, "url": s.previews.URL(lease), "generation": generation}
	if s.browserOrigins != nil {
		result["url"] = s.browserPreviewURL(originBinding, lease)
		result["viewer_url"] = s.browserPreviewViewerURL(originBinding, lease)
		result["credentialless"] = true
	}
	return result, nil
}
func (s *InstanceWorkbenchService) previewTarget(ctx context.Context, host string) (workbench.PreviewLease, workbenchTarget, error) {
	lease, err := s.previews.LookupHost(ctx, host)
	if err != nil {
		return lease, workbenchTarget{}, err
	}
	return s.resolvePreviewLease(ctx, lease)
}
func (s *InstanceWorkbenchService) resolvePreviewLease(ctx context.Context, lease workbench.PreviewLease) (workbench.PreviewLease, workbenchTarget, error) {
	if !s.validSharedPreview(ctx, lease.Scope.InstanceID, lease.Shared) {
		return lease, workbenchTarget{}, workbench.ErrPreviewNotFound
	}
	target, err := s.resolve(ctx, lease.Scope.UserID, false, lease.Scope.InstanceID, lease.Scope.Generation)
	if err != nil || target.scope != lease.Scope || lease.Target.Port == target.gatewayPort {
		return lease, target, fmt.Errorf("%w", workbench.ErrPreviewNotFound)
	}
	return lease, target, nil
}
