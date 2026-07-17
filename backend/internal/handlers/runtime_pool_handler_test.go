package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawreef/internal/middleware"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"

	"github.com/gin-gonic/gin"
)

func TestRuntimePoolHandlerRejectsNonAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{}, &runtimePoolHandlerBindingRepo{}, &runtimePoolHandlerRolloutRepo{}, nil, &runtimePoolHandlerEvents{})

	router := runtimePoolHandlerRouter(20, "user", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime-pods", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, body = %s, want 403", rec.Code, rec.Body.String())
	}
}

func TestRuntimePoolHandlerListPodsReturnsMetricsForAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	podIP := "10.42.0.31"
	nodeName := "node-a"
	handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{pods: []models.RuntimePod{{
		ID:              9,
		RuntimeType:     "openclaw",
		PodName:         "openclaw-runtime-abcde",
		PodIP:           &podIP,
		NodeName:        &nodeName,
		State:           "ready",
		UsedSlots:       37,
		Capacity:        100,
		Draining:        false,
		CPUMillisUsed:   13600,
		MemoryBytesUsed: 42949672960,
		DiskBytesUsed:   214748364800,
		NetworkRXBytes:  9223372,
		NetworkTXBytes:  19223372,
		LastSeenAt:      &now,
	}}}, &runtimePoolHandlerBindingRepo{}, &runtimePoolHandlerRolloutRepo{}, nil, &runtimePoolHandlerEvents{})

	router := runtimePoolHandlerRouter(1, "admin", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime-pods", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			Pods []struct {
				ID              int64  `json:"id"`
				RuntimeType     string `json:"runtime_type"`
				PodName         string `json:"pod_name"`
				CPUMillisUsed   int64  `json:"cpu_millis_used"`
				MemoryBytesUsed int64  `json:"memory_bytes_used"`
				DiskBytesUsed   int64  `json:"disk_bytes_used"`
				NetworkRXBytes  int64  `json:"network_rx_bytes"`
				NetworkTXBytes  int64  `json:"network_tx_bytes"`
				LastSeenAt      string `json:"last_seen_at"`
			} `json:"pods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data.Pods) != 1 {
		t.Fatalf("pods length = %d, want 1", len(resp.Data.Pods))
	}
	pod := resp.Data.Pods[0]
	if pod.ID != 9 || pod.RuntimeType != "openclaw" || pod.PodName != "openclaw-runtime-abcde" {
		t.Fatalf("pod identity = %#v, want runtime pod data", pod)
	}
	if pod.CPUMillisUsed != 13600 || pod.MemoryBytesUsed != 42949672960 || pod.DiskBytesUsed != 214748364800 || pod.NetworkRXBytes != 9223372 || pod.NetworkTXBytes != 19223372 {
		t.Fatalf("pod metrics = %#v, want persisted metrics", pod)
	}
}

func TestRuntimePoolHandlerListPodsOmitsStaleRuntimePods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recentSeen := time.Now().UTC()
	staleSeen := recentSeen.Add(-5 * time.Minute)
	handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{pods: []models.RuntimePod{
		{ID: 1, RuntimeType: "openclaw", PodName: "openclaw-runtime-live", State: "ready", LastSeenAt: &recentSeen},
		{ID: 2, RuntimeType: "openclaw", PodName: "openclaw-runtime-deleted", State: "unhealthy", LastSeenAt: &staleSeen},
		{ID: 3, RuntimeType: "hermes", PodName: "hermes-runtime-recent-unhealthy", State: "unhealthy", LastSeenAt: &recentSeen},
	}}, &runtimePoolHandlerBindingRepo{}, &runtimePoolHandlerRolloutRepo{}, nil, &runtimePoolHandlerEvents{})

	router := runtimePoolHandlerRouter(1, "admin", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime-pods", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			Pods []struct {
				ID      int64  `json:"id"`
				PodName string `json:"pod_name"`
			} `json:"pods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data.Pods) != 2 {
		t.Fatalf("pods length = %d, want 2: %#v", len(resp.Data.Pods), resp.Data.Pods)
	}
	for _, pod := range resp.Data.Pods {
		if pod.ID == 2 || pod.PodName == "openclaw-runtime-deleted" {
			t.Fatalf("stale deleted pod was returned: %#v", pod)
		}
	}
}

