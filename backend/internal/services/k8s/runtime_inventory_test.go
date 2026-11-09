package k8s

import (
	"context"
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRuntimeInventoryUsesConfiguredSlotsAndActualContainerStatus(t *testing.T) {
	dep := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "runtime", Namespace: "system", RuntimeType: "openclaw", Image: "runtime:v1", Replicas: 1})
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "runtime-a", Namespace: "system", Labels: dep.Spec.Selector.MatchLabels}, Spec: corev1.PodSpec{Containers: []corev1.Container{
		{Name: "runtime", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")}}},
		{Name: "slot-1", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")}}},
		{Name: "slot-0", Resources: corev1.ResourceRequirements{Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("2"), corev1.ResourceMemory: resource.MustParse("4Gi")}}},
	}}, Status: corev1.PodStatus{ContainerStatuses: []corev1.ContainerStatus{
		{Name: "slot-0", Ready: true, State: corev1.ContainerState{Running: &corev1.ContainerStateRunning{}}},
		{Name: "slot-1", RestartCount: 4, State: corev1.ContainerState{Waiting: &corev1.ContainerStateWaiting{Reason: "CrashLoopBackOff"}}},
	}}}
	service := NewRuntimeDeploymentService(fake.NewSimpleClientset(dep, pod))
	pods, err := service.ListPods(context.Background(), "system", "openclaw")
	if err != nil || len(pods) != 1 {
		t.Fatalf("inventory: %v %+v", err, pods)
	}
	data, _ := json.Marshal(pods[0])
	var got struct {
		Capacity               int
		CPUMillisLimit         *int64
		MemoryBytesLimit       *int64
		CoordinatorCPUMillis   *int64
		CoordinatorMemoryBytes *int64
		Slots                  []struct {
			Index            int
			ContainerName    string `json:"container_name"`
			Ready            bool
			RestartCount     int32 `json:"restart_count"`
			Reason           string
			State            string
			CPUMillisLimit   *int64 `json:"cpu_millis_limit"`
			MemoryBytesLimit *int64 `json:"memory_bytes_limit"`
		}
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Capacity != 2 || len(got.Slots) != 2 {
		t.Fatalf("configured capacity = %s", data)
	}
	if got.Slots[0].Index != 1 || got.Slots[0].ContainerName != "slot-0" || !got.Slots[0].Ready || got.Slots[0].State != "running" {
		t.Fatalf("first slot = %+v", got.Slots[0])
	}
	if got.Slots[0].CPUMillisLimit == nil || *got.Slots[0].CPUMillisLimit != 2000 || got.Slots[0].MemoryBytesLimit == nil || *got.Slots[0].MemoryBytesLimit != 4*1024*1024*1024 {
		t.Fatalf("slot resource limits = %+v", got.Slots[0])
	}
	if got.Slots[1].Ready || got.Slots[1].RestartCount != 4 || got.Slots[1].Reason != "CrashLoopBackOff" || got.Slots[1].State != "waiting" {
		t.Fatalf("second slot = %+v", got.Slots[1])
	}
	if got.CoordinatorCPUMillis == nil || *got.CoordinatorCPUMillis != 100 || got.CoordinatorMemoryBytes == nil || *got.CoordinatorMemoryBytes != 128*1024*1024 {
		t.Fatalf("coordinator reservation = %s", data)
	}
	if got.CPUMillisLimit == nil || *got.CPUMillisLimit != 4100 || got.MemoryBytesLimit == nil || *got.MemoryBytesLimit != int64(128*1024*1024+8*1024*1024*1024) {
		t.Fatalf("resource limits = %s", data)
	}
}

