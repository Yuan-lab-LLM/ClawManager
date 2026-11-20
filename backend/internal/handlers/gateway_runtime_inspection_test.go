package handlers

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func inspectionContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("instanceID", 8)
	c.Set("userID", 7)
	c.Set("runtimeGeneration", 3)
	return c, w
}

func TestRuntimeTaskInspectionIsReadOnlyAndUsesCurrentOwner(t *testing.T) {
	for _, missing := range []bool{false, true} {
		store := &runtimeReceiptStore{missing: missing, state: models.GatewayTaskState{Revoked: true, Finished: false}}
		h := NewGatewayRuntimeHandler(store, nil, nil)
		c, w := inspectionContext(`{"generation":3,"user_id":999,"instance_id":999,"tasks":[{"generation":2,"session_id":"s","task_id":"t","user_id":999,"instance_id":999}]}`)
		h.InspectTasks(c)
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		if store.checks != 0 || store.finishes != 0 || store.inspections != 1 {
			t.Fatal("inspection mutated task state")
		}
		if store.identity.InstanceID != 8 || store.identity.UserID != 7 || store.identity.Generation != 3 || store.requested[0].InstanceID != 8 || store.requested[0].UserID != 7 || store.requested[0].Generation != 2 {
			t.Fatal("untrusted inspection scope")
		}
		var result struct {
			ReadOnly      bool                         `json:"read_only"`
			ReplayAllowed bool                         `json:"receipt_replay_allowed"`
			Tasks         []models.GatewayTaskSnapshot `json:"tasks"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		if !result.ReadOnly || result.ReplayAllowed || len(result.Tasks) != 1 || (result.Tasks[0].State == nil) != missing {
			t.Fatal("incorrect read-only response")
		}
		if !missing && (result.Tasks[0].State.Finished || result.Tasks[0].State.RuntimeAcknowledged) {
			t.Fatal("inferred completion from revocation")
		}
	}
}

func TestRuntimeTaskInspectionRejectsInvalidBatches(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"stale-client", `{"generation":2,"tasks":[{"generation":2,"session_id":"s","task_id":"t"}]}`, 403},
		{"future-task", `{"generation":3,"tasks":[{"generation":4,"session_id":"s","task_id":"t"}]}`, 400},
		{"empty", `{"generation":3,"tasks":[]}`, 400},
		{"trailing", `{"generation":3,"tasks":[{"generation":2,"session_id":"s","task_id":"t"}]}{}`, 400},
		{"negative", `{"generation":3,"tasks":[{"generation":-1,"session_id":"s","task_id":"t"}]}`, 400},
		{"duplicate", `{"generation":3,"tasks":[{"generation":2,"session_id":"s","task_id":"t"},{"generation":2,"session_id":"s","task_id":"t"}]}`, 400},
		{"oversized", `{"generation":3,"padding":"` + strings.Repeat("x", 33000) + `","tasks":[]}`, 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &runtimeReceiptStore{}
			h := NewGatewayRuntimeHandler(store, nil, nil)
			c, w := inspectionContext(tc.body)
			h.InspectTasks(c)
			if w.Code != tc.status || store.inspections != 0 || store.checks != 0 || store.finishes != 0 {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	store := &runtimeReceiptStore{}
	c, w := inspectionContext(`{"generation":3,"tasks":[{"generation":2,"session_id":"s","task_id":"t"}]}`)
	c.Set("instanceID", 0)
	NewGatewayRuntimeHandler(store, nil, nil).InspectTasks(c)
	if w.Code != 403 || store.inspections != 0 {
		t.Fatal("personal credential accepted")
	}
}

func TestRuntimeTaskInspectionErrorDoesNotBecomeMissing(t *testing.T) {
	for _, err := range []error{errors.New("private-db-error"), models.ErrModelUnavailable} {
		store := &runtimeReceiptStore{inspectErr: err}
		c, w := inspectionContext(`{"generation":3,"tasks":[{"generation":2,"session_id":"s","task_id":"t"}]}`)
		NewGatewayRuntimeHandler(store, nil, nil).InspectTasks(c)
		if w.Code == 200 || strings.Contains(w.Body.String(), "private-db-error") || store.finishes != 0 || store.checks != 0 {
			t.Fatal("inspection error hidden or leaked")
		}
	}
}

func TestRuntimeTaskFinishDoesNotCreateOrAcknowledgeUnknownTask(t *testing.T) {
	for _, name := range []string{"missing", "inspection-failed", "write-fence", "old-generation"} {
		t.Run(name, func(t *testing.T) {
			store := &runtimeReceiptStore{state: models.GatewayTaskState{Revoked: true}}
			switch name {
			case "missing":
				store.missing = true
			case "inspection-failed":
				store.inspectErr = models.ErrModelUnavailable
			case "write-fence":
				store.finishErr = models.ErrModelUnavailable
			}
			audit := &stubEgressAuditService{}
			c, w := inspectionContext(`{"generation":3,"session_id":"s","task_id":"t","cancelled":true}`)
			if name == "old-generation" {
				c.Set("runtimeGeneration", 4)
			}
			NewGatewayRuntimeHandler(store, nil, audit).FinishTask(c)
			if w.Code != 403 || store.checks != 0 || store.state.Finished || store.state.RuntimeAcknowledged || len(audit.events) != 0 {
				t.Fatalf("false receipt acceptance: %d %s", w.Code, w.Body.String())
			}
			if name != "write-fence" && store.finishes != 0 {
				t.Fatal("invalid receipt reached write")
			}
		})
	}
}
