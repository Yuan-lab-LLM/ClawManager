package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func TestRuntimeAgentSlotRegistrationCannotExceedPhysicalCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, example := range []struct {
		physical, configured, want int
		capability                 string
	}{{1, 8, 1, "container-slots-v1"}, {8, 8, 8, "container-slots-v1"}, {16, 8, 0, "container-slots-v1"}, {1, 0, 1, "container-slots-v1"}, {16, 0, 16, "container-slots-v1"}, {16, 8, 0, " container-slots-v1 "}} {
		repo := &runtimeAgentHandlerPodRepo{}
		handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", ContainerSlots: 8, MaxGatewaysPerPod: example.configured}, repo, &runtimeAgentHandlerBindingRepo{}, nil, nil, nil)
		router := gin.New()
		router.POST("/register", handler.Register)
		body := []byte(`{"runtime_type":"openclaw","namespace":"runtime","pod_name":"pool","deployment_name":"pool","image_ref":"test","state":"ready","capabilities":["` + example.capability + `"],"capacity":` + strconv.Itoa(example.physical) + `}`)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(runtimeAgentTokenHeader, "secret")
		router.ServeHTTP(recorder, request)
		if example.want == 0 {
			if recorder.Code != http.StatusConflict || repo.podsByID[1] != nil || !strings.Contains(recorder.Body.String(), "physical Runtime slot capacity exceeds configured admission limit") {
				t.Fatalf("mismatch accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
			}
			continue
		}
		if recorder.Code != http.StatusOK || repo.podsByID[1] == nil || repo.podsByID[1].Capacity != example.want {
			t.Fatalf("physical=%d configured=%d: status=%d pod=%+v", example.physical, example.configured, recorder.Code, repo.podsByID[1])
		}
	}
}

func TestRuntimeAgentSlotHeartbeatPreservesRegisteredCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capabilities := `["container-slots-v1"]`
	repo := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{1: {ID: 1, Capacity: 1, CapabilitiesJSON: &capabilities}}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", ContainerSlots: 8, MaxGatewaysPerPod: 8}, repo, &runtimeAgentHandlerBindingRepo{}, nil, nil, nil)
	router := gin.New()
	router.POST("/heartbeat", handler.Heartbeat)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewBufferString(`{"pod_id":1,"state":"ready","used_slots":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(runtimeAgentTokenHeader, "secret")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || repo.lastHeartbeatCapacity != 1 {
		t.Fatalf("heartbeat inflated physical slot capacity: status=%d capacity=%d", recorder.Code, repo.lastHeartbeatCapacity)
	}
}

func TestRuntimeAgentSlotHeartbeatRepairsPreviouslyInflatedCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capabilities := `["container-slots-v1"]`
	repo := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{1: {ID: 1, Capacity: 8, CapabilitiesJSON: &capabilities}}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", ContainerSlots: 8, MaxGatewaysPerPod: 8}, repo, &runtimeAgentHandlerBindingRepo{}, nil, nil, nil)
	router := gin.New()
	router.POST("/heartbeat", handler.Heartbeat)
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewBufferString(`{"pod_id":1,"state":"ready","used_slots":1,"max_gateways":1}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set(runtimeAgentTokenHeader, "secret")
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || repo.lastHeartbeatCapacity != 1 {
		t.Fatalf("live physical slot report did not repair inflated capacity: status=%d capacity=%d", recorder.Code, repo.lastHeartbeatCapacity)
	}
}

func TestRuntimeAgentSlotHeartbeatRejectsCapacityAboveAdmissionLimit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, slots := range []int{0, 8} {
		capabilities := `["container-slots-v1"]`
		repo := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{1: {ID: 1, Capacity: 8, CapabilitiesJSON: &capabilities}}}
		handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", ContainerSlots: slots, MaxGatewaysPerPod: 8}, repo, &runtimeAgentHandlerBindingRepo{}, nil, nil, nil)
		router := gin.New()
		router.POST("/heartbeat", handler.Heartbeat)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/heartbeat", bytes.NewBufferString(`{"pod_id":1,"state":"ready","max_gateways":16}`))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(runtimeAgentTokenHeader, "secret")
		router.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusConflict || repo.lastHeartbeatCapacity != 0 || !strings.Contains(recorder.Body.String(), "physical Runtime slot capacity exceeds configured admission limit") {
			t.Fatalf("container_slots=%d: status=%d capacity=%d body=%s", slots, recorder.Code, repo.lastHeartbeatCapacity, recorder.Body.String())
		}
	}
}

func TestRuntimeAgentSlotHeartbeatPreservesPhysicalCapacityWithoutSlotConfig(t *testing.T) {
	gin.SetMode(gin.TestMode)
	capabilities := `["container-slots-v1"]`
	repo := &runtimeAgentHandlerPodRepo{podsByID: map[int64]*models.RuntimePod{1: {ID: 1, Capacity: 1, CapabilitiesJSON: &capabilities}}}
	handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", MaxGatewaysPerPod: 8}, repo, &runtimeAgentHandlerBindingRepo{}, nil, nil, nil)
	router := gin.New()
	router.POST("/heartbeat", handler.Heartbeat)
	serveRuntimeAgentRequest(t, router, "/heartbeat", `{"pod_id":1,"state":"ready"}`)
	if repo.lastHeartbeatCapacity != 1 {
		t.Fatalf("physical capacity changed to admission limit: %d", repo.lastHeartbeatCapacity)
	}
}
