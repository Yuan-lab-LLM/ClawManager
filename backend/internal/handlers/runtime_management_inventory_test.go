package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func TestRuntimeInventoryShowsReportedOwnerMissingFromDatabase(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Microsecond)
	uid, first, second := "hermes-pod", 20000, 20001
	snapshot := models.RuntimePodObservation{PodUID: uid, ObservedAt: now, Slots: []models.RuntimeSlotObservation{
		{ContainerName: "slot-0", GatewayPort: first, State: "occupied", InstanceID: 25, UserID: 1, GatewayID: "gw-25-14", Generation: 14, InstanceState: "starting", GatewayState: "error", Error: "legacy owner proof unavailable"},
		{ContainerName: "slot-1", GatewayPort: second, State: "free"},
	}}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	raw := string(data)
	pod := models.RuntimePod{ID: 132, RuntimeType: "hermes", PodUID: &uid, State: "ready", Capacity: 2, UsedSlots: 1, LastSeenAt: &now, ObservationAt: &now, ObservationJSON: &raw, Slots: []models.RuntimeSlot{
		{Index: 1, ContainerName: "slot-0", GatewayPort: &first, Ready: true},
		{Index: 2, ContainerName: "slot-1", GatewayPort: &second, Ready: true},
	}}
	handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{pods: []models.RuntimePod{pod}}, &runtimePoolHandlerBindingRepo{}, nil, nil, nil)
	router := runtimePoolHandlerRouter(1, "admin", handler)
	response := runtimePageRequest(t, router, "?page=1")
	if len(response.Data.Pods) != 1 {
		t.Fatal("reported Pod disappeared")
	}
	actual := response.Data.Pods[0]
	slot := actual.Slots[0]
	if slot.InstanceID == nil || *slot.InstanceID != 25 || slot.InstanceState != "error" || slot.ReconciliationState != "residual" || slot.BindingConfirmed {
		t.Fatalf("runtime owner was hidden by absent database binding: %+v", slot)
	}
	if response.Data.Summary["error"] != 1 || response.Data.Summary["occupied"] != 1 || response.Data.Summary["idle"] != 1 {
		t.Fatalf("summary disagrees with slot facts: %+v", response.Data.Summary)
	}
}

