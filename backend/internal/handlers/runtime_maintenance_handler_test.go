package handlers

import (
	"clawreef/internal/models"
	"context"
	"github.com/gin-gonic/gin"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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
func (s *maintenanceTestStore) Begin(_ context.Context, id, owner, generation, actor int, timeout time.Duration) (*models.RuntimeMaintenance, error) {
	s.calls++
	s.id = id
	s.owner = owner
	return s.state, s.err
}
func (s *maintenanceTestStore) Cancel(context.Context, int, string) error { s.calls++; return s.err }
func (s *maintenanceTestStore) Poll(_ context.Context, id, owner int, r models.RuntimeMaintenanceReport) (*models.RuntimeMaintenance, error) {
	s.calls++
	s.id = id
	s.owner = owner
	s.report = r
	return s.state, s.err
}

func TestRuntimeMaintenanceBeginAdminOnlyAndOldRuntimeNotUpgraded(t *testing.T) {
	for _, tc := range []struct {
		role, body    string
		err           error
		status, calls int
	}{
		{"user", `{"timeout_seconds":60}`, nil, 403, 0},
		{"admin", `{"timeout_seconds":0}`, nil, 400, 0},
		{"admin", `{"timeout_seconds":601}`, nil, 400, 0},
		{"admin", `{"timeout_seconds":60}`, models.ErrRuntimeMaintenanceConflict, 409, 1},
		{"admin", `{"timeout_seconds":60}`, nil, 202, 1},
	} {
		store := &maintenanceTestStore{err: tc.err}
		h := NewRuntimeMaintenanceHandler(store, protocolInstanceStore{instance: &models.Instance{ID: 8, UserID: 7, Type: "opencode", Status: "running", RuntimeGeneration: 3}})
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
		c.Request.Header.Set("Content-Type", "application/json")
		c.Params = gin.Params{{Key: "id", Value: "8"}}
		c.Set("userID", 1)
		c.Set("userRole", tc.role)
		h.Begin(c)
		if w.Code != tc.status || store.calls != tc.calls {
			t.Fatalf("%s %d %s calls=%d", tc.role, w.Code, w.Body.String(), store.calls)
		}
	}
}
func TestRuntimeMaintenancePollTrustsBoundIdentityAndRejectsStaleGeneration(t *testing.T) {
	for _, gen := range []int{2, 3} {
		store := &maintenanceTestStore{}
		h := NewRuntimeMaintenanceHandler(store, nil)
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
func TestRuntimeMaintenanceStaleGenerationNeverReportsDrained(t *testing.T) {
	now := time.Now().UTC()
	state := &models.RuntimeMaintenance{OwnerID: 7, Generation: 2, GateClosed: true, ReportedAt: &now, Deadline: now.Add(time.Minute)}
	result := maintenanceResult(state, &models.Instance{UserID: 7, RuntimeGeneration: 3, Status: "running"})
	if result["drained"] != false || result["draining"] != false {
		t.Fatal(result)
	}
}

func TestRuntimeMaintenanceMissingCountersCannotCertifyIdle(t *testing.T) {
	store := &maintenanceTestStore{}
	h := NewRuntimeMaintenanceHandler(store, nil)
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
