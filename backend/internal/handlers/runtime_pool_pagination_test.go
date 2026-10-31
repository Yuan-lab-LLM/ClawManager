package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services"
	"clawreef/internal/services/k8s"
	"github.com/gin-gonic/gin"
)

type paginationBindings struct {
	*runtimePoolHandlerBindingRepo
	batchCalls, singleCalls int
}

func (r *paginationBindings) ListByRuntimePodID(ctx context.Context, id int64) ([]models.InstanceRuntimeBinding, error) {
	r.singleCalls++
	return r.runtimePoolHandlerBindingRepo.ListByRuntimePodID(ctx, id)
}

func (r *paginationBindings) ListByRuntimePodIDs(_ context.Context, ids []int64) ([]models.InstanceRuntimeBinding, error) {
	r.batchCalls++
	var out []models.InstanceRuntimeBinding
	for _, binding := range r.bindings {
		for _, id := range ids {
			if binding.RuntimePodID == id {
				out = append(out, binding)
				break
			}
		}
	}
	return out, nil
}

func runtimePagingFixture() (*gin.Engine, *paginationBindings) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC()
	var pods []models.RuntimePod
	for id := 6; id >= 1; id-- {
		node, ip := fmt.Sprintf("node-%d", id%2), fmt.Sprintf("10.0.0.%d", id)
		kind := "openclaw"
		if id > 4 {
			kind = "hermes"
		}
		pods = append(pods, models.RuntimePod{ID: int64(id), RuntimeType: kind, Namespace: "runtime", PodName: fmt.Sprintf("pod-%02d", id), NodeName: &node, PodIP: &ip, State: "ready", Capacity: 2, LastSeenAt: &now, SchedulingPaused: id == 2, Slots: []models.RuntimeSlot{{Index: 1, Ready: true}, {Index: 2, Ready: true}}})
	}
	pods[0].UsedSlots = 1 // Reported occupancy without a binding is not available.
	owner := "OffPageOwner"
	bindings := &paginationBindings{runtimePoolHandlerBindingRepo: &runtimePoolHandlerBindingRepo{bindings: []models.InstanceRuntimeBinding{{RuntimePodID: 5, InstanceID: 505, GatewayPort: 20000, State: "running", Generation: 1}}}}
	repo := &runtimePoolHandlerPodRepo{pods: pods}
	instances := &runtimeAgentHandlerInstanceRepo{instancesByID: map[int]*models.Instance{505: {ID: 505, Name: "OffPageAgent", Owner: &owner, Status: "running"}}}
	scheduler := services.NewRuntimeScheduler(instances, repo, bindings, nil, nil, nil, nil, nil, time.Second)
	handler := NewRuntimePoolHandler(repo, bindings, nil, scheduler, nil)
	return runtimePoolHandlerRouter(1, "admin", handler), bindings
}

type runtimePageResponse struct {
	Data struct {
		Pods     []runtimePoolPodListItem `json:"pods"`
		PageInfo struct {
			Page       int `json:"page"`
			PageSize   int `json:"page_size"`
			Total      int `json:"total"`
			TotalPages int `json:"total_pages"`
		} `json:"pagination"`
		Summary   map[string]int `json:"summary"`
		Nodes     []string       `json:"nodes"`
		UpdatedAt string         `json:"updated_at"`
	} `json:"data"`
}

func runtimePageRequest(t *testing.T, router *gin.Engine, query string) runtimePageResponse {
	t.Helper()
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime-pods"+query, nil))
	if rec.Code != 200 {
		t.Fatalf("response %d: %s", rec.Code, rec.Body.String())
	}
	var response runtimePageResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	return response
}

func TestRuntimePodsServerPaginationAndGlobalSummary(t *testing.T) {
	router, bindings := runtimePagingFixture()
	page := runtimePageRequest(t, router, "?page=1&runtime_type=all")
	if len(page.Data.Pods) != 4 || page.Data.Pods[0].ID != 1 || page.Data.Pods[3].ID != 4 || page.Data.PageInfo.Total != 6 || page.Data.PageInfo.PageSize != 4 || page.Data.PageInfo.TotalPages != 2 {
		t.Fatalf("unexpected first page: %+v", page.Data)
	}
	if page.Data.Summary["pods"] != 6 || page.Data.Summary["online"] != 6 || page.Data.Summary["schedulable"] != 5 || page.Data.Summary["capacity"] != 12 || page.Data.Summary["occupied"] != 2 || page.Data.Summary["available"] != 7 || page.Data.Summary["running"] != 1 {
		t.Fatalf("incorrect global summary: %+v", page.Data.Summary)
	}
	if len(page.Data.Nodes) != 2 || page.Data.Nodes[0] != "node-0" || page.Data.Nodes[1] != "node-1" {
		t.Fatalf("global nodes=%v", page.Data.Nodes)
	}
	if _, err := time.Parse(time.RFC3339, page.Data.UpdatedAt); err != nil {
		t.Fatalf("invalid snapshot timestamp: %q", page.Data.UpdatedAt)
	}
	if bindings.batchCalls != 1 || bindings.singleCalls != 0 {
		t.Fatalf("bindings were not batched: batch=%d single=%d", bindings.batchCalls, bindings.singleCalls)
	}
	page = runtimePageRequest(t, router, "?page=2&page_size=4")
	if len(page.Data.Pods) != 2 || page.Data.Pods[0].ID != 5 || page.Data.Pods[1].ID != 6 {
		t.Fatalf("unexpected second page: %+v", page.Data)
	}
	legacy := runtimePageRequest(t, router, "?runtime_type=openclaw")
	if len(legacy.Data.Pods) != 4 || legacy.Data.Summary != nil {
		t.Fatalf("legacy list contract changed: %+v", legacy.Data)
	}
}

