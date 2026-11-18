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
	ktesting "k8s.io/client-go/testing"
)

func TestRestoreWorkspaceAutomaticallyProvisionsOwnedVolumeWithoutOverwritingPartialData(t *testing.T) {
	if runtime.GOOS != "windows" && os.Geteuid() != 0 {
		t.Skip("single-node directory provider requires Unix root")
	}
	ctx := context.Background()
	s := Store{controlDB(t), "restore-workspace"}
	if err := s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig); err != nil {
		t.Fatal(err)
	}
	x, _, err := s.Create(ctx, 1, "restore-workspace-0001", CreateTaskRequest{Type: Drill, Purpose: "functional_test", SourceID: "sbk_" + strings.Repeat("a", 32)}, permitTask)
	if err != nil {
		t.Fatal(err)
	}
	l, err := s.AcquireLeadership(ctx, "restore-workspace-controller", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	x, err = s.ClaimLedTask(ctx, l, x.ID, x.Version, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"preflighting", "provisioning"} {
		x, err = s.Advance(ctx, x, l.Owner, phase, "none")
		if err != nil {
			t.Fatal(err)
		}
	}
	r := StageVolumeRequest{Phase: "restore", Namespace: "owned-drill-target", NamespaceUID: "target-namespace-uid", SourceNamespaceUID: "source-namespace-uid", NodeName: "owned-node", HostRoot: "/owned/restore-target", StorageClass: "manual", CapacityBytes: 8 << 20}
	client := fake.NewSimpleClientset(&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: r.Namespace, UID: types.UID(r.NamespaceUID)}})
	created := 0
	client.PrependReactor("create", "persistentvolumeclaims", func(action ktesting.Action) (bool, k8sruntime.Object, error) {
		created++
		p := action.(ktesting.CreateAction).GetObject().(*corev1.PersistentVolumeClaim)
		p.UID = "owned-restore-pvc"
		p.Status.Phase = corev1.ClaimBound
		return false, nil, nil
	})
	client.PrependReactor("create", "persistentvolumes", func(action ktesting.Action) (bool, k8sruntime.Object, error) {
		created++
		p := action.(ktesting.CreateAction).GetObject().(*corev1.PersistentVolume)
		p.UID = "owned-restore-pv"
		p.Status.Phase = corev1.VolumeBound
		return false, nil, nil
	})
	d := LocalStageVolumes{Client: client, MountDirectory: t.TempDir()}
	bad := r
	bad.SourceNamespaceUID = r.NamespaceUID
	if _, _, err = d.PrepareRestoreWorkspace(ctx, s, l, x, bad); err == nil || created != 0 {
		t.Fatal("source namespace reused as restore target", err)
	}
	v, ready, err := d.PrepareRestoreWorkspace(ctx, s, l, x, r)
	if err != nil || !ready || created != 2 || v.PVCUID != "owned-restore-pvc" {
		t.Fatal("restore workspace not provisioned", v, ready, err)
	}
	again, ready, err := d.PrepareRestoreWorkspace(ctx, s, l, x, r)
	if err != nil || !ready || again.ID != v.ID || created != 2 {
		t.Fatal("restore workspace was not adopted", again, ready, err)
	}
	data := filepath.Join(d.MountDirectory, v.ID, "data")
	if entries, err := os.ReadDir(data); err != nil || len(entries) != 0 {
		t.Fatal("restore target directory is not fresh", err)
	}
	partial := filepath.Join(data, "partial-result.bin")
	if err = os.WriteFile(partial, []byte("keep partial restore until explicit cleanup"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, _, err = d.PrepareRestoreWorkspace(ctx, s, l, x, r); err == nil {
		t.Fatal("partial target was silently reset")
	}
	if b, err := os.ReadFile(partial); err != nil || string(b) != "keep partial restore until explicit cleanup" {
		t.Fatal("partial restore data lost", err)
	}
	foreign := filepath.Join(d.MountDirectory, "unrelated")
	if err = os.WriteFile(foreign, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "restore-writer", Namespace: r.Namespace}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "target", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: v.Name()}}}}}}
	if _, err = client.CoreV1().Pods(r.Namespace).Create(ctx, pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if done, err := d.Cleanup(ctx, s, l, x, v); err != nil || done {
		t.Fatal("active restore writer lost its volume", done, err)
	}
	if err = client.CoreV1().Pods(r.Namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 4; i++ {
		done, err := d.Cleanup(ctx, s, l, x, v)
		if err != nil {
			t.Fatal(err)
		}
		if done {
			break
		}
		if i == 3 {
			t.Fatal("owned restore workspace was not cleaned")
		}
	}
	if _, err = os.Stat(filepath.Join(d.MountDirectory, v.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("owned target retained", err)
	}
	if b, err := os.ReadFile(foreign); err != nil || string(b) != "keep" {
		t.Fatal("unrelated data removed", err)
	}
	if _, _, err = d.PrepareRestoreWorkspace(ctx, s, l, x, r); err == nil {
		t.Fatal("deleted restore target resurrected")
	}
}
