package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/runtimeauth"
	k8sservice "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// slotRuntimeAgentClient preserves all existing HTTP upgrade/policy operations
// while requiring persisted ownership and teardown evidence for slot lifecycle.
type slotRuntimeAgentClient struct {
	*runtimeAgentHTTPClient
	kube                kubernetes.Interface
	namespace           string
	assignments         *k8sservice.RuntimeAssignmentService
	reconcileMu         sync.Mutex
	reconcileCursor     int
	security            *RuntimeSecurityAuthority
	protectTeamPreview  func(context.Context, runtimeauth.Identity) error
	admissionMu         sync.RWMutex
	assignmentAdmission func(context.Context, string, string, int, int, func() error) error
}

// Legacy callers must never infer slot teardown from a 404 or an absent process
// status. Only DeleteGateway's matching, persisted teardown receipt is authority.
func (*slotRuntimeAgentClient) RequiresConfirmedGatewayStop() bool { return true }

// ReconcileAssignments retries persisted work after an interrupted API call or
// backend restart. It never invents a new generation or overrides a missing
// source-stop receipt. Slot books and the shared ledger reject delayed replays.
func (c *slotRuntimeAgentClient) ReconcileAssignments(ctx context.Context) error {
	c.reconcileMu.Lock()
	defer c.reconcileMu.Unlock()
	assignments, err := c.assignments.ListPending(ctx)
	var failures []error
	if err != nil {
		failures = append(failures, err)
	}
	if len(assignments) == 0 {
		return errors.Join(failures...)
	}
	sort.Slice(assignments, func(i, j int) bool { return assignments[i].InstanceID < assignments[j].InstanceID })
	start := sort.Search(len(assignments), func(i int) bool { return assignments[i].InstanceID > c.reconcileCursor })
	if start == len(assignments) {
		start = 0
	}
	jobs := make(chan k8sservice.RuntimeDesiredAssignment)
	var wg sync.WaitGroup
	var failureMu sync.Mutex
	for i := 0; i < min(8, len(assignments)); i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for desired := range jobs {
				if err := c.reconcileAssignment(ctx, desired); err != nil {
					failureMu.Lock()
					failures = append(failures, fmt.Errorf("instance %d generation %d: %w", desired.InstanceID, desired.Generation, err))
					failureMu.Unlock()
				}
			}
		}()
	}
dispatch:
	for i := 0; i < len(assignments); i++ {
		if ctx.Err() != nil {
			break
		}
		desired := assignments[(start+i)%len(assignments)]
		select {
		case jobs <- desired:
			c.reconcileCursor = desired.InstanceID
		case <-ctx.Done():
			break dispatch
		}
	}
	close(jobs)
	wg.Wait()
	if ctx.Err() != nil {
		failures = append(failures, ctx.Err())
	}
	return errors.Join(failures...)
}

func (c *slotRuntimeAgentClient) reconcileAssignment(ctx context.Context, desired k8sservice.RuntimeDesiredAssignment) error {
	requestCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var err error
	if desired.State == "draining" {
		err = c.DeleteGateway(requestCtx, desired.Endpoint, desired.GatewayID)
	} else {
		var req RuntimeAgentCreateGatewayRequest
		err = json.Unmarshal(desired.Request, &req)
		if err == nil && (req.InstanceID != desired.InstanceID || req.UserID != desired.UserID || req.Generation != desired.Generation || req.PodUID != desired.PodUID || req.AgentType != desired.RuntimeType) {
			err = fmt.Errorf("persisted request does not match assignment identity")
		}
		if err == nil {
			req.Environment = desired.Environment
			_, err = c.CreateGateway(requestCtx, desired.Endpoint, req)
		}
	}
	return err
}

func NewSlotRuntimeAgentClient(controlToken string, kube kubernetes.Interface, namespace string) RuntimeAgentClient {
	return NewSlotRuntimeAgentClientWithHTTPClient(controlToken, kube, namespace, nil)
}

