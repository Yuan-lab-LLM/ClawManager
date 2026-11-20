package k8s

import (
	"context"
	"errors"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	kt "k8s.io/client-go/testing"
	"strings"
	"testing"
)

type fenceAuthorityFunc func(context.Context, ReceiptWriterPin) (string, error)

func (f fenceAuthorityFunc) ValidateWriterFence(c context.Context, p ReceiptWriterPin) (string, error) {
	return f(c, p)
}
func isolationFixture() (ReceiptWriterPin, []runtime.Object) {
	p := ReceiptWriterPin{InstanceID: 8, OwnerID: 7, Generation: 3, Operation: strings.Repeat("a", 32), Role: "pro", Namespace: "instances", Deployment: "desktop", DeploymentUID: "deployment-uid", PVC: "receipts", PVCUID: "claim-uid", PV: "pv-receipts", PVUID: "pv-uid", OldPodUID: "old-pod-uid", OldNode: "node1", OldNodeUID: "node-uid"}
	zero := int32(0)
	d := &appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: p.Deployment, Namespace: p.Namespace, UID: "deployment-uid", Generation: 5, ResourceVersion: "10"}, Spec: appsv1.DeploymentSpec{Replicas: &zero}, Status: appsv1.DeploymentStatus{ObservedGeneration: 5}}
	c := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: p.PVC, Namespace: p.Namespace, UID: "claim-uid", ResourceVersion: "11", Labels: map[string]string{"clawmanager.io/storage-purpose": "task-receipts", "instance-id": "8", "user-id": "7"}}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: p.PV, AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOncePod}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	v := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: p.PV, UID: "pv-uid", ResourceVersion: "12"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Name: p.PVC, Namespace: p.Namespace, UID: "claim-uid"}}, Status: corev1.PersistentVolumeStatus{Phase: corev1.VolumeBound}}
	n := &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: p.OldNode, UID: "node-uid"}}
	return p, []runtime.Object{d, c, v, n}
}
func trustedFence(context.Context, ReceiptWriterPin) (string, error) {
	return "server-validated-epoch", nil
}
func TestReceiptIsolationRequiresIndependentAuthorityAndNeverMutates(t *testing.T) {
	p, objects := isolationFixture()
	c := fake.NewSimpleClientset(objects...)
	if _, e := NewReceiptWriterIsolationInspector(c, nil).Inspect(context.Background(), p); e != ErrReceiptIsolationUnproven {
		t.Fatal(e)
	}
	r, e := NewReceiptWriterIsolationInspector(c, fenceAuthorityFunc(trustedFence)).Inspect(context.Background(), p)
	if e != nil || !r.InfrastructureObservedClear || !r.IsolationAuthorityChecked || r.LifecycleAuthorized {
		t.Fatalf("%+v %v", r, e)
	}
	for _, a := range c.Actions() {
		if a.GetVerb() != "get" && a.GetVerb() != "list" {
			t.Fatalf("mutation: %+v", a)
		}
	}
}
func TestReceiptIsolationRejectsUnsafePlacementStorageAndConsumers(t *testing.T) {
	for _, name := range []string{"deployment-uid", "not-observed", "replicas", "pvc-uid", "shared-volume", "pv-claim", "node-replaced", "old-pod", "pending-consumer", "terminated-consumer", "volume-attachment", "hpa"} {
		t.Run(name, func(t *testing.T) {
			p, objects := isolationFixture()
			d := objects[0].(*appsv1.Deployment)
			claim := objects[1].(*corev1.PersistentVolumeClaim)
			v := objects[2].(*corev1.PersistentVolume)
			switch name {
			case "deployment-uid":
				d.UID = "replacement"
			case "not-observed":
				d.Status.ObservedGeneration = 4
			case "replicas":
				one := int32(1)
				d.Spec.Replicas = &one
			case "pvc-uid":
				claim.UID = "replacement"
			case "shared-volume":
				claim.Spec.AccessModes = []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}
			case "pv-claim":
				v.Spec.ClaimRef.UID = "replacement"
			case "node-replaced":
				objects[3].(*corev1.Node).UID = "new-node"
			case "old-pod":
				objects = append(objects, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "old", Namespace: p.Namespace, UID: "old-pod-uid"}})
			case "pending-consumer", "terminated-consumer":
				phase := corev1.PodPending
				if name == "terminated-consumer" {
					phase = corev1.PodSucceeded
				}
				objects = append(objects, &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "consumer", Namespace: p.Namespace}, Spec: corev1.PodSpec{Volumes: []corev1.Volume{{Name: "r", VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: p.PVC}}}}}, Status: corev1.PodStatus{Phase: phase}})
			case "volume-attachment":
				objects = append(objects, &storagev1.VolumeAttachment{ObjectMeta: metav1.ObjectMeta{Name: "attachment"}, Spec: storagev1.VolumeAttachmentSpec{Source: storagev1.VolumeAttachmentSource{PersistentVolumeName: &p.PV}}})
			case "hpa":
				objects = append(objects, &autoscalingv2.HorizontalPodAutoscaler{ObjectMeta: metav1.ObjectMeta{Name: "autoscaler", Namespace: p.Namespace}, Spec: autoscalingv2.HorizontalPodAutoscalerSpec{ScaleTargetRef: autoscalingv2.CrossVersionObjectReference{Kind: "Deployment", Name: p.Deployment}}})
			}
			if r, e := NewReceiptWriterIsolationInspector(fake.NewSimpleClientset(objects...), fenceAuthorityFunc(trustedFence)).Inspect(context.Background(), p); r != nil || e != ErrReceiptIsolationUnproven {
				t.Fatalf("%+v %v", r, e)
			}
		})
	}
}
func TestReceiptIsolationAuthorityAndAPIErrorsRemainUnproven(t *testing.T) {
	p, objects := isolationFixture()
	c := fake.NewSimpleClientset(objects...)
	calls := 0
	a := fenceAuthorityFunc(func(context.Context, ReceiptWriterPin) (string, error) {
		calls++
		if calls > 1 {
			return "new-epoch", nil
		}
		return "old-epoch", nil
	})
	if _, e := NewReceiptWriterIsolationInspector(c, a).Inspect(context.Background(), p); e != ErrReceiptIsolationUnproven {
		t.Fatal(e)
	}
	c.PrependReactor("list", "pods", func(kt.Action) (bool, runtime.Object, error) { return true, nil, errors.New("PRIVATE-API-DETAIL") })
	if _, e := NewReceiptWriterIsolationInspector(c, fenceAuthorityFunc(trustedFence)).Inspect(context.Background(), p); e != ErrReceiptIsolationUnproven {
		t.Fatal(e)
	}
}
func TestReceiptIsolationLiteRequiresOriginalPoolBinding(t *testing.T) {
	p, objects := isolationFixture()
	p.Role = "lite"
	claim := objects[1].(*corev1.PersistentVolumeClaim)
	claim.Annotations = map[string]string{"clawmanager.io/runtime-deployment": p.Deployment}
	if _, err := NewReceiptWriterIsolationInspector(fake.NewSimpleClientset(objects...), fenceAuthorityFunc(trustedFence)).Inspect(context.Background(), p); err != nil {
		t.Fatal(err)
	}
	claim.Annotations["clawmanager.io/runtime-deployment"] = "other-pool"
	if _, err := NewReceiptWriterIsolationInspector(fake.NewSimpleClientset(objects...), fenceAuthorityFunc(trustedFence)).Inspect(context.Background(), p); err != ErrReceiptIsolationUnproven {
		t.Fatal(err)
	}
}
func TestReceiptIsolationResourceVersionChangesDuringScanAreRejected(t *testing.T) {
	p, objects := isolationFixture()
	c := fake.NewSimpleClientset(objects...)
	reads := 0
	c.PrependReactor("get", "deployments", func(kt.Action) (bool, runtime.Object, error) {
		reads++
		value := objects[0].(*appsv1.Deployment).DeepCopy()
		if reads > 1 {
			value.ResourceVersion = "changed-during-inspection"
		}
		return true, value, nil
	})
	if r, err := NewReceiptWriterIsolationInspector(c, fenceAuthorityFunc(trustedFence)).Inspect(context.Background(), p); r != nil || err != ErrReceiptIsolationUnproven {
		t.Fatalf("%+v %v", r, err)
	}
}
