package systembackup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
)

func TestStageDirectoryRequiresExactOwnerAndPreservesOtherData(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		t.Skip("single-node directory provider requires Unix root for collector ownership")
	}
	root := t.TempDir()
	id, _ := RandomID("srs_")
	task, _ := RandomID("sbk_")
	v := StageVolume{ID: id, TaskID: task, Hash: strings.Repeat("a", 64)}
	if err := PrepareStageDirectory(root, "installation", v); err != nil {
		t.Fatal(err)
	}
	if err := PrepareStageDirectory(root, "installation", v); err != nil {
		t.Fatal("lost response could not adopt own directory", err)
	}
	other := filepath.Join(root, "unrelated")
	if err := os.WriteFile(other, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, v.ID, "private.enc"), []byte("encrypted fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := RemoveStageDirectory(root, "other-installation", v); PublicError(err) != "cleanup_unsafe_target" {
		t.Fatal("foreign origin deleted", err)
	}
	if err := RemoveStageDirectory(root, "installation", v); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, v.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	if raw, err := os.ReadFile(other); err != nil || string(raw) != "keep" {
		t.Fatal("unrelated data changed")
	}
	if err := os.Mkdir(filepath.Join(root, v.ID), 0700); err != nil {
		t.Fatal(err)
	}
	if err := RemoveStageDirectory(root, "installation", v); PublicError(err) != "cleanup_unsafe_target" {
		t.Fatal("unmarked directory deleted", err)
	}
}

