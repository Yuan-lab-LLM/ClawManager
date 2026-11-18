package services

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"clawreef/internal/repository"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"clawreef/internal/services/k8s"
)

type ClusterResourceService interface {
	GetOverview(ctx context.Context) (*ClusterResourceOverview, error)
}

type ClusterResourceOverview struct {
	NodeCount  int                  `json:"node_count"`
	ReadyNodes int                  `json:"ready_nodes"`
	CPU        ResourceSummary      `json:"cpu"`
	Memory     ResourceSummary      `json:"memory"`
	Disk       ResourceSummary      `json:"disk"`
	Nodes      []NodeResourceDetail `json:"nodes"`
}

type ResourceSummary struct {
	Capacity    float64    `json:"capacity"`
	Allocatable float64    `json:"allocatable"`
	Requested   float64    `json:"requested"`
	Unit        string     `json:"unit"`
	Used        *float64   `json:"used"`
	ObservedAt  *time.Time `json:"observed_at"`
}

type NodeResourceDetail struct {
	Name           string          `json:"name"`
	Ready          bool            `json:"ready"`
	Roles          []string        `json:"roles"`
	KubeletVersion string          `json:"kubelet_version"`
	InternalIP     string          `json:"internal_ip"`
	PodCount       int             `json:"pod_count"`
	CPU            ResourceSummary `json:"cpu"`
	Memory         ResourceSummary `json:"memory"`
	Disk           ResourceSummary `json:"disk"`
}

type clusterResourceService struct {
	client       *k8s.Client
	instanceRepo repository.InstanceRepository
}

func NewClusterResourceService(instanceRepo repository.InstanceRepository) ClusterResourceService {
	return &clusterResourceService{
		client:       k8s.GetClient(),
		instanceRepo: instanceRepo,
	}
}

func (s *clusterResourceService) GetOverview(ctx context.Context) (*ClusterResourceOverview, error) {
	if s.client == nil || s.client.Clientset == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	nodes, err := s.client.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list nodes: %w", err)
	}

	pods, err := s.client.Clientset.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	podByNode := make(map[string][]corev1.Pod)
	for _, pod := range pods.Items {
		if pod.Spec.NodeName == "" {
			continue
		}
		if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
			continue
		}
		podByNode[pod.Spec.NodeName] = append(podByNode[pod.Spec.NodeName], pod)
	}

	overview := &ClusterResourceOverview{
		NodeCount: len(nodes.Items),
		CPU:       ResourceSummary{Unit: "cores"},
		Memory:    ResourceSummary{Unit: "GiB"},
		Disk:      ResourceSummary{Unit: "GiB"},
		Nodes:     make([]NodeResourceDetail, 0, len(nodes.Items)),
	}

	for _, node := range nodes.Items {
		ready := isNodeReady(node)
		if ready {
			overview.ReadyNodes++
		}

		detail := NodeResourceDetail{
			Name:           node.Name,
			Ready:          ready,
			Roles:          nodeRoles(node),
			KubeletVersion: node.Status.NodeInfo.KubeletVersion,
			InternalIP:     nodeInternalIP(node),
			PodCount:       len(podByNode[node.Name]),
			CPU:            ResourceSummary{Unit: "cores"},
			Memory:         ResourceSummary{Unit: "GiB"},
			Disk:           ResourceSummary{Unit: "GiB"},
		}

		detail.CPU.Capacity = cpuQuantityToCores(node.Status.Capacity[corev1.ResourceCPU])
		detail.CPU.Allocatable = cpuQuantityToCores(node.Status.Allocatable[corev1.ResourceCPU])
		detail.Memory.Capacity = bytesToGiB(node.Status.Capacity[corev1.ResourceMemory])
		detail.Memory.Allocatable = bytesToGiB(node.Status.Allocatable[corev1.ResourceMemory])
		detail.Disk.Capacity = bytesToGiB(node.Status.Capacity[corev1.ResourceEphemeralStorage])
		detail.Disk.Allocatable = bytesToGiB(node.Status.Allocatable[corev1.ResourceEphemeralStorage])

		for _, pod := range podByNode[node.Name] {
			for _, container := range pod.Spec.Containers {
				detail.CPU.Requested += cpuQuantityToCores(container.Resources.Requests[corev1.ResourceCPU])
				detail.Memory.Requested += bytesToGiB(container.Resources.Requests[corev1.ResourceMemory])
				detail.Disk.Requested += bytesToGiB(container.Resources.Requests[corev1.ResourceEphemeralStorage])
			}
		}

		overview.CPU.Capacity += detail.CPU.Capacity
		overview.CPU.Allocatable += detail.CPU.Allocatable
		overview.CPU.Requested += detail.CPU.Requested
		overview.Memory.Capacity += detail.Memory.Capacity
		overview.Memory.Allocatable += detail.Memory.Allocatable
		overview.Memory.Requested += detail.Memory.Requested
		overview.Disk.Capacity += detail.Disk.Capacity
		overview.Disk.Allocatable += detail.Disk.Allocatable
		overview.Disk.Requested += detail.Disk.Requested

		overview.Nodes = append(overview.Nodes, detail)
	}

	s.collectUsage(ctx, overview)
	aggregateClusterUsage(overview)

	sort.Slice(overview.Nodes, func(i, j int) bool {
		return overview.Nodes[i].Name < overview.Nodes[j].Name
	})

	return overview, nil
}

func isNodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func nodeInternalIP(node corev1.Node) string {
	for _, address := range node.Status.Addresses {
		if address.Type == corev1.NodeInternalIP {
			return address.Address
		}
	}
	return ""
}

func nodeRoles(node corev1.Node) []string {
	roles := make([]string, 0)
	for key := range node.Labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
			if role == "" {
				role = "default"
			}
			roles = append(roles, role)
		}
	}
	sort.Strings(roles)
	if len(roles) == 0 {
		return []string{"worker"}
	}
	return roles
}

func cpuQuantityToCores(q resource.Quantity) float64 {
	return float64(q.MilliValue()) / 1000
}

func bytesToGiB(q resource.Quantity) float64 {
	return float64(q.Value()) / 1024 / 1024 / 1024
}

// collectUsage bounds kubelet reads independently from capacity collection. A
// missing or stale node never turns a partial sum into a cluster measurement.
func (s *clusterResourceService) collectUsage(ctx context.Context, overview *ClusterResourceOverview) {
	restClient := s.client.Clientset.CoreV1().RESTClient()
	if restClient == nil {
		return
	}
	jobs := make(chan int)
	var workers sync.WaitGroup
	for w := 0; w < 4; w++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range jobs {
				requestCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
				raw, err := restClient.Get().Resource("nodes").Name(overview.Nodes[i].Name).SubResource("proxy").Suffix("stats/summary").DoRaw(requestCtx)
				cancel()
				if err == nil {
					applyKubeletUsage(&overview.Nodes[i], raw, time.Now().UTC())
				}
			}
		}()
	}
	for i := range overview.Nodes {
		select {
		case jobs <- i:
		case <-ctx.Done():
			close(jobs)
			workers.Wait()
			return
		}
	}
	close(jobs)
	workers.Wait()
}

type kubeletUsage struct {
	Time          time.Time `json:"time"`
	NanoCores     *float64  `json:"usageNanoCores"`
	WorkingSet    *float64  `json:"workingSetBytes"`
	UsedBytes     *float64  `json:"usedBytes"`
	CapacityBytes *float64  `json:"capacityBytes"`
}

func applyKubeletUsage(detail *NodeResourceDetail, raw []byte, now time.Time) {
	var summary struct {
		Node struct {
			CPU    kubeletUsage `json:"cpu"`
			Memory kubeletUsage `json:"memory"`
			FS     kubeletUsage `json:"fs"`
		} `json:"node"`
	}
	if json.Unmarshal(raw, &summary) != nil {
		return
	}
	set := func(target *ResourceSummary, stats kubeletUsage, value *float64, scale float64, disk bool) {
		if value == nil || stats.Time.IsZero() || now.Sub(stats.Time) > 2*time.Minute || stats.Time.After(now.Add(10*time.Second)) {
			return
		}
		used := *value / scale
		if math.IsNaN(used) || math.IsInf(used, 0) || used < 0 || target.Capacity <= 0 || used > target.Capacity {
			return
		}
		if disk {
			if stats.CapacityBytes == nil {
				return
			}
			capacity := *stats.CapacityBytes / scale
			// Kubelet rootfs and advertised ephemeral storage must describe the
			// same filesystem; tolerate quantity rounding, not another mount.
			if math.IsNaN(capacity) || math.IsInf(capacity, 0) || math.Abs(capacity-target.Capacity) > math.Max(0.01, target.Capacity*0.01) {
				return
			}
		}
		target.Used = &used
		observed := stats.Time.UTC()
		target.ObservedAt = &observed
	}
	set(&detail.CPU, summary.Node.CPU, summary.Node.CPU.NanoCores, 1e9, false)
	set(&detail.Memory, summary.Node.Memory, summary.Node.Memory.WorkingSet, 1<<30, false)
	set(&detail.Disk, summary.Node.FS, summary.Node.FS.UsedBytes, 1<<30, true)
}

func aggregateClusterUsage(overview *ClusterResourceOverview) {
	for resourceIndex := 0; resourceIndex < 3; resourceIndex++ {
		target := []*ResourceSummary{&overview.CPU, &overview.Memory, &overview.Disk}[resourceIndex]
		target.Used = nil
		target.ObservedAt = nil
		if len(overview.Nodes) == 0 {
			continue
		}
		total := 0.0
		var oldest time.Time
		complete := true
		for i := range overview.Nodes {
			node := &overview.Nodes[i]
			value := []*ResourceSummary{&node.CPU, &node.Memory, &node.Disk}[resourceIndex]
			if value.Used == nil || value.ObservedAt == nil {
				complete = false
				break
			}
			total += *value.Used
			if oldest.IsZero() || value.ObservedAt.Before(oldest) {
				oldest = *value.ObservedAt
			}
		}
		if complete {
			target.Used = &total
			target.ObservedAt = &oldest
		}
	}
}
