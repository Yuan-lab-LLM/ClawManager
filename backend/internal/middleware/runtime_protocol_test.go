package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"clawreef/internal/models"
	"github.com/gin-gonic/gin"
)

type protocolPolicyStore struct {
	state *models.RuntimeProtocolState
	err   error
	reads int
	id    int
}

func (s *protocolPolicyStore) Get(_ context.Context, id int) (*models.RuntimeProtocolState, error) {
	s.reads++
	s.id = id
	return s.state, s.err
}
func (*protocolPolicyStore) Negotiate(context.Context, int, int, models.RuntimeCapabilityReport) error {
	panic("generation request must not enroll or downgrade an instance")
}

func TestRuntimeProtocolPolicyCompatibilityBoundary(t *testing.T) {
	for _, tc := range []struct {
		name       string
		instanceID int
		state      *models.RuntimeProtocolState
		err        error
		wantStatus int
		wantPolicy int
		wantReads  int
	}{
		{"legacy-instance", 8, nil, nil, http.StatusNoContent, 0, 1},
		{"enrolled-instance", 8, &models.RuntimeProtocolState{Protocol: 1}, nil, http.StatusNoContent, 1, 1},
		// The current runtime's generation/report freshness never implicitly
		// clears enrollment: even this old generation must enforce protocol 1.
		{"old-enrollment-remains-enforced", 8, &models.RuntimeProtocolState{Protocol: 1, Generation: 1}, nil, http.StatusNoContent, 1, 1},
		{"database-failure-is-not-legacy", 8, nil, errors.New("offline"), http.StatusServiceUnavailable, 0, 1},
		{"personal-key-no-instance", 0, nil, errors.New("must not be called"), http.StatusNoContent, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &protocolPolicyStore{state: tc.state, err: tc.err}
			router := gin.New()
			reached := false
			router.POST("/generate", func(c *gin.Context) {
				c.Set("instanceID", tc.instanceID)
				c.Set("runtimeGeneration", 9)
				c.Next()
			}, RuntimeProtocolPolicy(store), func(c *gin.Context) {
				reached = true
				if policy := c.GetInt("runtimeGovernanceProtocol"); policy != tc.wantPolicy {
					t.Fatalf("policy = %d, want %d", policy, tc.wantPolicy)
				}
				c.Status(http.StatusNoContent)
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/generate", nil))
			if w.Code != tc.wantStatus || reached != (tc.wantStatus == http.StatusNoContent) || store.reads != tc.wantReads {
				t.Fatalf("status=%d reached=%v reads=%d body=%s", w.Code, reached, store.reads, w.Body.String())
			}
			if store.reads > 0 && store.id != tc.instanceID {
				t.Fatal("policy queried for wrong instance")
			}
		})
	}
}
