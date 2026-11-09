package systembackup

import (
	"context"
	"errors"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
)

type LocalStageVolumes struct {
	Client         kubernetes.Interface
	MountDirectory string
}

func stageLabels(v StageVolume) map[string]string {
	return map[string]string{"backup.clawmanager.io/resource": v.ID, "backup.clawmanager.io/task": v.TaskID, "backup.clawmanager.io/role": "stage"}
}
func stagePVC(v StageVolume) *corev1.PersistentVolumeClaim {
	mode := corev1.PersistentVolumeFilesystem
	return &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: v.Name(), Namespace: v.Request.Namespace, Labels: stageLabels(v), Annotations: map[string]string{"backup.clawmanager.io/request-sha256": v.Hash}}, Spec: corev1.PersistentVolumeClaimSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: &v.Request.StorageClass, VolumeName: v.Name(), VolumeMode: &mode, Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{corev1.ResourceStorage: *resource.NewQuantity(v.Request.CapacityBytes, resource.BinarySI)}}}}
}
func stagePV(v StageVolume) *corev1.PersistentVolume {
	mode := corev1.PersistentVolumeFilesystem
	hostType := corev1.HostPathDirectory
	return &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: v.Name(), Labels: stageLabels(v), Annotations: map[string]string{"backup.clawmanager.io/request-sha256": v.Hash}}, Spec: corev1.PersistentVolumeSpec{AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce}, StorageClassName: v.Request.StorageClass, VolumeMode: &mode, Capacity: corev1.ResourceList{corev1.ResourceStorage: *resource.NewQuantity(v.Request.CapacityBytes, resource.BinarySI)}, PersistentVolumeReclaimPolicy: corev1.PersistentVolumeReclaimRetain, PersistentVolumeSource: corev1.PersistentVolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: v.HostPath(), Type: &hostType}}, ClaimRef: &corev1.ObjectReference{APIVersion: "v1", Kind: "PersistentVolumeClaim", Name: v.Name(), Namespace: v.Request.Namespace, UID: types.UID(v.PVCUID)}, NodeAffinity: &corev1.VolumeNodeAffinity{Required: &corev1.NodeSelector{NodeSelectorTerms: []corev1.NodeSelectorTerm{{MatchFields: []corev1.NodeSelectorRequirement{{Key: "metadata.name", Operator: corev1.NodeSelectorOpIn, Values: []string{v.Request.NodeName}}}}}}}}}
}
func stageMetadata(want, got metav1.Object) bool {
	if want.GetName() != got.GetName() || want.GetNamespace() != got.GetNamespace() || got.GetDeletionTimestamp() != nil || got.GetAnnotations()["backup.clawmanager.io/request-sha256"] != want.GetAnnotations()["backup.clawmanager.io/request-sha256"] {
		return false
	}
	for k, v := range want.GetLabels() {
		if got.GetLabels()[k] != v {
			return false
		}
	}
	return true
}
func matchesStagePVC(v StageVolume, p *corev1.PersistentVolumeClaim) bool {
	w := stagePVC(v)
	if !stageMetadata(w, p) || p.Spec.StorageClassName == nil || *p.Spec.StorageClassName != v.Request.StorageClass || p.Spec.VolumeName != v.Name() || p.Spec.VolumeMode != nil && *p.Spec.VolumeMode != corev1.PersistentVolumeFilesystem || p.Spec.DataSource != nil || p.Spec.DataSourceRef != nil || p.Spec.Selector != nil || !reflect.DeepEqual(p.Spec.AccessModes, w.Spec.AccessModes) {
		return false
	}
	q := p.Spec.Resources.Requests[corev1.ResourceStorage]
	return q.Value() == v.Request.CapacityBytes
}
func matchesStagePV(v StageVolume, p *corev1.PersistentVolume) bool {
	w := stagePV(v)
	return stageMetadata(w, p) && p.Spec.StorageClassName == w.Spec.StorageClassName && p.Spec.VolumeMode != nil && *p.Spec.VolumeMode == corev1.PersistentVolumeFilesystem && reflect.DeepEqual(p.Spec.PersistentVolumeSource, w.Spec.PersistentVolumeSource) && reflect.DeepEqual(p.Spec.NodeAffinity, w.Spec.NodeAffinity) && reflect.DeepEqual(p.Spec.AccessModes, w.Spec.AccessModes) && p.Spec.PersistentVolumeReclaimPolicy == corev1.PersistentVolumeReclaimRetain && p.Spec.Capacity.Storage().Value() == v.Request.CapacityBytes && p.Spec.ClaimRef != nil && p.Spec.ClaimRef.Name == v.Name() && p.Spec.ClaimRef.Namespace == v.Request.Namespace && string(p.Spec.ClaimRef.UID) == v.PVCUID && len(p.Spec.MountOptions) == 0
}
func (d LocalStageVolumes) namespace(ctx context.Context, v StageVolume) error {
	if d.Client == nil {
		return errors.New("not_ready")
	}
	n, err := d.Client.CoreV1().Namespaces().Get(ctx, v.Request.Namespace, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if string(n.UID) != v.Request.NamespaceUID || n.DeletionTimestamp != nil {
		return errors.New("isolation_invalid")
	}
	return nil
}
func (d LocalStageVolumes) Ensure(ctx context.Context, s Store, l Leadership, t Task, v StageVolume) (StageVolume, bool, error) {
	actual, err := s.StageVolume(ctx, t.ID, v.Request.Phase)
	if err != nil {
		return v, false, err
	}
	if actual.ID != v.ID || actual.Hash != v.Hash {
		return v, false, errors.New("artifact_conflict")
	}
	v = actual
	if err := d.namespace(ctx, v); err != nil {
		return v, false, err
	}
	if v.State != "prepared" && v.State != "ready" {
		return v, false, errors.New("state_conflict")
	}
	if err := s.withStageDirectoryFence(ctx, l, t, v, func() error { return PrepareStageDirectory(d.MountDirectory, s.InstallationID, v) }); err != nil {
		return v, false, err
	}
	pvc, err := d.Client.CoreV1().PersistentVolumeClaims(v.Request.Namespace).Get(ctx, v.Name(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) && v.PVCUID == "" && v.State == "prepared" {
		if err = s.updateStageVolume(ctx, l, t, v, "pvc_sent", ""); err != nil {
			return v, false, err
		}
		pvc, err = d.Client.CoreV1().PersistentVolumeClaims(v.Request.Namespace).Create(ctx, stagePVC(v), metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			pvc, err = d.Client.CoreV1().PersistentVolumeClaims(v.Request.Namespace).Get(ctx, v.Name(), metav1.GetOptions{})
		}
		if err != nil {
			return v, false, unknownBackupExecution(err)
		}
	}
	if err != nil {
		return v, false, err
	}
	if !matchesStagePVC(v, pvc) || pvc.UID == "" || v.PVCUID != "" && v.PVCUID != string(pvc.UID) {
		return v, false, errors.New("artifact_conflict")
	}
	if v.PVCUID == "" {
		if err = s.updateStageVolume(ctx, l, t, v, "pvc_uid", string(pvc.UID)); err != nil {
			return v, false, err
		}
		v.PVCUID = string(pvc.UID)
	}
	pv, err := d.Client.CoreV1().PersistentVolumes().Get(ctx, v.Name(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) && v.PVUID == "" && v.State == "prepared" {
		if err = s.updateStageVolume(ctx, l, t, v, "pv_sent", ""); err != nil {
			return v, false, err
		}
		pv, err = d.Client.CoreV1().PersistentVolumes().Create(ctx, stagePV(v), metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			pv, err = d.Client.CoreV1().PersistentVolumes().Get(ctx, v.Name(), metav1.GetOptions{})
		}
		if err != nil {
			return v, false, unknownBackupExecution(err)
		}
	}
	if err != nil {
		return v, false, err
	}
	if !matchesStagePV(v, pv) || pv.UID == "" || v.PVUID != "" && v.PVUID != string(pv.UID) {
		return v, false, errors.New("artifact_conflict")
	}
	if v.PVUID == "" {
		if err = s.updateStageVolume(ctx, l, t, v, "pv_uid", string(pv.UID)); err != nil {
			return v, false, err
		}
		v.PVUID = string(pv.UID)
	}
	if pvc.Status.Phase != corev1.ClaimBound || pv.Status.Phase != corev1.VolumeBound {
		return v, false, nil
	}
	if err = s.updateStageVolume(ctx, l, t, v, "ready", ""); err != nil {
		return v, false, err
	}
	v.State = "ready"
	return v, true, nil
}

func (d LocalStageVolumes) Cleanup(ctx context.Context, s Store, l Leadership, t Task, v StageVolume) (bool, error) {
	actual, err := s.StageVolume(ctx, t.ID, v.Request.Phase)
	if err != nil {
		return false, err
	}
	if actual.ID != v.ID || actual.Hash != v.Hash {
		return false, errors.New("artifact_conflict")
	}
	v = actual
	if err := d.namespace(ctx, v); err != nil {
		return false, err
	}
	if v.State == "deleted" {
		return true, nil
	}
	if err := s.updateStageVolume(ctx, l, t, v, "delete_requested", ""); err != nil {
		return false, err
	}
	pods, err := d.Client.CoreV1().Pods(v.Request.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return false, err
	}
	for _, pod := range pods.Items {
		for _, volume := range pod.Spec.Volumes {
			if volume.PersistentVolumeClaim != nil && volume.PersistentVolumeClaim.ClaimName == v.Name() {
				return false, nil
			}
		}
	}
	pvc, err := d.Client.CoreV1().PersistentVolumeClaims(v.Request.Namespace).Get(ctx, v.Name(), metav1.GetOptions{})
	if err == nil {
		if !matchesStagePVCForDelete(v, pvc) {
			return false, errors.New("cleanup_unsafe_target")
		}
		if v.PVCUID == "" {
			if err = s.updateStageVolume(ctx, l, t, v, "pvc_uid", string(pvc.UID)); err != nil {
				return false, err
			}
			return false, nil
		}
		uid := types.UID(v.PVCUID)
		if pvc.DeletionTimestamp != nil {
			return false, nil
		}
		err = d.Client.CoreV1().PersistentVolumeClaims(v.Request.Namespace).Delete(ctx, v.Name(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		return false, err
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	if v.PVCUID == "" && v.PVCCreateSent {
		return false, nil
	}
	pv, err := d.Client.CoreV1().PersistentVolumes().Get(ctx, v.Name(), metav1.GetOptions{})
	if err == nil {
		if !matchesStagePVForDelete(v, pv) {
			return false, errors.New("cleanup_unsafe_target")
		}
		if v.PVUID == "" {
			if err = s.updateStageVolume(ctx, l, t, v, "pv_uid", string(pv.UID)); err != nil {
				return false, err
			}
			return false, nil
		}
		uid := types.UID(v.PVUID)
		if pv.DeletionTimestamp != nil {
			return false, nil
		}
		err = d.Client.CoreV1().PersistentVolumes().Delete(ctx, v.Name(), metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &uid}})
		return false, err
	}
	if !apierrors.IsNotFound(err) {
		return false, err
	}
	if v.PVUID == "" && v.PVCreateSent {
		return false, nil
	}
	if err = RemoveStageDirectory(d.MountDirectory, s.InstallationID, v); err != nil {
		return false, err
	}
	if err = s.updateStageVolume(ctx, l, t, v, "deleted", ""); err != nil {
		return false, err
	}
	return true, nil
}
func matchesStagePVCForDelete(v StageVolume, p *corev1.PersistentVolumeClaim) bool {
	copy := p.DeepCopy()
	copy.DeletionTimestamp = nil
	return matchesStagePVC(v, copy) && p.UID != "" && (v.PVCUID == "" || string(p.UID) == v.PVCUID)
}
func matchesStagePVForDelete(v StageVolume, p *corev1.PersistentVolume) bool {
	copy := p.DeepCopy()
	copy.DeletionTimestamp = nil
	return matchesStagePV(v, copy) && p.UID != "" && (v.PVUID == "" || string(p.UID) == v.PVUID)
}