func TestRuntimeInventorySlotsKeepContainerHealthSeparateFromBinding(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{pods: []models.RuntimePod{{ID: 9, Capacity: 2, Slots: []models.RuntimeSlot{{Index: 1, ContainerName: "slot-0", Ready: true, State: "running"}, {Index: 2, ContainerName: "slot-1", State: "waiting", Reason: "CrashLoopBackOff"}}}}}, &runtimePoolHandlerBindingRepo{bindings: []models.InstanceRuntimeBinding{{RuntimePodID: 9, InstanceID: 42, GatewayPort: 20003, GatewayID: "gateway", State: "running", Generation: 3}}}, nil, nil, nil)
	router := runtimePoolHandlerRouter(1, "admin", handler)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/runtime-pods", nil))
	var response struct {
		Data struct {
			Pods []models.RuntimePod `json:"pods"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if rec.Code != 200 || len(response.Data.Pods) != 1 {
		t.Fatalf("response = %s", rec.Body.String())
	}
	slot := response.Data.Pods[0].Slots[1]
	if slot.Ready || slot.State != "waiting" || slot.InstanceID == nil || *slot.InstanceID != 42 || slot.InstanceState != "running" || slot.Generation == nil || *slot.Generation != 3 {
		t.Fatalf("slot lost independent health and instance state: %+v", slot)
	}
}

func TestRuntimeInventoryEventsUnavailableIsExplicit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewRuntimePoolHandler(&runtimePoolHandlerPodRepo{pods: []models.RuntimePod{{ID: 9, Namespace: "system", PodName: "runtime-a"}}}, nil, nil, nil, nil)
	router := gin.New()
	router.GET("/pods/:id/events", handler.GetPodEvents)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/pods/9/events", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("events without Kubernetes = %d %s", rec.Code, rec.Body.String())
	}
}

func TestRuntimeInventoryOmitsUnknownMetrics(t *testing.T) {
	items := runtimePoolPodListItems([]models.RuntimePod{{ID: -1, Capacity: 2}}, false)
	data, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if _, known := fields["cpu_millis_used"]; known {
		t.Fatalf("unknown CPU metric was reported as a value: %s", data)
	}
	if _, known := fields["memory_bytes_used"]; known {
		t.Fatalf("unknown memory metric was reported as a value: %s", data)
	}
}

func TestRuntimeInventoryLegacySlotsRemainUnknownAndUseRuntimePortBlock(t *testing.T) {
	handler := NewRuntimePoolHandler(nil, &runtimePoolHandlerBindingRepo{bindings: []models.InstanceRuntimeBinding{{RuntimePodID: 9, InstanceID: 41, GatewayPort: 20000, State: "running"}, {RuntimePodID: 9, InstanceID: 42, GatewayPort: 20001, State: "running"}}}, nil, nil, nil)
	items := runtimePoolPodListItems([]models.RuntimePod{{ID: 9, RuntimeType: "hermes", Capacity: 2, SchedulingPaused: true}}, true)
	handler.enrichRuntimeInventory(context.Background(), items)
	if len(items[0].Slots) != 2 || items[0].Slots[1].Ready || items[0].Slots[1].State != "unknown" || items[0].Slots[1].InstanceID == nil {
		t.Fatalf("legacy slots = %+v", items[0].Slots)
	}
	if items[0].SchedulingEnabled == nil || *items[0].SchedulingEnabled {
		t.Fatalf("paused pod scheduling = %+v", items[0].SchedulingEnabled)
	}
}

func TestRuntimeInventoryDoesNotAttachBindingToDifferentConfiguredPort(t *testing.T) {
	port := 21000
	handler := NewRuntimePoolHandler(nil, &runtimePoolHandlerBindingRepo{bindings: []models.InstanceRuntimeBinding{{RuntimePodID: 9, InstanceID: 42, GatewayPort: 20000, State: "running"}}}, nil, nil, nil)
	items := runtimePoolPodListItems([]models.RuntimePod{{ID: 9, RuntimeType: "openclaw", Capacity: 1, Slots: []models.RuntimeSlot{{Index: 1, GatewayPort: &port}}}}, true)
	handler.enrichRuntimeInventory(context.Background(), items)
	if items[0].Slots[0].InstanceID != nil {
		t.Fatalf("binding for stale port attached to configured slot: %+v", items[0].Slots[0])
	}
}

func TestRuntimeInventorySeparatesDatabaseBindingsFromResidualCounts(t *testing.T) {
	h := NewRuntimePoolHandler(nil, &runtimePoolHandlerBindingRepo{}, nil, nil, nil)
	items := []runtimePoolPodListItem{{RuntimePod: models.RuntimePod{ID: 132, Capacity: 8, UsedSlots: 2, Slots: []models.RuntimeSlot{{Index: 1, InstanceID: inventoryTestInt(25)}}}, AgentReported: true}}
	h.enrichRuntimeInventory(context.Background(), items)
	data, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]interface{}
	if err = json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["bound_instances"] != float64(0) {
		t.Fatalf("missing verified SQL binding count: %v", fields["bound_instances"])
	}
	if items[0].UsedSlots != 2 {
		t.Fatal("display count changed conservative occupancy authority")
	}
}
func inventoryTestInt(n int) *int { return &n }

func TestRuntimeInventoryHidesProvenRetiredPodWithoutWaitingForHeartbeatExpiry(t *testing.T) {
	now := time.Now().UTC()
	for _, window := range []time.Duration{0, time.Minute} {
		pods := []models.RuntimePod{{ID: 1, State: "deleted", LastSeenAt: &now}, {ID: 2, State: "ready", LastSeenAt: &now}}
		got := filterCurrentRuntimePods(pods, now, window)
		if len(got) != 1 || got[0].ID != 2 {
			t.Fatalf("retired source reappeared with %s heartbeat window: %+v", window, got)
		}
	}
}

func TestRuntimeBindingSummaryDoesNotInventZeroForUnknownDatabaseInventory(t *testing.T) {
	page := runtimePodPage([]runtimePoolPodListItem{{RuntimePod: models.RuntimePod{ID: 1, Capacity: 8}, BoundInstances: nil}}, runtimePodPageQuery{page: 1, size: 16}, "", time.Now())
	summary := page["summary"].(map[string]int)
	if _, known := summary["bound_instances"]; known {
		t.Fatal("unknown database inventory was presented as zero bindings")
	}
}
