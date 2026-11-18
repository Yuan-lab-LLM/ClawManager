package services

import (
	"clawreef/internal/models"
	"clawreef/internal/runtimeeviction"
	"clawreef/internal/services/k8s"
	"context"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"strconv"
)

// A deleted controller need not prevent retiring an already stopped/deleting
// instance. The protected ledger supplies exact execution identity; the trusted
// management PVC supplies only the ledger mount, never restart provenance.
func retiredFaultSourcePod(ctx context.Context, c *slotRuntimeAgentClient, source models.RuntimePod, instance models.Instance, binding models.InstanceRuntimeBinding, root string) (*corev1.Pod, error) {
	if instance.Status != "stopped" && instance.Status != "deleting" {
		return nil, fmt.Errorf("missing historical template cannot authorize restart placement")
	}
	observed, observeErr := c.kube.CoreV1().Pods(source.Namespace).Get(ctx, source.PodName, metav1.GetOptions{})
	if observeErr == nil && string(observed.UID) == stringValue(source.PodUID) {
		return nil, fmt.Errorf("retirement fallback cannot bypass a live source identity conflict")
	}
	if observeErr != nil && !apierrors.IsNotFound(observeErr) {
		return nil, observeErr
	}
	slot, err := runtimeeviction.InspectSourceLedger(root, instance.ID, instance.UserID, binding.Generation, instance.Type, stringValue(source.PodUID))
	if err != nil {
		return nil, fmt.Errorf("retained source ledger cannot establish old execution identity: %w", err)
	}
	node, err := k8s.RuntimeFaultHistoricalNode(ctx, c.kube, source)
	if err != nil {
		return nil, err
	}
	deployment, err := c.kube.AppsV1().Deployments(source.Namespace).Get(ctx, "clawmanager-app", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	var mount *corev1.VolumeMount
	var volume *corev1.Volume
	for _, container := range deployment.Spec.Template.Spec.Containers {
		if container.Name != "clawmanager-app" {
			continue
		}
		for _, m := range container.VolumeMounts {
			if m.Name == "workspaces" && m.MountPath == root && !m.ReadOnly && m.SubPathExpr == "" {
				copy := m
				mount = &copy
			}
		}
	}
	for _, v := range deployment.Spec.Template.Spec.Volumes {
		if v.Name == "workspaces" && v.PersistentVolumeClaim != nil && !v.PersistentVolumeClaim.ReadOnly {
			copy := v
			volume = &copy
		}
	}
	if mount == nil || volume == nil {
		return nil, fmt.Errorf("management workspace PVC cannot be verified for source retirement")
	}
	return &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: source.Namespace, Name: source.PodName, UID: types.UID(*source.PodUID), Annotations: map[string]string{"clawmanager.io/reconstructed-source": "true", "clawmanager.io/retirement-ledger-source": "true"}}, Spec: corev1.PodSpec{NodeName: node.Name, Volumes: []corev1.Volume{*volume}, Containers: []corev1.Container{{Name: slot, Image: source.ImageRef, Env: []corev1.EnvVar{{Name: "RUNTIME_WORKSPACE_ROOT", Value: root}, {Name: "RUNTIME_GATEWAY_PORT_START", Value: strconv.Itoa(binding.GatewayPort)}}, VolumeMounts: []corev1.VolumeMount{*mount}}}}}, nil
}
