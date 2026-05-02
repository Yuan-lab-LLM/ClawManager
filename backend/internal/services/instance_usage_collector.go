package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
)

// InstanceUsageCollector periodically polls K8s metrics-server for running
// instances and records resource usage snapshots into the database.
type InstanceUsageCollector struct {
	instanceRepo repository.InstanceRepository
	usageRepo    repository.InstanceUsageRepository
	client       *k8s.Client
	podService   *k8s.PodService
	interval     time.Duration
	stopChan     chan struct{}
	stopOnce     sync.Once
	collecting   sync.Mutex
}

// NewInstanceUsageCollector creates a new collector. The collection interval
// defaults to 60 seconds and can be overridden with the USAGE_COLLECT_INTERVAL
// environment variable (in seconds).
func NewInstanceUsageCollector(
	instanceRepo repository.InstanceRepository,
	usageRepo repository.InstanceUsageRepository,
) *InstanceUsageCollector {
	interval := 60 * time.Second
	if v := os.Getenv("USAGE_COLLECT_INTERVAL"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			interval = time.Duration(secs) * time.Second
		}
	}
	return &InstanceUsageCollector{
		instanceRepo: instanceRepo,
		usageRepo:    usageRepo,
		client:       k8s.GetClient(),
		podService:   k8s.NewPodService(),
		interval:     interval,
		stopChan:     make(chan struct{}),
	}
}

// Start launches the collection loop in a background goroutine.
func (c *InstanceUsageCollector) Start() {
	log.Printf("[UsageCollector] Starting with interval %v", c.interval)
	go c.loop()
}

// Stop signals the collector to shut down. Safe to call multiple times.
func (c *InstanceUsageCollector) Stop() {
	c.stopOnce.Do(func() {
		log.Println("[UsageCollector] Stopping...")
		close(c.stopChan)
	})
}

func (c *InstanceUsageCollector) loop() {
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()

	// Run once immediately on start.
	c.safeTick()

	for {
		select {
		case <-ticker.C:
			c.safeTick()
		case <-c.stopChan:
			log.Println("[UsageCollector] Stopped")
			return
		}
	}
}

// safeTick wraps tick with panic recovery and overlap guard.
func (c *InstanceUsageCollector) safeTick() {
	if !c.collecting.TryLock() {
		log.Println("[UsageCollector] Previous collection still running, skipping tick")
		return
	}
	defer c.collecting.Unlock()

	defer func() {
		if r := recover(); r != nil {
			log.Printf("[UsageCollector] Recovered from panic: %v", r)
		}
	}()

	c.collectAll()
}