func NewSlotRuntimeAgentClientWithHTTPClient(controlToken string, kube kubernetes.Interface, namespace string, httpClient *http.Client) RuntimeAgentClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: runtimeAgentControlTimeout}
	}
	copyClient := *httpClient
	base := copyClient.Transport
	if base == nil {
		base = http.DefaultTransport
	}
	copyClient.Transport = slotTLSTransport{base: base}
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return fmt.Errorf("runtime slot redirects are forbidden") }
	embedded := NewRuntimeAgentClientWithHTTPClient(controlToken, &copyClient).(*runtimeAgentHTTPClient)
	if copyClient.Timeout == runtimeAgentControlTimeout {
		embedded.upgradeHTTPClient.Timeout = runtimeAgentUpgradeTimeout
	}
	return &slotRuntimeAgentClient{
		runtimeAgentHTTPClient: embedded,
		kube:                   kube, namespace: namespace, assignments: k8sservice.NewRuntimeAssignmentService(kube, namespace),
	}
}

type slotTLSTransport struct{ base http.RoundTripper }

func (t slotTLSTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" {
		return nil, fmt.Errorf("runtime slots require authenticated HTTPS")
	}
	return t.base.RoundTrip(r)
}

// canRetryPendingGateway is deliberately narrower than CreateGateway: only a
// never-published generation following a confirmed source stop can rebuild env.
func (c *slotRuntimeAgentClient) canRetryPendingGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) error {
	podUID, err := c.targetPodUID(ctx, endpoint)
	if err != nil {
		return err
	}
	if req.PodUID == "" || req.PodUID != podUID {
		return fmt.Errorf("pending runtime target Pod identity changed")
	}
	return c.assignments.CanRetryUnpublished(ctx, k8sservice.RuntimeAssignment{
		InstanceID: req.InstanceID, UserID: req.UserID, Generation: req.Generation, RuntimeType: req.AgentType,
		Endpoint: endpoint, PodUID: podUID, GatewayID: fmt.Sprintf("gw-%d-%d", req.InstanceID, req.Generation),
	})
}

func (c *slotRuntimeAgentClient) CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	podUID, err := c.targetPodUID(ctx, endpoint)
	if err != nil {
		return nil, err
	}
	if req.PodUID != "" && req.PodUID != podUID {
		return nil, fmt.Errorf("runtime target Pod identity changed")
	}
	req.PodUID = podUID
	req, err = c.prepareSecurity(ctx, podUID, req)
	if err != nil {
		return nil, err
	}
	request, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	gatewayID := fmt.Sprintf("gw-%d-%d", req.InstanceID, req.Generation)
	publish := func() error {
		return c.assignments.EnsureDesired(ctx, k8sservice.RuntimeAssignment{
			InstanceID: req.InstanceID, UserID: req.UserID, Generation: req.Generation, RuntimeType: req.AgentType, Endpoint: endpoint, GatewayID: gatewayID, PodUID: podUID, Request: request, Environment: req.Environment,
		})
	}
	c.admissionMu.RLock()
	admit := c.assignmentAdmission
	c.admissionMu.RUnlock()
	if admit != nil {
		err = admit(ctx, podUID, req.AgentType, req.InstanceID, req.Generation, publish)
	} else {
		err = publish()
	}
	if err != nil {
		return nil, err
	}
	// EnsureDesired has already enforced confirmed source stop. Only then may
	// the sticky SQL marker disable legacy credentials for this generation.
	if err := c.protectPublishedGeneration(ctx, req); err != nil {
		return nil, err
	}
	if err := c.assignments.CanStart(ctx, endpoint, gatewayID); err != nil {
		return nil, err
	}
	response, err := c.runtimeAgentHTTPClient.createGateway(ctx, endpoint, req)
	if err != nil {
		return nil, err
	}
	if response == nil || response.GatewayID != gatewayID {
		return nil, fmt.Errorf("create response does not match assigned gateway")
	}
	if err := c.assignments.MarkApplied(ctx, endpoint, gatewayID); err != nil {
		return nil, err
	}
	return response, nil
}

