package handlers

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

func TestRuntimeProtocolInspectIsReadOnlyAndOwnerBound(t *testing.T) {
	for _, tc := range []struct {
		name   string
		bound  bool
		state  *models.RuntimeProtocolState
		err    error
		status int
	}{
		{"unbound", false, nil, nil, 403},
		{"legacy", true, nil, nil, 200},
		{"enrolled", true, &models.RuntimeProtocolState{Protocol: 1, Generation: 3}, nil, 200},
		{"old generation", true, &models.RuntimeProtocolState{Protocol: 1, Generation: 2}, nil, 200},
		{"unknown protocol", true, &models.RuntimeProtocolState{Protocol: 2}, nil, 503},
		{"unreadable", true, nil, errors.New("private database detail"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &protocolTestStore{state: tc.state, err: tc.err}
			h := NewRuntimeProtocolHandler(store, nil)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest("GET", "/?instance_id=999&generation=999", nil)
			if tc.bound {
				c.Set("instanceID", 8)
				c.Set("userID", 7)
				c.Set("runtimeGeneration", 3)
			}
			h.Inspect(c)
			if w.Code != tc.status {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if store.instanceID != 0 || store.ownerID != 0 {
				t.Fatal("inspect called enrollment")
			}
			if !tc.bound && store.calls != 0 {
				t.Fatal("unbound request read state")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("cacheable capability response")
			}
			if tc.status == 200 {
				var body struct {
					Protocol, Generation int
					Enrolled             bool
					Required             []string `json:"required_capabilities"`
					Sticky               bool     `json:"registration_is_sticky"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if body.Protocol != 1 || body.Generation != 3 || body.Enrolled != (tc.state != nil) || !body.Sticky || len(body.Required) != 4 {
					t.Fatalf("bad preflight: %s", w.Body.String())
				}
			}
		})
	}
}