func (c *InstanceUsageCollector) collectAll() {
	if c.client == nil || c.client.Clientset == nil {
		log.Println("[UsageCollector] K8s client not initialized, skipping collection")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	instances, err := c.instanceRepo.GetAllRunning()
	if err != nil {
		log.Printf("[UsageCollector] Failed to list running instances: %v", err)
		return
	}
	if len(instances) == 0 {
		return
	}

	metricsAvailable := c.isMetricsServerAvailable(ctx)
	if !metricsAvailable {
		log.Println("[UsageCollector] metrics-server not available, recording uptime only")
	}

	now := time.Now()
	recorded := 0
	for _, inst := range instances {
		record := c.collectInstance(ctx, &inst, now, metricsAvailable)
		if record != nil {
			if err := c.usageRepo.Create(record); err != nil {
				log.Printf("[UsageCollector] Failed to record usage for instance %d: %v", inst.ID, err)
			} else {
				recorded++
			}
		}
	}

	if recorded > 0 {
		log.Printf("[UsageCollector] Recorded usage for %d/%d instances", recorded, len(instances))
	}
}

// collectInstance gathers resource usage for a single instance. Returns nil if
// the pod cannot be found (instance may have just stopped).
func (c *InstanceUsageCollector) collectInstance(
	ctx context.Context,
	instance *models.Instance,
	now time.Time,
	metricsAvailable bool,
) *models.InstanceUsage {
	pod, err := c.podService.GetPod(ctx, instance.UserID, instance.ID)
	if err != nil {
		// Pod not found — instance may have just stopped, skip silently.
		return nil
	}

	record := &models.InstanceUsage{
		InstanceID: instance.ID,
		RecordedAt: now,
	}

	// Calculate uptime from pod start time.
	if pod.Status.StartTime != nil {
		uptime := int(now.Sub(pod.Status.StartTime.Time).Seconds())
		if uptime < 0 {
			uptime = 0
		}
		record.UptimeSeconds = &uptime
	}

	// Fetch CPU and memory from metrics-server if available.
	if metricsAvailable {
		namespace := c.client.GetNamespace(instance.UserID)
		metrics, err := c.fetchPodMetrics(ctx, namespace, pod.Name)
		if err != nil {
			// Non-fatal: we still have uptime.
			log.Printf("[UsageCollector] Failed to fetch metrics for pod %s: %v", pod.Name, err)
			return record
		}

		// Sum CPU and memory across all containers in the pod.
		var totalCPUMillicores int64
		var totalMemoryBytes int64
		for _, container := range metrics.Containers {
			totalCPUMillicores += parseCPUToMillicores(container.Usage.CPU)
			totalMemoryBytes += parseMemoryToBytes(container.Usage.Memory)
		}

		// CPU usage as percentage of instance's allocated cores.
		if instance.CPUCores > 0 {
			cpuPercent := (float64(totalCPUMillicores) / 1000.0 / instance.CPUCores) * 100.0
			cpuPercent = math.Round(cpuPercent*100) / 100 // round to 2 decimals
			record.CPUUsagePercent = &cpuPercent
		}

		// Memory usage in GB.
		memGB := float64(totalMemoryBytes) / (1024 * 1024 * 1024)
		memGB = math.Round(memGB*100) / 100
		record.MemoryUsageGB = &memGB
	}

	return record
}

// isMetricsServerAvailable checks whether the metrics-server API is reachable.
func (c *InstanceUsageCollector) isMetricsServerAvailable(ctx context.Context) bool {
	body, err := c.client.Clientset.RESTClient().Get().
		AbsPath("/apis/metrics.k8s.io/v1beta1").
		DoRaw(ctx)
	if err != nil {
		return false
	}
	// A successful response (even empty resource list) means the API is up.
	return len(body) > 0
}

// podMetricsResponse mirrors the metrics-server PodMetrics JSON structure.
// We define it locally to avoid adding k8s.io/metrics as a dependency.
type podMetricsResponse struct {
	Containers []containerMetrics `json:"containers"`
}

type containerMetrics struct {
	Name  string        `json:"name"`
	Usage resourceUsage `json:"usage"`
}

type resourceUsage struct {
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// fetchPodMetrics calls the metrics-server API for a specific pod.
func (c *InstanceUsageCollector) fetchPodMetrics(ctx context.Context, namespace, podName string) (*podMetricsResponse, error) {
	path := fmt.Sprintf("/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods/%s", namespace, podName)
	resp, err := c.client.Clientset.RESTClient().Get().
		AbsPath(path).
		Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("metrics request failed: %w", err)
	}
	defer resp.Close()

	data, err := io.ReadAll(resp)
	if err != nil {
		return nil, fmt.Errorf("failed to read metrics response: %w", err)
	}

	var metrics podMetricsResponse
	if err := json.Unmarshal(data, &metrics); err != nil {
		return nil, fmt.Errorf("failed to parse metrics response: %w", err)
	}
	return &metrics, nil
}

// parseCPUToMillicores converts a K8s CPU quantity string to millicores.
// Examples: "100m" → 100, "2" → 2000, "1500n" → 1 (rounded).
func parseCPUToMillicores(cpu string) int64 {
	cpu = strings.TrimSpace(cpu)
	if cpu == "" {
		return 0
	}
	if strings.HasSuffix(cpu, "n") {
		// Nanocores → millicores
		val, err := strconv.ParseInt(strings.TrimSuffix(cpu, "n"), 10, 64)
		if err != nil {
			return 0
		}
		return val / 1_000_000
	}
	if strings.HasSuffix(cpu, "m") {
		val, err := strconv.ParseInt(strings.TrimSuffix(cpu, "m"), 10, 64)
		if err != nil {
			return 0
		}
		return val
	}
	// Plain number = whole cores
	val, err := strconv.ParseFloat(cpu, 64)
	if err != nil {
		return 0
	}
	return int64(val * 1000)
}

// parseMemoryToBytes converts a K8s memory quantity string to bytes.
// Examples: "128Mi" → 134217728, "1Gi" → 1073741824, "1000Ki" → 1024000.
func parseMemoryToBytes(mem string) int64 {
	mem = strings.TrimSpace(mem)
	if mem == "" {
		return 0
	}

	suffixes := []struct {
		suffix     string
		multiplier int64
	}{
		{"Ti", 1024 * 1024 * 1024 * 1024},
		{"Gi", 1024 * 1024 * 1024},
		{"Mi", 1024 * 1024},
		{"Ki", 1024},
		{"T", 1000_000_000_000},
		{"G", 1000_000_000},
		{"M", 1000_000},
		{"K", 1000},
	}

	for _, s := range suffixes {
		if strings.HasSuffix(mem, s.suffix) {
			val, err := strconv.ParseInt(strings.TrimSuffix(mem, s.suffix), 10, 64)
			if err != nil {
				return 0
			}
			return val * s.multiplier
		}
	}

	// Plain bytes
	val, err := strconv.ParseInt(mem, 10, 64)
	if err != nil {
		return 0
	}
	return val
}

