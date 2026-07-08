package k8s

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func TestHostPathPVNodeAffinitySelectsReadyNode(t *testing.T) {
	service := &PVCService{
		client: &Client{
			Clientset: fake.NewSimpleClientset(
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-b",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node-b-host",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-a",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node-a-host",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			),
		},
	}

	affinity, err := service.hostPathPVNodeAffinity(context.Background())
	if err != nil {
		t.Fatalf("hostPathPVNodeAffinity returned error: %v", err)
	}
	requireHostnameAffinity(t, affinity, "node-a-host")
}

func TestHostPathPVNodeAffinitySkipsUnschedulableAndNotReadyNodes(t *testing.T) {
	service := &PVCService{
		client: &Client{
			Clientset: fake.NewSimpleClientset(
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-a",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node-a-host",
						},
					},
					Spec: corev1.NodeSpec{Unschedulable: true},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-b",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node-b-host",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionFalse},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "node-c",
						Labels: map[string]string{
							"kubernetes.io/hostname": "node-c-host",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			),
		},
	}

	affinity, err := service.hostPathPVNodeAffinity(context.Background())
	if err != nil {
		t.Fatalf("hostPathPVNodeAffinity returned error: %v", err)
	}
	requireHostnameAffinity(t, affinity, "node-c-host")
}

func TestCreatePVForTeamSharedPVCUsesWorkspaceNFSWhenConfigured(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	namespace := "clawmanager-user-1"
	pvcName := "clawreef-team-28-shared"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pvcName,
			Namespace:       namespace,
			UID:             types.UID("pvc-uid"),
			ResourceVersion: "1",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	clientset := fake.NewSimpleClientset(pvc)
	service := &PVCService{
		client: &Client{
			Clientset:          clientset,
			Namespace:          "clawmanager",
			WorkspaceRoot:      workspaceRoot,
			WorkspaceNFSServer: "workspace-store.clawmanager-system.svc.cluster.local",
			WorkspaceNFSPath:   "/",
		},
	}

	if _, err := service.createPVForTeamSharedPVC(ctx, namespace, pvcName, 1, 28, 10, "manual"); err != nil {
		t.Fatalf("createPVForTeamSharedPVC returned error: %v", err)
	}

	pv, err := clientset.CoreV1().PersistentVolumes().Get(ctx, "clawreef-pv-clawmanager-user-1-team-28-shared", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Team shared PV: %v", err)
	}
	if pv.Spec.PersistentVolumeSource.NFS == nil {
		t.Fatalf("expected NFS PV source, got %#v", pv.Spec.PersistentVolumeSource)
	}
	if pv.Spec.PersistentVolumeSource.NFS.Server != "workspace-store.clawmanager-system.svc.cluster.local" {
		t.Fatalf("unexpected NFS server: %#v", pv.Spec.PersistentVolumeSource.NFS)
	}
	if pv.Spec.PersistentVolumeSource.NFS.Path != "/teams/user-1/team-28-shared" {
		t.Fatalf("unexpected NFS path: %#v", pv.Spec.PersistentVolumeSource.NFS)
	}
	if pv.Spec.NodeAffinity != nil {
		t.Fatalf("NFS-backed Team PV must not pin runtime sharing to one node: %#v", pv.Spec.NodeAffinity)
	}
	if _, err := os.Stat(filepath.Join(workspaceRoot, "teams", "user-1", "team-28-shared")); err != nil {
		t.Fatalf("expected runtime workspace team directory to be created: %v", err)
	}
}