func TestRuntimeInventoryEventSourceReadsOnlyRequestedPod(t *testing.T) {
	service := NewRuntimeDeploymentService(fake.NewSimpleClientset(
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "system"}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "runtime-a"}, Type: "Warning", Reason: "BackOff", Message: "container restarted", Count: 4},
		&corev1.Event{ObjectMeta: metav1.ObjectMeta{Name: "b", Namespace: "system"}, InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "other"}, Reason: "Pulled"},
	))
	source, ok := service.(interface {
		ListRuntimePodEvents(context.Context, string, string) ([]corev1.Event, error)
	})
	if !ok {
		t.Fatal("runtime deployment inventory does not expose Kubernetes pod events")
	}
	events, err := source.ListRuntimePodEvents(context.Background(), "system", "runtime-a")
	if err != nil || len(events) != 1 || events[0].Reason != "BackOff" {
		t.Fatalf("events = %+v %v", events, err)
	}
}

func TestRuntimeInventoryWorkspaceIdentityUsesActualPodVolume(t *testing.T) {
	dep := BuildRuntimeDeployment(RuntimeDeploymentSpec{Name: "runtime", Namespace: "system", RuntimeType: "openclaw", Image: "runtime:v1", Replicas: 1})
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "runtime-a", Namespace: "system", Labels: dep.Spec.Selector.MatchLabels}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "workspaces", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "source"}}}}, Containers: []corev1.Container{{Name: "runtime", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}, {Name: "slot-0", VolumeMounts: []corev1.VolumeMount{{Name: "workspaces", MountPath: "/workspaces"}}}}}}
	changed := pod.DeepCopy()
	changed.Name = "runtime-b"
	changed.Spec.Volumes[0].PersistentVolumeClaim.ClaimName = "target"
	dynamic := pod.DeepCopy()
	dynamic.Name = "runtime-c"
	dynamic.Spec.Containers[1].VolumeMounts[0].SubPathExpr = "$(POD_NAME)"
	service := NewRuntimeDeploymentService(fake.NewSimpleClientset(dep, pod, changed, dynamic, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "source", Namespace: "system", UID: "source-uid"}}, &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "system", UID: "target-uid"}}))
	pods, err := service.ListPods(context.Background(), "system", "openclaw")
	if err != nil || len(pods) != 3 {
		t.Fatalf("inventory %+v %v", pods, err)
	}
	data, _ := json.Marshal(pods)
	var got []struct{ WorkspaceStorageIdentity string }
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got[0].WorkspaceStorageIdentity == "" || got[1].WorkspaceStorageIdentity == "" || got[0].WorkspaceStorageIdentity == got[1].WorkspaceStorageIdentity {
		t.Fatalf("different actual PVCs not distinguished: %s", data)
	}
	if got[2].WorkspaceStorageIdentity != "" {
		t.Fatalf("dynamic subPathExpr accepted as shared storage: %s", data)
	}
}

func TestRuntimeInventoryKeepsRequestsSeparateFromSharedLimits(t *testing.T) {
	pod := corev1.Pod{Spec: corev1.PodSpec{Resources: &corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("16Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("4"), corev1.ResourceMemory: resource.MustParse("16Gi")}}, Containers: []corev1.Container{
		{Name: "runtime", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("128Mi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("512Mi")}}},
		{Name: "slot-0", Resources: corev1.ResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("100m"), corev1.ResourceMemory: resource.MustParse("1Gi")}, Limits: corev1.ResourceList{corev1.ResourceCPU: resource.MustParse("1"), corev1.ResourceMemory: resource.MustParse("4Gi")}}},
	}}}
	cpu, memory := runtimePodRequests(pod)
	coordCPU, coordMemory := runtimeCoordinatorRequests(pod)
	slots := runtimePodSlots(pod)
	if *cpu != 4000 || *memory != 16<<30 || *coordCPU != 100 || *coordMemory != 128<<20 {
		t.Fatal("Pod or coordinator requests replaced by hard limits")
	}
	if len(slots) != 1 || slots[0].CPUMillisRequest == nil || *slots[0].CPUMillisRequest != 100 || slots[0].MemoryBytesRequest == nil || *slots[0].MemoryBytesRequest != 1<<30 || *slots[0].MemoryBytesLimit != 4<<30 {
		t.Fatal("slot reservation and burst ceiling must remain distinct")
	}
}