func TestRuntimePoolHandlerListPodsIncludesUnreportedDeploymentPods(t *testing.T) {
	gin.SetMode(gin.TestMode)
	deployments := &runtimePoolHandlerDeploymentService{
		pods: []k8s.RuntimeDeploymentPod{{
			RuntimeType:    "openclaw",
			Namespace:      "clawmanager-system",
			DeploymentName: "openclaw-runtime",
			PodName:        "openclaw-runtime-current",
			ImageRef:       "registry/openclaw-lite:final2",
			State:          "ready",
		}},
	}
	scheduler := services.NewRuntimeScheduler(
		nil,
		&runtimePoolHandlerPodRepo{},
		nil,
		&runtimePoolHandlerRolloutRepo{},
		&runtimePoolHandlerAgentClient{},
		services.NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
		services.WithRuntimeSchedulerNamespace("clawmanager-system"),
		services.WithRuntimeSchedulerMaxGatewaysPerPod(33),
	)
	handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{}, &runtimePoolHandlerBindingRepo{}, &runtimePoolHandlerRolloutRepo{}, scheduler, &runtimePoolHandlerEvents{})

	router := runtimePoolHandlerRouter(1, "admin", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime-pods?runtime_type=openclaw", nil)
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s, want 200", rec.Code, rec.Body.String())
	}

	var resp struct {
		Data struct {
			Pods []struct {
				ID             int64  `json:"id"`
				RuntimeType    string `json:"runtime_type"`
				PodName        string `json:"pod_name"`
				ImageRef       string `json:"image_ref"`
				State          string `json:"state"`
				Capacity       int    `json:"capacity"`
				AgentReported  bool   `json:"agent_reported"`
				DeploymentName string `json:"deployment_name"`
			} `json:"pods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(resp.Data.Pods) != 1 {
		t.Fatalf("pods length = %d, want 1: %#v", len(resp.Data.Pods), resp.Data.Pods)
	}
	pod := resp.Data.Pods[0]
	if pod.ID >= 0 || pod.AgentReported {
		t.Fatalf("fallback pod identity = id:%d agent_reported:%t, want synthetic unreported pod", pod.ID, pod.AgentReported)
	}
	if pod.RuntimeType != "openclaw" || pod.PodName != "openclaw-runtime-current" || pod.DeploymentName != "openclaw-runtime" {
		t.Fatalf("fallback pod metadata = %#v, want OpenClaw deployment pod", pod)
	}
	if pod.ImageRef != "registry/openclaw-lite:final2" {
		t.Fatalf("fallback pod image = %q, want registry/openclaw-lite:final2", pod.ImageRef)
	}
	if pod.State != "unhealthy" {
		t.Fatalf("fallback pod state = %q, want unhealthy while agent is not reporting", pod.State)
	}
	if pod.Capacity != 33 {
		t.Fatalf("fallback pod capacity = %d, want configured capacity 33", pod.Capacity)
	}
}

func TestRuntimePoolHandlerStartRolloutStoresRequesterAndPublishesEvent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rolloutRepo := &runtimePoolHandlerRolloutRepo{}
	events := &runtimePoolHandlerEvents{}
	handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{}, &runtimePoolHandlerBindingRepo{}, rolloutRepo, nil, events)

	router := runtimePoolHandlerRouter(7, "admin", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/runtime-rollouts", bytes.NewBufferString(`{
		"runtime_type": "hermes",
		"target_image_ref": "ghcr.io/example/hermes:v2",
		"batch_size": 2,
		"max_unavailable": 1
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", rec.Code, rec.Body.String())
	}
	if rolloutRepo.created == nil {
		t.Fatalf("rollout was not created")
	}
	if rolloutRepo.created.StartedBy == nil || *rolloutRepo.created.StartedBy != 7 {
		t.Fatalf("started_by = %#v, want 7", rolloutRepo.created.StartedBy)
	}
	if events.lastType != "runtime_rollout" {
		t.Fatalf("event type = %q, want runtime_rollout", events.lastType)
	}
}

func TestRuntimePoolHandlerStartRolloutRunsSchedulerImmediately(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rolloutRepo := &runtimePoolHandlerRolloutRepo{}
	podRepo := &runtimePoolHandlerPodRepo{pods: []models.RuntimePod{{
		ID:             21,
		RuntimeType:    "hermes",
		Namespace:      "clawmanager-system",
		PodName:        "hermes-runtime-old",
		DeploymentName: "hermes-runtime",
		ImageRef:       "registry/hermes:v1",
		State:          "ready",
		Capacity:       100,
		UsedSlots:      7,
	}}}
	deployments := &runtimePoolHandlerDeploymentService{}
	scheduler := services.NewRuntimeScheduler(
		nil,
		podRepo,
		nil,
		rolloutRepo,
		&runtimePoolHandlerAgentClient{},
		services.NewRuntimeEventService(nil),
		nil,
		deployments,
		time.Second,
	)
	handler := NewRuntimePoolHandler(podRepo, &runtimePoolHandlerBindingRepo{}, rolloutRepo, scheduler, &runtimePoolHandlerEvents{})

	router := runtimePoolHandlerRouter(7, "admin", handler)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/runtime-rollouts", bytes.NewBufferString(`{
		"runtime_type": "hermes",
		"target_image_ref": "registry/hermes:v2",
		"batch_size": 1,
		"max_unavailable": 1
	}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s, want 201", rec.Code, rec.Body.String())
	}
	if got := len(deployments.rollouts); got != 1 {
		t.Fatalf("deployment rollouts = %d, want 1", got)
	}
	rollout := deployments.rollouts[0]
	if rollout.namespace != "clawmanager-system" || rollout.name != "hermes-runtime" || rollout.image != "registry/hermes:v2" {
		t.Fatalf("deployment rollout = %+v, want hermes-runtime registry/hermes:v2", rollout)
	}
	if podRepo.markedPodID != 21 || podRepo.markedState != "draining" || !podRepo.markedDraining {
		t.Fatalf("pod mark = id:%d state:%q draining:%t, want drain pod 21", podRepo.markedPodID, podRepo.markedState, podRepo.markedDraining)
	}
}