func TestCreatePVForTeamSharedPVCResolvesWorkspaceServiceDNS(t *testing.T) {
	ctx := context.Background()
	namespace := "clawmanager-user-1"
	pvcName := "clawreef-team-28-shared"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            pvcName,
			Namespace:       namespace,
			UID:             types.UID("pvc-uid"),
			ResourceVersion: "1",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workspace-store",
			Namespace: "clawmanager-system",
		},
		Spec: corev1.ServiceSpec{ClusterIP: "10.96.0.44"},
	}
	clientset := fake.NewSimpleClientset(pvc, service)
	pvcService := &PVCService{
		client: &Client{
			Clientset:          clientset,
			Namespace:          "clawmanager",
			WorkspaceRoot:      t.TempDir(),
			WorkspaceNFSServer: "workspace-store.clawmanager-system.svc.cluster.local",
			WorkspaceNFSPath:   "/",
		},
	}

	if _, err := pvcService.createPVForTeamSharedPVC(ctx, namespace, pvcName, 1, 28, 10, "manual"); err != nil {
		t.Fatalf("createPVForTeamSharedPVC returned error: %v", err)
	}

	pv, err := clientset.CoreV1().PersistentVolumes().Get(ctx, "clawreef-pv-clawmanager-user-1-team-28-shared", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Team shared PV: %v", err)
	}
	if pv.Spec.NFS == nil || pv.Spec.NFS.Server != "10.96.0.44" {
		t.Fatalf("expected resolved ClusterIP NFS server, got %#v", pv.Spec.PersistentVolumeSource)
	}
}