func TestRuntimePodsSearchFiltersBeforePagination(t *testing.T) {
	for _, search := range []string{"OFFPAGEOWNER", "OffPageAgent", "505", "pod-05", "10.0.0.5"} {
		t.Run(search, func(t *testing.T) {
			router, _ := runtimePagingFixture()
			page := runtimePageRequest(t, router, "?page=1&page_size=4&search="+url.QueryEscape(search))
			if len(page.Data.Pods) != 1 || page.Data.Pods[0].ID != 5 || page.Data.PageInfo.Total != 1 || page.Data.Summary["pods"] != 6 {
				t.Fatalf("off-page search failed: %+v", page.Data)
			}
		})
	}
	router, _ := runtimePagingFixture()
	page := runtimePageRequest(t, router, "?page=1&runtime_type=openclaw&node=node-0&scheduling=disabled")
	if len(page.Data.Pods) != 1 || page.Data.Pods[0].ID != 2 || page.Data.PageInfo.Total != 1 || page.Data.Summary["pods"] != 6 || len(page.Data.Nodes) != 2 {
		t.Fatalf("filters or global totals failed: %+v", page.Data)
	}
	page = runtimePageRequest(t, router, "?page=99&search=no-match")
	if len(page.Data.Pods) != 0 || page.Data.PageInfo.Total != 0 || page.Data.PageInfo.TotalPages != 0 || page.Data.PageInfo.Page != 99 {
		t.Fatalf("empty page contract failed: %+v", page.Data)
	}
}

func TestRuntimePodsRejectInvalidPagingQueries(t *testing.T) {
	for _, query := range []string{"?page=", "?page=0", "?page=-1", "?page=no", "?page=1&page_size=0", "?page=1&page_size=101", "?page=1&page_size=no", "?page=1&scheduling=maybe"} {
		router, _ := runtimePagingFixture()
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime-pods"+query, nil))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("query %s: got %d, want 400", query, rec.Code)
		}
	}
}

func TestRuntimePodsSummaryKeepsUnknownAndOccupiedSlotsConservative(t *testing.T) {
	instance := 42
	slots := []models.RuntimeSlot{
		{Ready: true, InstanceID: &instance, InstanceState: "running"},
		{Ready: true, InstanceID: &instance, InstanceState: "creating"},
		{Ready: true, InstanceID: &instance, InstanceState: "stopping"},
		{Ready: true, InstanceID: &instance, InstanceState: "failed"},
		{Ready: true},
		{Ready: false, InstanceID: &instance, InstanceState: "running"},
		{State: "unknown"},
	}
	items := []runtimePoolPodListItem{{RuntimePod: models.RuntimePod{ID: 1, State: "ready", Capacity: 7, UsedSlots: 6, Slots: slots}, AgentReported: true, BindingsReported: true}}
	response := runtimePodPage(items, runtimePodPageQuery{page: 1, size: 4}, "", time.Now())
	summary := response["summary"].(map[string]int)
	for key, want := range map[string]int{"running": 1, "starting": 1, "migrating": 1, "error": 1, "idle": 1, "unavailable": 2, "occupied": 6, "available": 0, "ready": 5} {
		if summary[key] != want {
			t.Errorf("summary[%s]=%d, want %d", key, summary[key], want)
		}
	}
	items[0].UsedSlots = 5
	items[0].BindingsReported = false
	summary = runtimePodPage(items, runtimePodPageQuery{page: 1, size: 4}, "", time.Now())["summary"].(map[string]int)
	if summary["available"] != 0 {
		t.Fatal("unknown binding inventory advertised available capacity")
	}
}

