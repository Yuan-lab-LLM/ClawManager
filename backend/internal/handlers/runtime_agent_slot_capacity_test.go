package handlers

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"clawreef/internal/config"
	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func TestRuntimeAgentSlotRegistrationCannotExceedPhysicalCapacity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, example := range []struct{ physical, configured, want int }{{1, 8, 1}, {16, 8, 8}, {1, 0, 1}} {
		repo := &runtimeAgentHandlerPodRepo{}
		handler := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret", ContainerSlots: 8, MaxGatewaysPerPod: example.configured}, repo, &runtimeAgentHandlerBindingRepo{}, nil, nil, nil)
		router := gin.New()
		router.POST("/register", handler.Register)
		body := []byte(`{"runtime_type":"openclaw","namespace":"runtime","pod_name":"pool","deployment_name":"pool","image_ref":"test","state":"ready","capabilities":["container-slots-v1"],"capacity":` + strconv.Itoa(example.physical) + `}`)
		recorder := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/register", bytes.NewReader(body))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set(runtimeAgentTokenHeader, "secret")
		router.ServeHTTP(recorder, request)
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