func TestCreateTeamSharedPVCCreatesWorkspaceDirectoryBeforeReturning(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	client := &Client{
		Clientset:          fake.NewSimpleClientset(),
		Namespace:          "clawmanager",
		StorageClass:       "manual",
		WorkspaceRoot:      workspaceRoot,
		WorkspaceNFSServer: "workspace-store.clawmanager-system.svc.cluster.local",
		WorkspaceNFSPath:   "/",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	if _, err := service.CreateTeamSharedPVC(ctx, 1, 28, 10, "manual"); err != nil {
		t.Fatalf("CreateTeamSharedPVC returned error: %v", err)
	}

	if _, err := os.Stat(filepath.Join(workspaceRoot, "teams", "user-1", "team-28-shared")); err != nil {
		t.Fatalf("expected Team shared runtime workspace directory before Lite gateways start: %v", err)
	}
}

func TestCreateTeamSharedPVCCreatesWritableRuntimeSubdirectories(t *testing.T) {
	ctx := context.Background()
	workspaceRoot := t.TempDir()
	client := &Client{
		Clientset:          fake.NewSimpleClientset(),
		Namespace:          "clawmanager",
		StorageClass:       "manual",
		WorkspaceRoot:      workspaceRoot,
		WorkspaceNFSServer: "workspace-store.clawmanager-system.svc.cluster.local",
		WorkspaceNFSPath:   "/",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	if _, err := service.CreateTeamSharedPVC(ctx, 1, 28, 10, "manual"); err != nil {
		t.Fatalf("CreateTeamSharedPVC returned error: %v", err)
	}

	for _, name := range []string{"status", "inbox", "results", "tasks", ".openclaw-redis-team", filepath.Join(".openclaw-redis-team", "tasks")} {
		dir := filepath.Join(workspaceRoot, "teams", "user-1", "team-28-shared", name)
		info, err := os.Stat(dir)
		if err != nil {
			t.Fatalf("expected Team shared runtime subdirectory %s before gateways start: %v", name, err)
		}
		if !info.IsDir() {
			t.Fatalf("expected %s to be a directory", dir)
		}
		if runtime.GOOS != "windows" {
			mode := info.Mode()
			if mode&os.ModeSetgid == 0 || mode.Perm()&0o020 == 0 {
				t.Fatalf("expected %s to preserve shared group-write/setgid permissions, got mode %v", dir, mode)
			}
		}
	}
}

func TestCreateTeamSharedPVCCreatesWorkspaceNFSPVBeforeReturning(t *testing.T) {
	ctx := context.Background()
	client := &Client{
		Clientset:          fake.NewSimpleClientset(),
		Namespace:          "clawmanager",
		StorageClass:       "manual",
		WorkspaceRoot:      t.TempDir(),
		WorkspaceNFSServer: "workspace-store.clawmanager-system.svc.cluster.local",
		WorkspaceNFSPath:   "/",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	if _, err := service.CreateTeamSharedPVC(ctx, 1, 28, 10, "manual"); err != nil {
		t.Fatalf("CreateTeamSharedPVC returned error: %v", err)
	}

	pv, err := client.Clientset.CoreV1().PersistentVolumes().Get(ctx, "clawreef-pv-clawmanager-user-1-team-28-shared", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Team shared NFS PV before Lite gateways start: %v", err)
	}
	if pv.Spec.NFS == nil || pv.Spec.NFS.Path != "/teams/user-1/team-28-shared" {
		t.Fatalf("unexpected Team shared PV source: %#v", pv.Spec.PersistentVolumeSource)
	}
}

func TestCreateTeamSharedPVCUsesWorkspaceNFSInsteadOfInstanceStorageClass(t *testing.T) {
	ctx := context.Background()
	client := &Client{
		Clientset:            fake.NewSimpleClientset(),
		Namespace:            "clawmanager",
		StorageClass:         "longhorn",
		InstanceStorageClass: "longhorn",
		WorkspaceRoot:        t.TempDir(),
		WorkspaceNFSServer:   "workspace-store.clawmanager-system.svc.cluster.local",
		WorkspaceNFSPath:     "/",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	pvc, err := service.CreateTeamSharedPVC(ctx, 1, 28, 10, "")
	if err != nil {
		t.Fatalf("CreateTeamSharedPVC returned error: %v", err)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "manual" {
		t.Fatalf("Team shared PVC storage class = %#v, want manual static NFS binding", pvc.Spec.StorageClassName)
	}
	pv, err := client.Clientset.CoreV1().PersistentVolumes().Get(ctx, "clawreef-pv-clawmanager-user-1-team-28-shared", metav1.GetOptions{})
	if err != nil {
		t.Fatalf("expected Team shared NFS PV: %v", err)
	}
	if pv.Spec.NFS == nil {
		t.Fatalf("Team shared PV should use workspace NFS, got %#v", pv.Spec.PersistentVolumeSource)
	}
}

func TestCreateTeamSharedPVCRejectsExistingNonWorkspaceNFSPV(t *testing.T) {
	ctx := context.Background()
	namespace := "clawmanager-user-1"
	pvcName := "clawreef-team-28-shared"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pvcName,
			Namespace: namespace,
			Labels:    map[string]string{"team-id": "28"},
		},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "clawreef-pv-clawmanager-user-1-team-28-shared"},
		Spec: corev1.PersistentVolumeSpec{
			ClaimRef: &corev1.ObjectReference{Namespace: namespace, Name: pvcName},
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/data/clawreef/user-1/team-28-shared"},
			},
		},
	}
	client := &Client{
		Clientset:          fake.NewSimpleClientset(pvc, pv),
		Namespace:          "clawmanager",
		StorageClass:       "manual",
		WorkspaceRoot:      t.TempDir(),
		WorkspaceNFSServer: "workspace-store.clawmanager-system.svc.cluster.local",
		WorkspaceNFSPath:   "/",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	_, err := service.CreateTeamSharedPVC(ctx, 1, 28, 10, "manual")
	if err == nil || !strings.Contains(err.Error(), "workspace NFS") {
		t.Fatalf("expected existing hostPath Team PV to be rejected, got %v", err)
	}
}

func TestCreatePVCUsesInstanceStorageClassDefault(t *testing.T) {
	ctx := context.Background()
	client := &Client{
		Clientset:            fake.NewSimpleClientset(),
		Namespace:            "clawmanager",
		StorageClass:         "standard",
		InstanceStorageClass: "longhorn",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	pvc, err := service.CreatePVC(ctx, 1, 371, 10, "")
	if err != nil {
		t.Fatalf("CreatePVC returned error: %v", err)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "longhorn" {
		t.Fatalf("PVC storage class = %#v, want longhorn", pvc.Spec.StorageClassName)
	}
}

func TestCreateTeamSharedPVCUsesWorkspaceStorageDefaults(t *testing.T) {
	ctx := context.Background()
	client := &Client{
		Clientset:             fake.NewSimpleClientset(),
		Namespace:             "clawmanager",
		StorageClass:          "standard",
		WorkspaceStorageClass: "longhorn-rwx",
		WorkspaceAccessMode:   "ReadWriteMany",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	pvc, err := service.CreateTeamSharedPVC(ctx, 1, 28, 10, "")
	if err != nil {
		t.Fatalf("CreateTeamSharedPVC returned error: %v", err)
	}
	if pvc.Spec.StorageClassName == nil || *pvc.Spec.StorageClassName != "longhorn-rwx" {
		t.Fatalf("PVC storage class = %#v, want longhorn-rwx", pvc.Spec.StorageClassName)
	}
	if len(pvc.Spec.AccessModes) != 1 || pvc.Spec.AccessModes[0] != corev1.ReadWriteMany {
		t.Fatalf("PVC access modes = %#v, want ReadWriteMany", pvc.Spec.AccessModes)
	}
}

func TestWaitForPVCBindingDoesNotCreateHostPathPVWhenFallbackDisabled(t *testing.T) {
	ctx := context.Background()
	namespace := "clawmanager-user-162"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clawreef-371-pvc",
			Namespace: namespace,
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
	clientset := fake.NewSimpleClientset(pvc)
	service := &PVCService{
		client: &Client{
			Clientset:               clientset,
			Namespace:               "clawmanager",
			StorageProfile:          "cluster",
			HostPathFallbackEnabled: false,
		},
	}

	_, err := service.waitForPVCBinding(ctx, namespace, pvc.Name, 162, 371, 10, "manual", time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "hostPath fallback is disabled") {
		t.Fatalf("expected disabled fallback error, got %v", err)
	}
	pvs, listErr := clientset.CoreV1().PersistentVolumes().List(ctx, metav1.ListOptions{})
	if listErr != nil {
		t.Fatalf("failed to list PVs: %v", listErr)
	}
	if len(pvs.Items) != 0 {
		t.Fatalf("expected no hostPath PV to be created, got %#v", pvs.Items)
	}
}

func TestDeletePVCSkipsDynamicProvisionerPV(t *testing.T) {
	ctx := context.Background()
	namespace := "clawmanager-user-162"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clawreef-371-pvc",
			Namespace: namespace,
			Labels:    map[string]string{"instance-id": "371"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: "pvc-dynamic-123",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{
			Name: "pvc-dynamic-123",
			Labels: map[string]string{
				"managed-by": "external-provisioner",
			},
		},
		Spec: corev1.PersistentVolumeSpec{
			PersistentVolumeSource: corev1.PersistentVolumeSource{
				CSI: &corev1.CSIPersistentVolumeSource{Driver: "driver.longhorn.io"},
			},
		},
	}
	client := &Client{
		Clientset: fake.NewSimpleClientset(pvc, pv),
		Namespace: "clawmanager",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	if err := service.DeletePVC(ctx, 162, 371); err != nil {
		t.Fatalf("DeletePVC returned error: %v", err)
	}
	if _, err := client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, pvc.Name, metav1.GetOptions{}); !errors.IsNotFound(err) {
		t.Fatalf("expected PVC to be deleted, got err=%v", err)
	}
	if _, err := client.Clientset.CoreV1().PersistentVolumes().Get(ctx, pv.Name, metav1.GetOptions{}); err != nil {
		t.Fatalf("dynamic provisioner PV must not be deleted, got %v", err)
	}
}

func TestHostPathPVNodeAffinitySkipsHardTaintedNodes(t *testing.T) {
	service := &PVCService{
		client: &Client{
			Clientset: fake.NewSimpleClientset(
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "k8s-master",
						Labels: map[string]string{
							"kubernetes.io/hostname": "k8s-master",
						},
					},
					Spec: corev1.NodeSpec{
						Taints: []corev1.Taint{
							{Key: "node-role.kubernetes.io/control-plane", Effect: corev1.TaintEffectNoSchedule},
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{
						Name: "k8s-worker1",
						Labels: map[string]string{
							"kubernetes.io/hostname": "k8s-worker1",
						},
					},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{
							{Type: corev1.NodeReady, Status: corev1.ConditionTrue},
						},
					},
				},
			),
		},
	}

	affinity, err := service.hostPathPVNodeAffinity(context.Background())
	if err != nil {
		t.Fatalf("hostPathPVNodeAffinity returned error: %v", err)
	}
	requireHostnameAffinity(t, affinity, "k8s-worker1")
}

func TestNodeSelectorForPVCUsesBoundPVNodeAffinity(t *testing.T) {
	ctx := context.Background()
	namespace := "clawmanager-user-162"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clawreef-371-pvc",
			Namespace: namespace,
			Labels:    map[string]string{"instance-id": "371"},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			VolumeName: "clawreef-pv-user-162-instance-371",
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound},
	}
	pv := &corev1.PersistentVolume{
		ObjectMeta: metav1.ObjectMeta{Name: "clawreef-pv-user-162-instance-371"},
		Spec: corev1.PersistentVolumeSpec{
			NodeAffinity: hostPathPVNodeAffinityForHostname("node125"),
		},
	}
	client := &Client{
		Clientset: fake.NewSimpleClientset(pvc, pv),
		Namespace: "clawmanager",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	selector, err := service.NodeSelectorForPVC(ctx, 162, 371, "manual")
	if err != nil {
		t.Fatalf("NodeSelectorForPVC returned error: %v", err)
	}
	if selector["kubernetes.io/hostname"] != "node125" {
		t.Fatalf("selector = %#v, want hostname node125", selector)
	}
}

func TestNodeSelectorForPVCFallsBackForNoProvisionerStorageClass(t *testing.T) {
	ctx := context.Background()
	namespace := "clawmanager-user-162"
	storageClassName := "manual"
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clawreef-371-pvc",
			Namespace: namespace,
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClassName,
		},
		Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimPending},
	}
	storageClass := &storagev1.StorageClass{
		ObjectMeta:  metav1.ObjectMeta{Name: "manual"},
		Provisioner: "kubernetes.io/no-provisioner",
	}
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "node125",
			Labels: map[string]string{"kubernetes.io/hostname": "node125"},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
		},
	}
	client := &Client{
		Clientset:    fake.NewSimpleClientset(pvc, storageClass, node),
		Namespace:    "clawmanager",
		StorageClass: "manual",
	}
	service := &PVCService{
		client:           client,
		namespaceService: &NamespaceService{client: client},
	}

	selector, err := service.NodeSelectorForPVC(ctx, 162, 371, "")
	if err != nil {
		t.Fatalf("NodeSelectorForPVC returned error: %v", err)
	}
	if selector["kubernetes.io/hostname"] != "node125" {
		t.Fatalf("selector = %#v, want hostname node125", selector)
	}
}