func runtimePoolHandlerRouter(userID int, role string, handler *RuntimePoolHandler) *gin.Engine {
	userRepo := &runtimePoolHandlerUserRepo{users: map[int]*models.User{
		userID: {ID: userID, Username: "tester", Role: role},
	}}
	router := gin.New()
	admin := router.Group("/api/v1/admin")
	admin.Use(func(c *gin.Context) {
		c.Set("userID", userID)
		c.Next()
	})
	admin.Use(middleware.SetUserInfo(userRepo))
	admin.Use(middleware.NewAdminAuth(userRepo))
	{
		admin.GET("/runtime-pods", handler.ListPods)
		admin.POST("/runtime-rollouts", handler.StartRollout)
	}
	return router
}

type runtimePoolHandlerUserRepo struct {
	users map[int]*models.User
}

func (r *runtimePoolHandlerUserRepo) Create(user *models.User) error { return nil }
func (r *runtimePoolHandlerUserRepo) GetByID(id int) (*models.User, error) {
	return r.users[id], nil
}
func (r *runtimePoolHandlerUserRepo) GetByUsername(username string) (*models.User, error) {
	return nil, nil
}
func (r *runtimePoolHandlerUserRepo) GetByEmail(email string) (*models.User, error) {
	return nil, nil
}
func (r *runtimePoolHandlerUserRepo) Update(user *models.User) error { return nil }
func (r *runtimePoolHandlerUserRepo) Delete(id int) error            { return nil }
func (r *runtimePoolHandlerUserRepo) List(offset, limit int) ([]models.User, error) {
	return nil, nil
}
func (r *runtimePoolHandlerUserRepo) Count() (int, error) { return 0, nil }

type runtimePoolHandlerPodRepo struct {
	pods           []models.RuntimePod
	markedPodID    int64
	markedState    string
	markedDraining bool
}