func (c *slotRuntimeAgentClient) DeleteGateway(ctx context.Context, endpoint, gatewayID string) error {
	var err error
	gatewayID, err = c.assignments.ResolvePendingGatewayID(ctx, endpoint, gatewayID)
	if err != nil {
		return err
	}
	if err := c.assignments.MarkDraining(ctx, endpoint, gatewayID); err != nil {
		return err
	}
	var receipt struct {
		GatewayID string `json:"gateway_id"`
		Stopped   bool   `json:"stopped"`
		Confirmed bool   `json:"confirmed"`
		Unmounted bool   `json:"unmounted"`
	}
	if err := c.do(ctx, http.MethodDelete, endpoint, "/v1/gateways/"+url.PathEscape(gatewayID), nil, &receipt); err != nil {
		// An immutable request can be published before the native POST reaches
		// its slot. Unknown DELETE remains unconfirmed; a separate operation
		// must persist a writer-generation fence before it can acknowledge stop.
		if !errors.Is(err, ErrRuntimeAgentConflict) {
			return err
		}
		podUID, identityErr := c.targetPodUID(ctx, endpoint)
		if identityErr != nil {
			return errors.Join(err, identityErr)
		}
		scope, scopeErr := c.assignments.PendingCancellation(ctx, endpoint, gatewayID, podUID)
		if scopeErr != nil {
			return errors.Join(err, scopeErr)
		}
		receipt.GatewayID, receipt.Stopped, receipt.Confirmed, receipt.Unmounted = "", false, false, false
		if cancelErr := c.do(ctx, http.MethodPost, endpoint, "/v1/gateways/cancel-pending", scope, &receipt); cancelErr != nil {
			return errors.Join(err, cancelErr)
		}
	}
	if receipt.GatewayID != gatewayID || !receipt.Stopped || !receipt.Confirmed || !receipt.Unmounted {
		return fmt.Errorf("runtime stop lacks matching confirmed teardown receipt")
	}
	return c.assignments.ConfirmStopped(ctx, endpoint, gatewayID)
}

func (c *slotRuntimeAgentClient) targetPodUID(ctx context.Context, endpoint string) (string, error) {
	if c.kube == nil || strings.TrimSpace(c.namespace) == "" {
		return "", fmt.Errorf("runtime target Kubernetes client and namespace are required")
	}
	parsed, err := url.Parse(endpoint)
	if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Opaque != "" ||
		(parsed.Path != "" && parsed.Path != "/") || parsed.RawPath != "" || strings.ContainsAny(endpoint, "?#") || endpoint != strings.TrimSpace(endpoint) {
		return "", fmt.Errorf("runtime target must be a direct Pod IP endpoint")
	}
	ip := net.ParseIP(parsed.Hostname())
	if ip == nil {
		return "", fmt.Errorf("runtime target must use a literal Pod IP")
	}
	if parsed.Port() != "" {
		port, err := strconv.Atoi(parsed.Port())
		if err != nil || port < 1 || port > 65535 {
			return "", fmt.Errorf("invalid runtime target port")
		}
	}
	pods, err := c.kube.CoreV1().Pods(c.namespace).List(ctx, metav1.ListOptions{
		FieldSelector: "status.podIP=" + parsed.Hostname(),
		LabelSelector: "clawmanager.io/runtime-protocol=container-slots-v1",
	})
	if err != nil {
		return "", err
	}
	var uid string
	for _, pod := range pods.Items {
		// Fake clients ignore field selectors; checking here also protects against
		// stale or unexpectedly broad API results and duplicate Pod IPs.
		if pod.Namespace != c.namespace || !ip.Equal(net.ParseIP(pod.Status.PodIP)) || pod.DeletionTimestamp != nil ||
			pod.Labels["clawmanager.io/runtime-protocol"] != "container-slots-v1" || strings.TrimSpace(string(pod.UID)) == "" {
			continue
		}
		ready := false
		for _, condition := range pod.Status.Conditions {
			if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
				ready = true
			}
		}
		if !ready {
			continue
		}
		if uid != "" {
			return "", fmt.Errorf("runtime target Pod IP is ambiguous")
		}
		uid = string(pod.UID)
	}
	if uid == "" {
		return "", fmt.Errorf("runtime target has no current ready container-slots Pod")
	}
	return uid, nil
}
