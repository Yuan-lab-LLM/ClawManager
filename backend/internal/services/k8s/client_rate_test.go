package k8s

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"clawreef/internal/config"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

func TestKubernetesClientAdmitsShortAuthorityRequestBurst(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"apiVersion":"v1","kind":"PodList","metadata":{"resourceVersion":"1"},"items":[]}`))
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "kubeconfig")
	data := fmt.Sprintf("apiVersion: v1\nkind: Config\nclusters:\n- name: test\n  cluster:\n    server: %s\ncontexts:\n- name: test\n  context:\n    cluster: test\n    user: test\ncurrent-context: test\nusers:\n- name: test\n  user: {}\n", server.URL)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	previous := globalClient
	t.Cleanup(func() { globalClient = previous })
	cfg := &config.Config{Kubernetes: config.KubernetesConfig{Mode: "outofcluster", OutOfCluster: config.OutOfClusterConfig{Kubeconfig: path}}, Runtime: config.RuntimePoolConfig{NetworkIsolation: true}}
	if err := Initialize(cfg); err != nil {
		t.Fatal(err)
	}
	// Security checks keep their existing short deadline. A healthy API must
	// not be refused solely by client-go's default five-QPS/ten-token queue.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var wg sync.WaitGroup
	failures := make(chan error, 24)
	for i := 0; i < 24; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := GetClient().Clientset.CoreV1().Pods("test").List(ctx, metav1.ListOptions{})
			if err != nil {
				failures <- err
			}
		}()
	}
	wg.Wait()
	close(failures)
	for err := range failures {
		t.Errorf("healthy authority lookup was rate-limited past its deadline: %v", err)
	}
}

func TestLegacyQPSOnlyOverrideBuildsKubernetesClient(t *testing.T) {
	clientCfg := &rest.Config{Host: "http://127.0.0.1:1"}
	cfg := &config.Config{Kubernetes: config.KubernetesConfig{APIQPS: 75}}
	if err := configureAPIRateLimits(clientCfg, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := kubernetes.NewForConfig(clientCfg); err != nil {
		t.Fatalf("QPS-only override cannot initialize client: %v", err)
	}
}

func TestKubernetesClientRateConfigurationKeepsLegacyAndValidatesOverrides(t *testing.T) {
	for _, tc := range []struct {
		name       string
		protected  bool
		qps, burst int
		wantQPS    float32
		wantBurst  int
		invalid    bool
	}{
		{"legacy", false, 0, 0, 7, 11, false},
		{"protected", true, 0, 0, 100, 200, false},
		{"override", true, 60, 90, 60, 90, false},
		{"negative-qps", true, -1, 100, 0, 0, true},
		{"negative-burst", true, 100, -1, 0, 0, true},
		{"unbounded-qps", true, 10001, 100, 0, 0, true},
		{"unbounded-burst", true, 100, 20001, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clientCfg := &rest.Config{QPS: 7, Burst: 11}
			cfg := &config.Config{Kubernetes: config.KubernetesConfig{APIQPS: tc.qps, APIBurst: tc.burst}, Runtime: config.RuntimePoolConfig{NetworkIsolation: tc.protected}}
			err := configureAPIRateLimits(clientCfg, cfg)
			if tc.invalid {
				if err == nil {
					t.Fatal("invalid limit admitted")
				}
				if clientCfg.QPS != 7 || clientCfg.Burst != 11 {
					t.Fatal("invalid settings changed client")
				}
				return
			}
			if err != nil || clientCfg.QPS != tc.wantQPS || clientCfg.Burst != tc.wantBurst {
				t.Fatalf("configuration changed: %v %+v", err, clientCfg)
			}
		})
	}
}