func (r *runtimePoolHandlerPodRepo) UpsertFromAgent(ctx context.Context, pod *models.RuntimePod) error {
	return nil
}
func (r *runtimePoolHandlerPodRepo) GetByID(ctx context.Context, id int64) (*models.RuntimePod, error) {
	for _, pod := range r.pods {
		if pod.ID == id {
			cp := pod
			return &cp, nil
		}
	}
	return nil, nil
}
func (r *runtimePoolHandlerPodRepo) GetByNamespaceName(ctx context.Context, namespace, podName string) (*models.RuntimePod, error) {
	return nil, nil
}
func (r *runtimePoolHandlerPodRepo) List(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	if runtimeType == "" {
		return append([]models.RuntimePod(nil), r.pods...), nil
	}
	var pods []models.RuntimePod
	for _, pod := range r.pods {
		if pod.RuntimeType == runtimeType {
			pods = append(pods, pod)
		}
	}
	return pods, nil
}
func (r *runtimePoolHandlerPodRepo) ListSchedulable(ctx context.Context, runtimeType string) ([]models.RuntimePod, error) {
	return nil, nil
}
func (r *runtimePoolHandlerPodRepo) TryClaimSlot(ctx context.Context, podID int64) (bool, error) {
	return false, nil
}
func (r *runtimePoolHandlerPodRepo) ReleaseSlot(ctx context.Context, podID int64) error {
	return nil
}
func (r *runtimePoolHandlerPodRepo) MarkState(ctx context.Context, podID int64, state string, draining bool) error {
	r.markedPodID = podID
	r.markedState = state
	r.markedDraining = draining
	return nil
}
func (r *runtimePoolHandlerPodRepo) UpdateHeartbeat(ctx context.Context, podID int64, state string, usedSlots int, capacity int, draining bool, lastSeenAt time.Time) error {
	return nil
}
func (r *runtimePoolHandlerPodRepo) MarkUnseenUnhealthy(ctx context.Context, cutoff time.Time) error {
	return nil
}
func (r *runtimePoolHandlerPodRepo) UpdateMetrics(ctx context.Context, podID int64, metrics repository.RuntimePodMetricsUpdate) error {
	return nil
}

type runtimePoolHandlerBindingRepo struct {
	bindings []models.InstanceRuntimeBinding
}

func (r *runtimePoolHandlerBindingRepo) Create(ctx context.Context, binding *models.InstanceRuntimeBinding) error {
	return nil
}
func (r *runtimePoolHandlerBindingRepo) GetByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error) {
	return nil, nil
}
func (r *runtimePoolHandlerBindingRepo) GetRunningByInstanceID(ctx context.Context, instanceID int) (*models.InstanceRuntimeBinding, error) {
	return nil, nil
}
func (r *runtimePoolHandlerBindingRepo) ListByRuntimePodID(ctx context.Context, runtimePodID int64) ([]models.InstanceRuntimeBinding, error) {
	var bindings []models.InstanceRuntimeBinding
	for _, binding := range r.bindings {
		if binding.RuntimePodID == runtimePodID {
			bindings = append(bindings, binding)
		}
	}
	return bindings, nil
}
func (r *runtimePoolHandlerBindingRepo) ListByRuntimePodIDs(ctx context.Context, runtimePodIDs []int64) ([]models.InstanceRuntimeBinding, error) {
	return nil, nil
}
func (r *runtimePoolHandlerBindingRepo) UpdateRunning(ctx context.Context, instanceID int, generation int, gatewayID string, port int, pid *int) error {
	return nil
}
func (r *runtimePoolHandlerBindingRepo) UpdateState(ctx context.Context, instanceID int, generation int, state string, message *string) error {
	return nil
}
func (r *runtimePoolHandlerBindingRepo) DeleteErrorByRuntimePodIDAndGatewayPort(ctx context.Context, runtimePodID int64, gatewayPort int) (int64, error) {
	return 0, nil
}
func (r *runtimePoolHandlerBindingRepo) DeleteByInstanceID(ctx context.Context, instanceID int) error {
	return nil
}
func (r *runtimePoolHandlerBindingRepo) DeleteByInstanceIDAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64) error {
	return nil
}
func (r *runtimePoolHandlerBindingRepo) DeleteRunningByInstanceIDGenerationAndReleaseSlot(ctx context.Context, instanceID int, runtimePodID int64, generation int) (bool, error) {
	return false, nil
}