func TestWaitForPVCBindingLeavesDynamicStorageClassToProvisioner(t *testing.T) {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "clawreef-114-pvc",
			Namespace: "clawmanager-user-155",
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			StorageClassName: stringPtr("local-path"),
		},
		Status: corev1.PersistentVolumeClaimStatus{
			Phase: corev1.ClaimPending,
		},
	}
	service := &PVCService{
		client: &Client{
			Clientset: fake.NewSimpleClientset(pvc),
		},
	}

	got, err := service.waitForPVCBinding(context.Background(), pvc.Namespace, pvc.Name, 155, 114, 100, "local-path", time.Nanosecond)
	if err != nil {
		t.Fatalf("waitForPVCBinding returned error: %v", err)
	}
	if got.Name != pvc.Name {
		t.Fatalf("expected PVC %q, got %q", pvc.Name, got.Name)
	}
	pvs, err := service.client.Clientset.CoreV1().PersistentVolumes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		t.Fatalf("failed to list PVs: %v", err)
	}
	if len(pvs.Items) != 0 {
		t.Fatalf("expected no manual PVs for dynamic storageClass, got %#v", pvs.Items)
	}
}

func stringPtr(value string) *string {
	return &value
}

func requireHostnameAffinity(t *testing.T, affinity *corev1.VolumeNodeAffinity, hostname string) {
	t.Helper()
	if affinity == nil || affinity.Required == nil || len(affinity.Required.NodeSelectorTerms) != 1 {
		t.Fatalf("unexpected node affinity: %#v", affinity)
	}
	expressions := affinity.Required.NodeSelectorTerms[0].MatchExpressions
	if len(expressions) != 1 {
		t.Fatalf("unexpected node affinity expressions: %#v", expressions)
	}
	expression := expressions[0]
	if expression.Key != "kubernetes.io/hostname" || expression.Operator != corev1.NodeSelectorOpIn {
		t.Fatalf("unexpected node affinity expression: %#v", expression)
	}
	if len(expression.Values) != 1 || expression.Values[0] != hostname {
		t.Fatalf("expected hostname %q, got %#v", hostname, expression.Values)
	}
}
