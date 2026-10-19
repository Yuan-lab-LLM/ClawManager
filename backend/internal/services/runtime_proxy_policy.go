package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

type RuntimeProxyPolicy struct {
	Contract    int      `json:"contract"`
	Revision    string   `json:"revision"`
	Namespace   string   `json:"namespace"`
	Proxies     []string `json:"proxies"`
	ExpiresAtMS int64    `json:"expires_at_ms"`
}

type runtimeProxyPolicyClient interface {
	ReadProxyPolicy(context.Context, string) (*RuntimeProxyPolicy, error)
	WriteProxyPolicy(context.Context, string, string, string, []string) (*RuntimeProxyPolicy, error)
}

func (c *runtimeAgentHTTPClient) ReadProxyPolicy(ctx context.Context, endpoint string) (*RuntimeProxyPolicy, error) {
	var result RuntimeProxyPolicy
	err := c.do(ctx, http.MethodGet, endpoint, "/v1/proxy-policy", nil, &result)
	return &result, err
}

func (c *runtimeAgentHTTPClient) WriteProxyPolicy(ctx context.Context, endpoint, revision, namespace string, proxies []string) (*RuntimeProxyPolicy, error) {
	var result RuntimeProxyPolicy
	err := c.do(ctx, http.MethodPut, endpoint, "/v1/proxy-policy", map[string]any{"expected_revision": revision, "namespace": namespace, "proxies": proxies}, &result)
	return &result, err
}

type RuntimeProxyPolicyCoordinator struct {
	client             kubernetes.Interface
	agent              runtimeProxyPolicyClient
	namespace, service string
	mu                 sync.Mutex
	validUntil         map[string]time.Time
}

func NewRuntimeProxyPolicyCoordinator(client kubernetes.Interface, agent RuntimeAgentClient, namespace, service string) *RuntimeProxyPolicyCoordinator {
	policyAgent, _ := agent.(runtimeProxyPolicyClient)
	return &RuntimeProxyPolicyCoordinator{client: client, agent: policyAgent, namespace: namespace, service: service, validUntil: map[string]time.Time{}}
}

// Discover from the tenant Service, never from Runtime CIDRs or caller headers.
// A Running candidate Pod may be included before readiness so rolling updates
// do not depend on discovering themselves through a Ready-only endpoint list.
func (c *RuntimeProxyPolicyCoordinator) discover(ctx context.Context) ([]string, error) {
	service, err := c.client.CoreV1().Services(c.namespace).Get(ctx, c.service, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	if len(service.Spec.Selector) == 0 {
		return nil, fmt.Errorf("proxy Service has no Pod selector")
	}
	pods, err := c.client.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{LabelSelector: labels.SelectorFromSet(service.Spec.Selector).String()})
	if err != nil {
		return nil, err
	}
	unique := map[string]bool{}
	for _, pod := range pods.Items {
		if pod.Namespace != c.namespace || pod.UID == "" || pod.Status.Phase != corev1.PodRunning {
			continue
		}
		// Retain a terminating old App while its bounded drain is in progress;
		// never keep a deleted address alive merely because it was once trusted.
		if pod.DeletionTimestamp != nil {
			// Kubernetes supplies the deletion deadline, not the start of grace.
			if time.Now().After(pod.DeletionTimestamp.Time) {
				continue
			}
		}
		for _, ip := range pod.Status.PodIPs {
			if ip.IP != "" {
				unique[ip.IP] = true
			}
		}
		if pod.Status.PodIP != "" {
			unique[pod.Status.PodIP] = true
		}
	}
	result := make([]string, 0, len(unique))
	for ip := range unique {
		result = append(result, ip)
	}
	sort.Strings(result)
	if len(result) == 0 {
		return nil, fmt.Errorf("no verified proxy Pod addresses")
	}
	return result, nil
}

