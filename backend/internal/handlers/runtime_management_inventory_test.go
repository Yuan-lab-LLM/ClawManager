package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

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
