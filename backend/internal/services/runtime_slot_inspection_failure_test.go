package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"clawreef/internal/models"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestSlotInspectionRecheckPreservesInfrastructureFailure(t *testing.T) {
	for _, capacity := range []int{2, 8} {
		t.Run(fmt.Sprint(capacity), func(t *testing.T) {
			kube := fake.NewSimpleClientset(auditRoutingPod(capacity, 1))
			lists := 0
			kube.PrependReactor("list", "pods", func(k8stesting.Action) (bool, runtime.Object, error) {
				lists++
				if lists == 2 {
					return true, nil, context.DeadlineExceeded
				}
				return false, nil, nil
			})
			client := NewSlotRuntimeAgentClientWithHTTPClient("token", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(*http.Request) (*http.Response, error) {
				body, _ := json.Marshal(auditFreeSnapshot())
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(string(body)))}, nil
			})}).(*slotRuntimeAgentClient)
			endpoint, uid := "https://10.0.0.2:19090", "pod-uid"
			observations, err, verified := client.observeRuntimeSlotsForAudit(t.Context(), models.RuntimePod{RuntimeType: "openclaw", Capacity: capacity, AgentEndpoint: &endpoint, PodUID: &uid})
			if !errors.Is(err, context.DeadlineExceeded) || !runtimeUpgradeInfrastructureRetryable(err) {
				t.Fatalf("transient recheck became a permanent identity failure: %v", err)
			}
			if verified || observations != nil {
				t.Fatal("failed identity recheck granted slot authority")
			}
		})
	}
}