// Ensure precedes both HTTP and WebSocket forwarding. The lease bounds stale
// trust; every renewal reads fresh Kubernetes state, and CAS rejects concurrent
// stale App updates. Old runtimes keep their existing contract (no unknown PUT).
func (c *RuntimeProxyPolicyCoordinator) Ensure(ctx context.Context, pod *models.RuntimePod) error {
	if c == nil {
		return nil
	}
	if pod == nil || pod.Namespace != c.namespace || pod.AgentEndpoint == nil || strings.TrimSpace(*pod.AgentEndpoint) == "" {
		return fmt.Errorf("proxy policy Runtime identity mismatch")
	}
	if c.client == nil || c.agent == nil {
		return fmt.Errorf("proxy policy coordinator unavailable")
	}
	key := fmt.Sprintf("%d/%s/%s", pod.ID, stringValue(pod.PodUID), *pod.AgentEndpoint)
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Now().Before(c.validUntil[key]) {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		current, err := c.agent.ReadProxyPolicy(ctx, *pod.AgentEndpoint)
		if errors.Is(err, ErrRuntimeAgentNotFound) || errors.Is(err, ErrRuntimeAgentUnsupported) {
			c.validUntil[key] = time.Now().Add(15 * time.Second)
			return nil
		}
		if err != nil {
			return fmt.Errorf("read managed proxy policy: %w", err)
		}
		if current.Contract != 1 || current.Namespace != c.namespace {
			return fmt.Errorf("unsupported managed proxy policy identity")
		}
		addresses, err := c.discover(ctx)
		if err != nil {
			return fmt.Errorf("discover proxy endpoints: %w", err)
		}
		policy, err := c.agent.WriteProxyPolicy(ctx, *pod.AgentEndpoint, current.Revision, c.namespace, addresses)
		if errors.Is(err, ErrRuntimeAgentConflict) {
			continue
		}
		if err != nil {
			return fmt.Errorf("apply managed proxy policy: %w", err)
		}
		if policy.Contract != 1 || policy.Namespace != c.namespace || policy.Revision == "" || strings.Join(policy.Proxies, ",") != strings.Join(addresses, ",") || policy.ExpiresAtMS <= time.Now().Add(10*time.Second).UnixMilli() {
			return fmt.Errorf("managed proxy policy acknowledgement mismatch")
		}
		c.validUntil[key] = time.Now().Add(10 * time.Second)
		return nil
	}
	return fmt.Errorf("proxy policy changed concurrently; retry request")
}

func WithInstanceProxyPolicyCoordinator(agent RuntimeAgentClient, namespace string) InstanceProxyServiceOption {
	return func(s *InstanceProxyService) {
		if client := k8s.GetClient(); client != nil {
			s.proxyPolicy = NewRuntimeProxyPolicyCoordinator(client.Clientset, agent, namespace, "clawmanager-frontend")
		}
	}
}

func WithRuntimeSchedulerProxyPolicyCoordinator(coordinator *RuntimeProxyPolicyCoordinator) RuntimeSchedulerOption {
	return func(s *RuntimeScheduler) { s.proxyPolicy = coordinator }
}

func (s *RuntimeScheduler) runProxyPolicyRenewal(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if s.leader == nil || s.leader.IsLeader(ctx) {
			if err := s.renewRuntimeProxyPolicies(ctx); err != nil && ctx.Err() == nil {
				log.Printf("runtime proxy policy renewal: %v", err)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (s *RuntimeScheduler) renewRuntimeProxyPolicies(ctx context.Context) error {
	if s.proxyPolicy == nil || s.podRepo == nil {
		return nil
	}
	pods, err := s.podRepo.List(ctx, "")
	if err != nil {
		return err
	}
	var failures []error
	for _, pod := range pods {
		if pod.Namespace != s.runtimeNamespace || (pod.RuntimeType != RuntimeTypeOpenClaw && pod.RuntimeType != RuntimeTypeHermes) || pod.AgentEndpoint == nil ||
			(pod.State != "ready" && !(pod.Draining && pod.UsedSlots > 0)) {
			continue
		}
		if err := s.proxyPolicy.Ensure(ctx, &pod); err != nil {
			failures = append(failures, fmt.Errorf("pod %d: %w", pod.ID, err))
		}
	}
	// Failure never removes a binding or restarts a running Gateway. Expiry
	// continues to fail closed; the next successful renewal repairs access.
	return errors.Join(failures...)
}