func TestStageResourcesAdoptOriginalUIDsAndWaitForUnmountBeforeCleanup(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		t.Skip("single-node directory provider requires Unix root for collector ownership")
	}
	ctx := context.Background()
	s := Store{controlDB(t), "stage-installation"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	l, err := s.AcquireLeadership(ctx, "stage-leader", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	x, _, err := s.Create(ctx, 1, "stage-fixture-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.ClaimLedTask(ctx, l, x.ID, x.Version, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.Advance(ctx, x, l.Owner, "preparing", "none")
	if err != nil {
		t.Fatal(err)
	}
	request := StageVolumeRequest{Phase: "capture", Namespace: "owned-backup", NamespaceUID: "namespace-uid", NodeName: "owned-node", HostRoot: "/owned/backup-stage", StorageClass: "manual", CapacityBytes: 8 << 20}
	v, err := s.PrepareStageVolume(ctx, l, x, request)
	if err != nil {
		t.Fatal(err)
	}
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: request.Namespace, UID: types.UID(request.NamespaceUID)}})
	client.PrependReactor("create", "persistentvolumeclaims", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		p := action.(k8stesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim)
		p.UID = "original-pvc"
		p.Status.Phase = corev1.ClaimBound
		return false, nil, nil
	})
	client.PrependReactor("create", "persistentvolumes", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
		p := action.(k8stesting.CreateAction).GetObject().(*corev1.PersistentVolume)
		p.UID = "original-pv"
		p.Status.Phase = corev1.VolumeBound
		return false, nil, nil
	})
	d := LocalStageVolumes{Client: client, MountDirectory: t.TempDir()}
	v, ready, err := d.Ensure(ctx, s, l, x, v)
	if err != nil || !ready {
		t.Fatal(v, ready, err)
	}
	v, err = s.StageVolume(ctx, x.ID, "capture")
	if err != nil || v.PVCUID != "original-pvc" || v.PVUID != "original-pv" || v.State != "ready" {
		t.Fatal(v, err)
	}
	if _, ready, err = d.Ensure(ctx, s, l, x, v); err != nil || !ready {
		t.Fatal("retry did not adopt original resources", err)
	}
	readyBeforeCleanup := v
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "still-mounted", Namespace: request.Namespace}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "stage", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: v.Name()}}}}}}
	if _, err = client.CoreV1().Pods(request.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if done, err := d.Cleanup(ctx, s, l, x, v); err != nil || done {
		t.Fatal("mounted volume removed", done, err)
	}
	if err = client.CoreV1().Pods(request.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		v, err = s.StageVolume(ctx, x.ID, "capture")
		if err != nil {
			t.Fatal(err)
		}
		done, e := d.Cleanup(ctx, s, l, x, v)
		if e != nil {
			t.Fatal(e)
		}
		if done {
			break
		}
		if i == 3 {
			t.Fatal("owned resources did not clean up")
		}
	}
	if _, err = os.Stat(filepath.Join(d.MountDirectory, v.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned directory retained", err)
	}
	if _, _, err = d.Ensure(ctx, s, l, x, readyBeforeCleanup); err == nil {
		t.Fatal("stale owner recreated deleted stage")
	}
	for _, action := range client.Actions() {
		if action.GetVerb() == "delete" {
			options := action.(k8stesting.DeleteAction).GetDeleteOptions()
			if action.GetResource().Resource != "pods" && (options.Preconditions == nil || options.Preconditions.UID == nil) {
				t.Fatal("delete lacked UID precondition")
			}
		}
	}
}

func TestStageUnknownCreatesAreNeitherRepeatedNorCleanedUsingStaleState(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		t.Skip("single-node directory provider requires Unix root")
	}
	for _, resource := range []string{"persistentvolumeclaims", "persistentvolumes"} {
		for _, scenario := range []string{"retry", "cleanup"} {
			t.Run(resource+"/"+scenario, func(t *testing.T) {
				ctx := context.Background()
				s := Store{controlDB(t), "unknown-stage-create"}
				if err := s.Bootstrap(ctx); err != nil {
					t.Fatal(err)
				}
				if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
					t.Fatal(err)
				}
				l, err := s.AcquireLeadership(ctx, "unknown-stage-controller", time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				x, _, err := s.Create(ctx, 1, "unknown-stage-0001", CreateTaskRequest{Type: Backup, Purpose: "functional_test"}, permitTask)
				if err != nil {
					t.Fatal(err)
				}
				x, err = s.ClaimLedTask(ctx, l, x.ID, x.Version, time.Minute)
				if err != nil {
					t.Fatal(err)
				}
				x, err = s.Advance(ctx, x, l.Owner, "preparing", "none")
				if err != nil {
					t.Fatal(err)
				}
				r := StageVolumeRequest{Phase: "capture", Namespace: "owned-stage", NamespaceUID: "owned-namespace", NodeName: "owned-node", HostRoot: "/owned/stage", StorageClass: "manual", CapacityBytes: 8 << 20}
				v, err := s.PrepareStageVolume(ctx, l, x, r)
				if err != nil {
					t.Fatal(err)
				}
				client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, UID: types.UID(r.NamespaceUID)}})
				client.PrependReactor("create", "persistentvolumeclaims", func(action k8stesting.Action) (bool, k8sruntime.Object, error) {
					p := action.(k8stesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim)
					p.UID = "original-pvc"
					p.Status.Phase = corev1.ClaimBound
					return false, nil, nil
				})
				creates := 0
				client.PrependReactor("create", resource, func(k8stesting.Action) (bool, k8sruntime.Object, error) {
					creates++
					return true, nil, errors.New("create response unavailable")
				})
				d := LocalStageVolumes{Client: client, MountDirectory: t.TempDir()}
				if _, _, err = d.Ensure(ctx, s, l, x, v); err == nil {
					t.Fatal("fixture did not lose Create response")
				}
				if scenario == "retry" {
					if _, _, err = d.Ensure(ctx, s, l, x, v); err == nil {
						t.Fatal("unknown create was considered safe")
					}
				}
				if creates != 1 {
					t.Fatal("stale resource state repeated unknown Create", creates)
				}
				for i := 0; i < 2; i++ {
					if done, err := d.Cleanup(ctx, s, l, x, v); err != nil || done {
						t.Fatal("stale unsent flag claimed unknown resource cleanup", done, err)
					}
				}
				if _, err = os.Stat(filepath.Join(d.MountDirectory, v.ID)); err != nil {
					t.Fatal("unknown resource ownership directory removed", err)
				}
			})
		}
	}
}