type runtimePoolHandlerRolloutRepo struct {
	created *models.RuntimeRollout
	byID    map[int64]*models.RuntimeRollout
}

func (r *runtimePoolHandlerRolloutRepo) Create(ctx context.Context, rollout *models.RuntimeRollout) error {
	rollout.ID = 12
	cp := *rollout
	r.created = &cp
	if r.byID == nil {
		r.byID = map[int64]*models.RuntimeRollout{}
	}
	r.byID[rollout.ID] = &cp
	return nil
}
func (r *runtimePoolHandlerRolloutRepo) GetByID(ctx context.Context, id int64) (*models.RuntimeRollout, error) {
	if r.byID == nil {
		return nil, nil
	}
	return r.byID[id], nil
}
func (r *runtimePoolHandlerRolloutRepo) ListActive(ctx context.Context, runtimeType string) ([]models.RuntimeRollout, error) {
	return nil, nil
}
func (r *runtimePoolHandlerRolloutRepo) UpdateStatus(ctx context.Context, id int64, status string, startedAt *time.Time, finishedAt *time.Time, message *string) error {
	return nil
}

type runtimePoolHandlerEvents struct {
	lastType    string
	lastPayload any
}

func (e *runtimePoolHandlerEvents) Publish(ctx context.Context, eventType string, payload any) error {
	e.lastType = eventType
	e.lastPayload = payload
	return nil
}

type runtimePoolHandlerDeploymentService struct {
	rollouts []runtimePoolHandlerDeploymentRollout
	pods     []k8s.RuntimeDeploymentPod
}

type runtimePoolHandlerDeploymentRollout struct {
	namespace      string
	name           string
	image          string
	maxUnavailable int
	maxSurge       int
}

func (s *runtimePoolHandlerDeploymentService) Ensure(ctx context.Context, spec k8s.RuntimeDeploymentSpec) error {
	return nil
}
func (s *runtimePoolHandlerDeploymentService) Scale(ctx context.Context, namespace, name string, replicas int32) error {
	return nil
}
func (s *runtimePoolHandlerDeploymentService) RolloutImage(ctx context.Context, namespace, name, image string, maxUnavailable, maxSurge int) error {
	s.rollouts = append(s.rollouts, runtimePoolHandlerDeploymentRollout{
		namespace:      namespace,
		name:           name,
		image:          image,
		maxUnavailable: maxUnavailable,
		maxSurge:       maxSurge,
	})
	return nil
}
func (s *runtimePoolHandlerDeploymentService) ListPods(ctx context.Context, namespace, runtimeType string) ([]k8s.RuntimeDeploymentPod, error) {
	var pods []k8s.RuntimeDeploymentPod
	for _, pod := range s.pods {
		if namespace != "" && pod.Namespace != namespace {
			continue
		}
		if runtimeType != "" && pod.RuntimeType != runtimeType {
			continue
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

type runtimePoolHandlerAgentClient struct{}

func (c *runtimePoolHandlerAgentClient) Health(ctx context.Context, endpoint string) error {
	return nil
}
func (c *runtimePoolHandlerAgentClient) CreateGateway(ctx context.Context, endpoint string, req services.RuntimeAgentCreateGatewayRequest) (*services.RuntimeAgentCreateGatewayResponse, error) {
	return nil, nil
}
func (c *runtimePoolHandlerAgentClient) DeleteGateway(ctx context.Context, endpoint, gatewayID string) error {
	return nil
}
func (c *runtimePoolHandlerAgentClient) Drain(ctx context.Context, endpoint string) error { return nil }
func (c *runtimePoolHandlerAgentClient) ResyncInstanceSkills(ctx context.Context, endpoint string, instanceID int, mode string) error {
	return nil
}