func TestRuntimePodsReplacementUIDDoesNotInheritOldAgentOrBindings(t *testing.T) {
	for _, tc := range []struct {
		query string
		stale bool
	}{{query: ""}, {query: "?page=1"}, {query: "?page=1", stale: true}} {
		t.Run(fmt.Sprintf("query=%s/stale=%t", tc.query, tc.stale), func(t *testing.T) {
			query := tc.query
			gin.SetMode(gin.TestMode)
			now := time.Now().UTC()
			if tc.stale {
				now = now.Add(-time.Hour)
			}
			oldUID, newUID, capabilities, version := "old-uid", "new-uid", `["old-agent-capability"]`, "old-version"
			repo := &runtimePoolHandlerPodRepo{pods: []models.RuntimePod{{ID: 9, RuntimeType: "openclaw", Namespace: "runtime", DeploymentName: "runtime", PodName: "same-name", PodUID: &oldUID, State: "ready", Capacity: 2, UsedSlots: 1, LastSeenAt: &now, CapabilitiesJSON: &capabilities, OpenClawVersion: &version}}}
			bindings := &paginationBindings{runtimePoolHandlerBindingRepo: &runtimePoolHandlerBindingRepo{bindings: []models.InstanceRuntimeBinding{{RuntimePodID: 9, InstanceID: 42, GatewayPort: 20000, State: "running", Generation: 1}}}}
			port1, port2 := 20000, 20003
			deployments := &runtimePoolHandlerDeploymentService{pods: []k8s.RuntimeDeploymentPod{{RuntimeType: "openclaw", Namespace: "runtime", DeploymentName: "runtime", PodName: "same-name", PodUID: newUID, State: "ready", Capacity: 2, Slots: []models.RuntimeSlot{{Index: 1, Ready: true, GatewayPort: &port1}, {Index: 2, Ready: true, GatewayPort: &port2}}}}}
			scheduler := services.NewRuntimeScheduler(nil, repo, bindings, nil, nil, nil, nil, deployments, time.Second, services.WithRuntimeSchedulerNamespace("runtime"))
			handler := NewRuntimePoolHandler(repo, bindings, nil, scheduler, nil)
			page := runtimePageRequest(t, runtimePoolHandlerRouter(1, "admin", handler), query)
			if len(page.Data.Pods) != 1 {
				t.Fatalf("replacement missing: %+v", page.Data)
			}
			pod := page.Data.Pods[0]
			if pod.ID >= 0 || pod.AgentReported || pod.BindingsReported || pod.UsedSlots != 0 || pod.PodUID == nil || *pod.PodUID != newUID || pod.OpenClawVersion != nil || len(pod.Capabilities) != 0 || pod.LastSeenAt != nil {
				t.Fatalf("replacement inherited old Agent identity: %+v", pod)
			}
			for _, slot := range pod.Slots {
				if slot.InstanceID != nil || slot.InstanceState != "" {
					t.Fatalf("replacement inherited old instance binding: %+v", slot)
				}
			}
			if query != "" && (page.Data.Summary["available"] != 0 || page.Data.Summary["schedulable"] != 0 || page.Data.Summary["occupied"] != 0) {
				t.Fatalf("replacement advertised old Agent capacity: %+v", page.Data.Summary)
			}
		})
	}
}

func TestRuntimePodsConfirmedEmptyInventoryDropsGhostReports(t *testing.T) {
	items := runtimePoolPodListItems([]models.RuntimePod{{ID: 9, Namespace: "runtime", PodName: "deleted-pod", State: "ready", Capacity: 1}}, true)
	if merged := mergeRuntimePoolDeploymentPods(items, []models.RuntimePod{}); len(merged) != 0 {
		t.Fatalf("confirmed empty Kubernetes inventory retained %d ghost reports", len(merged))
	}
	if merged := mergeRuntimePoolDeploymentPods(items, nil); len(merged) != 1 {
		t.Fatal("unconfigured inventory provider lost registry-only compatibility")
	}
}

func TestRuntimePodsLegacyUnknownUIDDoesNotBecomeConfirmed(t *testing.T) {
	uid := "live-uid"
	items := runtimePoolPodListItems([]models.RuntimePod{{ID: 9, Namespace: "runtime", PodName: "legacy-pod", State: "ready"}}, true)
	live := []models.RuntimePod{{ID: -1, Namespace: "runtime", PodName: "legacy-pod", PodUID: &uid, KubernetesPodUID: &uid}}
	merged := mergeRuntimePoolDeploymentPods(items, live)
	if len(merged) != 1 || !merged[0].AgentReported || merged[0].ID != 9 || merged[0].PodUID != nil || merged[0].RegisteredPodUID != nil {
		t.Fatalf("legacy display changed or unknown UID became confirmed: %+v", merged)
	}
}
