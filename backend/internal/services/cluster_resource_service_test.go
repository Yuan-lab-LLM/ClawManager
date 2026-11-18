package services

import (
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestKubeletUsageFreshZeroAndIndependentMissing(t *testing.T) {
	now := time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC)
	detail := NodeResourceDetail{CPU: ResourceSummary{Capacity: 4}, Memory: ResourceSummary{Capacity: 8}, Disk: ResourceSummary{Capacity: 16}}
	raw := []byte(`{"node":{"cpu":{"time":"2026-11-16T01:00:00Z","usageNanoCores":0},"memory":{"time":"2026-11-16T01:00:00Z","workingSetBytes":1073741824},"fs":{"time":"2026-11-16T00:55:00Z","usedBytes":1073741824,"capacityBytes":17179869184}}}`)
	applyKubeletUsage(&detail, raw, now)
	if detail.CPU.Used == nil || *detail.CPU.Used != 0 || detail.CPU.ObservedAt == nil {
		t.Fatalf("known CPU zero lost: %+v", detail.CPU)
	}
	if detail.Memory.Used == nil || *detail.Memory.Used != 1 {
		t.Fatalf("memory conversion: %+v", detail.Memory)
	}
	if detail.Disk.Used != nil {
		t.Fatal("stale disk must remain unavailable")
	}
}

func TestKubeletUsageRejectsCapacityMismatchAndInvalid(t *testing.T) {
	now := time.Date(2026, 11, 16, 1, 0, 0, 0, time.UTC)
	for _, raw := range []string{
		`{"node":{"cpu":{"time":"2026-11-16T01:00:00Z","usageNanoCores":5000000000},"memory":{"time":"2026-11-16T01:00:00Z","workingSetBytes":-1},"fs":{"time":"2026-11-16T01:00:00Z","usedBytes":1073741824,"capacityBytes":34359738368}}}`,
		`{"node":{"cpu":{"time":"2026-11-16T02:00:00Z","usageNanoCores":1}}}`,
		`{invalid`,
	} {
		detail := NodeResourceDetail{CPU: ResourceSummary{Capacity: 4}, Memory: ResourceSummary{Capacity: 8}, Disk: ResourceSummary{Capacity: 16}}
		applyKubeletUsage(&detail, []byte(raw), now)
		if detail.CPU.Used != nil || detail.Memory.Used != nil || detail.Disk.Used != nil {
			t.Fatalf("invalid usage accepted: %+v", detail)
		}
	}
}

func TestAggregateUsageNeverReportsPartialNodes(t *testing.T) {
	zero, one := 0.0, 1.0
	now := time.Now().UTC()
	old := now.Add(-time.Second)
	nodes := []NodeResourceDetail{{CPU: ResourceSummary{Used: &zero, ObservedAt: &now}, Memory: ResourceSummary{Used: &one, ObservedAt: &now}}, {CPU: ResourceSummary{Used: &one, ObservedAt: &old}}}
	var overview ClusterResourceOverview
	overview.Nodes = nodes
	aggregateClusterUsage(&overview)
	if overview.CPU.Used == nil || *overview.CPU.Used != 1 || !overview.CPU.ObservedAt.Equal(old) {
		t.Fatalf("complete CPU aggregate: %+v", overview.CPU)
	}
	if overview.Memory.Used != nil || overview.Memory.ObservedAt != nil {
		t.Fatal("partial memory must be null")
	}
	overview.Nodes = nil
	aggregateClusterUsage(&overview)
	if overview.CPU.Used != nil {
		t.Fatal("no nodes is unknown")
	}
}

func TestClusterOverviewReadsNodeProxyAndPreservesRequests(t *testing.T) {
	seen := false
	failMetrics := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/nodes":
			w.Write([]byte(`{"apiVersion":"v1","kind":"NodeList","items":[{"metadata":{"name":"node-a"},"status":{"capacity":{"cpu":"4","memory":"8Gi","ephemeral-storage":"16Gi"},"allocatable":{"cpu":"4","memory":"8Gi","ephemeral-storage":"16Gi"}}}]}`))
		case "/api/v1/pods":
			w.Write([]byte(`{"apiVersion":"v1","kind":"PodList","items":[{"metadata":{"name":"p"},"spec":{"nodeName":"node-a","containers":[{"name":"c","resources":{"requests":{"cpu":"500m","memory":"1Gi","ephemeral-storage":"2Gi"}}}]}}]}`))
		case "/api/v1/nodes/node-a/proxy/stats/summary":
			seen = true
			if failMetrics {
				http.Error(w, "metrics unavailable", http.StatusForbidden)
				return
			}
			stamp := time.Now().UTC().Format(time.RFC3339Nano)
			raw := `{"node":{"cpu":{"time":"STAMP","usageNanoCores":1000000000},"memory":{"time":"STAMP","workingSetBytes":0},"fs":{"time":"STAMP","usedBytes":1073741824,"capacityBytes":17179869184}}}`
			w.Write([]byte(strings.ReplaceAll(raw, "STAMP", stamp)))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := kubernetes.NewForConfig(&rest.Config{Host: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	service := &clusterResourceService{client: &k8s.Client{Clientset: client}}
	result, err := service.GetOverview(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !seen || result.CPU.Used == nil || *result.CPU.Used != 1 {
		t.Fatalf("node proxy usage not used: %+v", result.CPU)
	}
	if result.CPU.Requested != 0.5 || result.Disk.Requested != 2 {
		t.Fatalf("requests changed: %+v %+v", result.CPU, result.Disk)
	}
	if result.Memory.Used == nil || *result.Memory.Used != 0 {
		t.Fatal("measured zero replaced with request")
	}
	encoded, _ := json.Marshal(result)
	if !strings.Contains(string(encoded), `"observed_at":`) {
		t.Fatal("observation timestamp missing")
	}
	failMetrics = true
	unavailable, err := service.GetOverview(context.Background())
	if err != nil {
		t.Fatalf("metrics failure must retain capacity: %v", err)
	}
	if unavailable.CPU.Used != nil || unavailable.CPU.Capacity != 4 || unavailable.CPU.Requested != 0.5 {
		t.Fatalf("metrics failure substituted requests or lost capacity: %+v", unavailable.CPU)
	}

}
