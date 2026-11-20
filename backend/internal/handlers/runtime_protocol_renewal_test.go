package handlers

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

type renewalTestStore struct {
	protocolTestStore
	renewals int
}

func (s *renewalTestStore) Renew(_ context.Context, instanceID, ownerID int, _ models.RuntimeCapabilityReport) error {
	s.renewals++
	s.instanceID, s.ownerID = instanceID, ownerID
	return s.err
}

const renewalReport = `{"protocol":1,"generation":3,"runtime_version":"native.1","adapter_version":"adapter.1","capabilities":["task_identity","turn_cancel","cancel_receipt","live_catalog"],"instance_id":999,"user_id":999}`

func renewalContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/runtime/capabilities/renew", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("instanceID", 8)
	c.Set("userID", 7)
	c.Set("runtimeGeneration", 3)
	return c, w
}

func TestRuntimeCapabilityRenewalDoesNotEnroll(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		status int
	}{
		{"success", nil, 200},
		{"changed contract", models.ErrRuntimeCapabilityRenewalConflict, 409},
		{"lost authorization", models.ErrModelUnavailable, 403},
		{"database failure", errors.New("private database detail"), 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &renewalTestStore{protocolTestStore: protocolTestStore{err: tc.err}}
			c, w := renewalContext(renewalReport)
			NewRuntimeProtocolHandler(store, nil).Renew(c)
			if w.Code != tc.status || store.renewals != 1 || store.calls != 0 || store.instanceID != 8 || store.ownerID != 7 {
				t.Fatalf("status=%d store=%+v body=%s", w.Code, store, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "private database detail") {
				t.Fatal("database detail leaked")
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("renewal response can be cached")
			}
		})
	}
	store := &protocolTestStore{}
	c, w := renewalContext(renewalReport)
	NewRuntimeProtocolHandler(store, nil).Renew(c)
	if w.Code != 503 || store.calls != 0 {
		t.Fatal("legacy store silently negotiated instead of renewing")
	}
}

func TestCapabilityWritesRejectTrailingAndOversizedBodies(t *testing.T) {
	for _, renew := range []bool{false, true} {
		for _, body := range []string{renewalReport + "{}", renewalReport + " garbage", renewalReport + strings.Repeat(" ", 4096), "null", strings.Replace(renewalReport, `"generation":3`, `"generation":2147483648`, 1)} {
			store := &renewalTestStore{}
			c, w := renewalContext(body)
			h := NewRuntimeProtocolHandler(store, nil)
			if renew {
				h.Renew(c)
			} else {
				h.Negotiate(c)
			}
			if w.Code != 400 || store.calls != 0 || store.renewals != 0 {
				t.Fatalf("invalid body reached repository: status=%d", w.Code)
			}
		}
	}
}

func TestRuntimeCapabilityRenewalRequiresTrustedCurrentIdentity(t *testing.T) {
	for _, name := range []string{"unbound", "stale", "missing_generation"} {
		store := &renewalTestStore{}
		c, w := renewalContext(renewalReport)
		switch name {
		case "unbound":
			c.Set("instanceID", 0)
		case "stale":
			c.Set("runtimeGeneration", 4)
		case "missing_generation":
			delete(c.Keys, "runtimeGeneration")
		}
		NewRuntimeProtocolHandler(store, nil).Renew(c)
		if w.Code != 403 || store.renewals != 0 || store.calls != 0 {
			t.Fatalf("%s accepted: %s", name, w.Body.String())
		}
	}
}
