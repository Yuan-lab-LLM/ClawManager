package handlers

import (
	"bytes"
	"clawreef/internal/config"
	"clawreef/internal/models"
	"clawreef/internal/services"
	"context"
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

type shutdownDeliveryStore struct {
	shutdownHistoryStore
	deliveries  []models.RuntimeShutdownDelivery
	deliveryErr error
}

func (s *shutdownDeliveryStore) RecordDelivery(_ context.Context, source string, d models.RuntimeShutdownDelivery) error {
	s.recordSource = source
	s.deliveries = append(s.deliveries, d)
	return s.deliveryErr
}
func shutdownDeliveryFixture() models.RuntimeShutdownDelivery {
	e := models.RuntimeShutdownEvent{InstanceID: 8, OwnerUserID: 7, Generation: 2, ClaimID: strings.Repeat("a", 64), Outcome: "cleanup_confirmed_release_observed"}
	return models.RuntimeShutdownDelivery{ID: e.DeliveryID(), Event: e}
}
func TestShutdownDeliveryHTTPAuthenticationAndOriginalOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, source    string
		auth            bool
		owner, instance int
		want, accepted  int
		storageError    error
	}{
		{"pro accepted", "pro", true, 7, 8, 200, 1, nil}, {"pro owner changed", "pro", true, 9, 8, 200, 0, nil}, {"pro wrong instance", "pro", true, 7, 9, 200, 0, nil}, {"pro missing auth", "pro", false, 7, 8, 401, 0, nil},
		{"lite deleted gateway", "lite", true, 7, 8, 200, 1, nil}, {"lite missing auth", "lite", false, 7, 8, 401, 0, nil}, {"storage fails", "lite", true, 7, 8, 503, 1, errors.New("password=secret")}, {"stale owner retained", "lite", true, 7, 8, 200, 1, models.ErrModelUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &shutdownDeliveryStore{deliveryErr: tc.storageError}
			r := gin.New()
			if tc.source == "pro" {
				h := NewAgentHandler(&agentHandlerSessionService{session: &services.AgentSession{Instance: &models.Instance{ID: tc.instance, UserID: tc.owner}, Agent: &models.InstanceAgent{InstanceID: tc.instance}}}, nil, nil, nil, nil, WithAgentShutdownAudits(store))
				r.POST("/deliver", h.ReportShutdownDeliveries)
			} else {
				h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, nil, nil, nil, nil, nil).WithShutdownAudits(store)
				r.POST("/deliver", h.ReportShutdownDeliveries)
			}
			raw, _ := json.Marshal(shutdownDeliveryBatch{Deliveries: []models.RuntimeShutdownDelivery{shutdownDeliveryFixture()}})
			req := httptest.NewRequest("POST", "/deliver", bytes.NewReader(raw))
			if tc.auth {
				req.Header.Set("Authorization", "Bearer session")
				req.Header.Set(runtimeAgentTokenHeader, "secret")
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want || len(store.deliveries) != tc.accepted {
				t.Fatalf("%d %d %s", w.Code, len(store.deliveries), w.Body.String())
			}
			if strings.Contains(w.Body.String(), "password") {
				t.Fatal("storage details leaked")
			}
			if tc.want == 200 {
				var out struct {
					Data struct {
						Version        int      `json:"version"`
						DiagnosticOnly bool     `json:"diagnostic_only"`
						IDs            []string `json:"accepted_ids"`
					} `json:"data"`
				}
				if json.Unmarshal(w.Body.Bytes(), &out) != nil || out.Data.Version != 1 || !out.Data.DiagnosticOnly || out.Data.IDs == nil {
					t.Fatal(w.Body.String())
				}
				want := tc.accepted
				if tc.storageError != nil {
					want = 0
				}
				if len(out.Data.IDs) != want {
					t.Fatal("invalid acknowledgement")
				}
			}
		})
	}
}
func TestShutdownDeliveryInvalidBatchNeverTouchesStorage(t *testing.T) {
	d := shutdownDeliveryFixture()
	valid, _ := json.Marshal(shutdownDeliveryBatch{Deliveries: []models.RuntimeShutdownDelivery{d}})
	duplicate, _ := json.Marshal(shutdownDeliveryBatch{Deliveries: []models.RuntimeShutdownDelivery{d, d}})
	tooMany := make([]models.RuntimeShutdownDelivery, 33)
	for i := range tooMany {
		v := d
		v.Event.Generation = i
		v.ID = v.Event.DeliveryID()
		tooMany[i] = v
	}
	many, _ := json.Marshal(shutdownDeliveryBatch{Deliveries: tooMany})
	for _, raw := range []string{`{}`, `{"deliveries":[]}`, `{"deliveries":null}`, string(duplicate), string(many), string(valid) + `{}`, strings.Replace(string(valid), `"generation":2`, `"generation":3`, 1), strings.Replace(string(valid), `"event":{`, `"event":{"unexpected":true,`, 1), strings.Repeat(" ", 65537) + string(valid)} {
		store := &shutdownDeliveryStore{}
		h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, nil, nil, nil, nil, nil).WithShutdownAudits(store)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(raw))
		c.Request.Header.Set(runtimeAgentTokenHeader, "secret")
		h.ReportShutdownDeliveries(c)
		if w.Code != 400 || len(store.deliveries) != 0 {
			t.Fatalf("accepted invalid batch: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestShutdownDeliveryUnsupportedStorageCannotFakeAck(t *testing.T) {
	h := NewRuntimeAgentHandler(config.RuntimePoolConfig{AgentReportToken: "secret"}, nil, nil, nil, nil, nil).WithShutdownAudits(&shutdownHistoryStore{})
	raw, _ := json.Marshal(shutdownDeliveryBatch{Deliveries: []models.RuntimeShutdownDelivery{shutdownDeliveryFixture()}})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", bytes.NewReader(raw))
	c.Request.Header.Set(runtimeAgentTokenHeader, "secret")
	h.ReportShutdownDeliveries(c)
	if w.Code != 503 || strings.Contains(w.Body.String(), "accepted_ids") {
		t.Fatal(w.Body.String())
	}
}
