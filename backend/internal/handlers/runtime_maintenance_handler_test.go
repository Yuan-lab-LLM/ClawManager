package handlers

import (
	"clawreef/internal/models"
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
)

type maintenanceTestStore struct {
	state     *models.RuntimeMaintenance
	calls     int
	id, owner int
	report    models.RuntimeMaintenanceReport
	err       error
}

func (s *maintenanceTestStore) Get(context.Context, int) (*models.RuntimeMaintenance, error) {
	s.calls++
	return s.state, s.err
}
func (s *maintenanceTestStore) Poll(_ context.Context, id, owner int, r models.RuntimeMaintenanceReport) (*models.RuntimeMaintenance, error) {
	s.calls++
	s.id = id
	s.owner = owner
	s.report = r
	return s.state, s.err
}

func TestRuntimeMaintenancePollTrustsBoundIdentityAndRejectsStaleGeneration(t *testing.T) {
	for _, gen := range []int{2, 3} {
		store := &maintenanceTestStore{}
		h := NewRuntimeMaintenanceHandler(store)
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"generation":3,"instance_id":999,"user_id":999,"gate_closed":false,"active_executions":0,"active_admissions":0,"active_jobs":0,"queued_inputs":0}`))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("instanceID", 8)
		c.Set("userID", 7)
		c.Set("runtimeGeneration", gen)
		h.Poll(c)
		if gen == 2 {
			if w.Code != 403 || store.calls != 0 {
				t.Fatal(w.Body.String())
			}
			continue
		}
		if w.Code != 200 || store.id != 8 || store.owner != 7 {
			t.Fatal(w.Body.String())
		}
	}
}
func TestRuntimeMaintenanceMissingCountersCannotCertifyIdle(t *testing.T) {
	store := &maintenanceTestStore{}
	h := NewRuntimeMaintenanceHandler(store)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"generation":3,"gate_closed":true}`))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("instanceID", 8)
	c.Set("userID", 7)
	c.Set("runtimeGeneration", 3)
	h.Poll(c)
	if w.Code != 400 || store.calls != 0 {
		t.Fatalf("incomplete report accepted: %s", w.Body.String())
	}
}
