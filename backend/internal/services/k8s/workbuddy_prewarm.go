package k8s

import (
	"context"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
)

const (
	workbuddyPrewarmPoolSizeEnv = "CLAWMANAGER_WORKBUDDY_PREWARM_POOL_SIZE"
	workbuddyPrewarmImageEnv    = "CLAWMANAGER_WORKBUDDY_PREWARM_IMAGE"
	workbuddyGoldenPVCNameEnv   = "CLAWMANAGER_WORKBUDDY_GOLDEN_PVC"
	workbuddyPrewarmIntervalEnv = "CLAWMANAGER_WORKBUDDY_PREWARM_INTERVAL_SECONDS"

	workbuddyPrewarmLabel       = "clawmanager.io/workbuddy-prewarm"
	workbuddyPrewarmStateLabel  = "clawmanager.io/prewarm-state"
	workbuddyPrewarmGoldenLabel = "clawmanager.io/golden-pvc"
	workbuddyPrewarmPVCLabel    = "clawmanager.io/prewarm-pvc"
	workbuddyPrewarmStateWarm   = "warming"
	workbuddyPrewarmStateReady  = "ready"
	workbuddyPrewarmStateClaim  = "claimed"
	workbuddyPrewarmMarker      = "/storage/.clawmanager-prewarmed"
	defaultPrewarmInterval      = 30 * time.Second
)

// WorkbuddyPrewarmController keeps cloned Windows disks attached and fully
// read on the Windows runtime node before users request an instance.
type WorkbuddyPrewarmController struct {
	client    *Client
	poolSize  int
	goldenPVC string
	image     string
	interval  time.Duration
}

func NewWorkbuddyPrewarmController() *WorkbuddyPrewarmController {
	poolSize, _ := strconv.Atoi(strings.TrimSpace(os.Getenv(workbuddyPrewarmPoolSizeEnv)))
	if poolSize < 0 {
		poolSize = 0
	}
	if poolSize > 20 {
		poolSize = 20
	}
	interval := defaultPrewarmInterval
	if seconds, err := strconv.Atoi(strings.TrimSpace(os.Getenv(workbuddyPrewarmIntervalEnv))); err == nil && seconds > 0 {
		interval = time.Duration(seconds) * time.Second
	}
	return &WorkbuddyPrewarmController{
		client:    globalClient,
		poolSize:  poolSize,
		goldenPVC: strings.TrimSpace(os.Getenv(workbuddyGoldenPVCNameEnv)),
		image:     strings.TrimSpace(os.Getenv(workbuddyPrewarmImageEnv)),
		interval:  interval,
	}
}

func (c *WorkbuddyPrewarmController) Enabled() bool {
	return c != nil && c.client != nil && c.client.Clientset != nil && c.poolSize > 0 && c.goldenPVC != "" && c.image != ""
}

func (c *WorkbuddyPrewarmController) Run(ctx context.Context) {
	if !c.Enabled() {
		log.Printf("WorkBuddy prewarm pool disabled (size=%d, golden_pvc=%t, image=%t)", c.poolSize, c.goldenPVC != "", c.image != "")
		return
	}
	log.Printf("WorkBuddy prewarm pool started (size=%d, interval=%s)", c.poolSize, c.interval)
	c.reconcileAndLog(ctx)
	ticker := time.NewTicker(c.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.reconcileAndLog(ctx)
		}
	}
}

func (c *WorkbuddyPrewarmController) reconcileAndLog(ctx context.Context) {
	if err := c.Reconcile(ctx); err != nil && ctx.Err() == nil {
		log.Printf("WorkBuddy prewarm reconcile failed: %v", err)
	}
}

func (c *WorkbuddyPrewarmController) Reconcile(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	namespaces, err := c.client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return fmt.Errorf("list namespaces: %w", err)
	}
	prefix := sanitizeK8sName(c.client.Namespace) + "-user-"
	for i := range namespaces.Items {
		namespace := namespaces.Items[i].Name
		if !strings.HasPrefix(namespace, prefix) {
			continue
		}
		if err := c.reconcileNamespace(ctx, namespace); err != nil {
			log.Printf("WorkBuddy prewarm namespace %s failed: %v", namespace, err)
		}
	}
	return nil
}

func (c *WorkbuddyPrewarmController) reconcileNamespace(ctx context.Context, namespace string) error {
	source, err := c.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, c.goldenPVC, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if source.Status.Phase != corev1.ClaimBound {
		return nil
	}

	selector := labels.Set{
		workbuddyPrewarmLabel:       "true",
		workbuddyPrewarmGoldenLabel: c.goldenPVC,
	}.AsSelector().String()
	pool, err := c.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return err
	}

	unclaimed := 0
	for i := range pool.Items {
		pvc := &pool.Items[i]
		if pvc.Labels[workbuddyPrewarmStateLabel] == workbuddyPrewarmStateClaim {
			continue
		}
		unclaimed++
		if err := c.ensureHolder(ctx, pvc); err != nil {
			log.Printf("WorkBuddy prewarm holder %s/%s failed: %v", namespace, pvc.Name, err)
		}
	}

	for unclaimed < c.poolSize {
		pvc, err := c.createClone(ctx, namespace, source)
		if err != nil {
			return err
		}
		unclaimed++
		if err := c.ensureHolder(ctx, pvc); err != nil {
			log.Printf("WorkBuddy prewarm holder %s/%s failed: %v", namespace, pvc.Name, err)
		}
	}
	return nil
}

func (c *WorkbuddyPrewarmController) createClone(ctx context.Context, namespace string, source *corev1.PersistentVolumeClaim) (*corev1.PersistentVolumeClaim, error) {
	size, ok := source.Spec.Resources.Requests[corev1.ResourceStorage]
	if !ok {
		return nil, fmt.Errorf("golden PVC %s/%s has no storage request", namespace, source.Name)
	}
	clone := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workbuddy-prewarm-" + strings.ToLower(utilrand.String(8)),
			Namespace: namespace,
			Labels: map[string]string{
				"app":                       "clawreef",
				"managed-by":                "clawreef",
				workbuddyPrewarmLabel:       "true",
				workbuddyPrewarmStateLabel:  workbuddyPrewarmStateWarm,
				workbuddyPrewarmGoldenLabel: c.goldenPVC,
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes:      append([]corev1.PersistentVolumeAccessMode(nil), source.Spec.AccessModes...),
			StorageClassName: source.Spec.StorageClassName,
			Resources: corev1.VolumeResourceRequirements{Requests: corev1.ResourceList{
				corev1.ResourceStorage: size.DeepCopy(),
			}},
			DataSource: &corev1.TypedLocalObjectReference{Kind: "PersistentVolumeClaim", Name: source.Name},
		},
	}
	created, err := c.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Create(ctx, clone, metav1.CreateOptions{})
	if err != nil {
		return nil, fmt.Errorf("create prewarm clone: %w", err)
	}
	log.Printf("WorkBuddy prewarm clone created: %s/%s", namespace, created.Name)
	return created, nil
}

func (c *WorkbuddyPrewarmController) ensureHolder(ctx context.Context, pvc *corev1.PersistentVolumeClaim) error {
	pods := c.client.Clientset.CoreV1().Pods(pvc.Namespace)
	name := workbuddyPrewarmHolderName(pvc.Name)
	pod, err := pods.Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			zero := int64(0)
			if deleteErr := pods.Delete(ctx, name, metav1.DeleteOptions{GracePeriodSeconds: &zero}); deleteErr != nil && !errors.IsNotFound(deleteErr) {
				return deleteErr
			}
			return nil
		}
		if workbuddyPodReady(pod) && pvc.Labels[workbuddyPrewarmStateLabel] != workbuddyPrewarmStateReady {
			copy := pvc.DeepCopy()
			copy.Labels[workbuddyPrewarmStateLabel] = workbuddyPrewarmStateReady
			_, err = c.client.Clientset.CoreV1().PersistentVolumeClaims(pvc.Namespace).Update(ctx, copy, metav1.UpdateOptions{})
			if err == nil {
				log.Printf("WorkBuddy prewarm clone ready: %s/%s", pvc.Namespace, pvc.Name)
			}
			return err
		}
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	controller := true
	blockOwnerDeletion := true
	zero := int64(0)
	automount := false
	holder := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: pvc.Namespace,
			Labels: map[string]string{
				"app":                    "clawreef-workbuddy-prewarm",
				"managed-by":             "clawreef",
				workbuddyPrewarmLabel:    "true",
				workbuddyPrewarmPVCLabel: pvc.Name,
			},
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion:         "v1",
				Kind:               "PersistentVolumeClaim",
				Name:               pvc.Name,
				UID:                pvc.UID,
				Controller:         &controller,
				BlockOwnerDeletion: &blockOwnerDeletion,
			}},
		},
		Spec: corev1.PodSpec{
			RestartPolicy:                 corev1.RestartPolicyNever,
			TerminationGracePeriodSeconds: &zero,
			AutomountServiceAccountToken:  &automount,
			NodeSelector: map[string]string{
				"clawmanager.io/windows-runtime": "true",
			},
			Containers: []corev1.Container{{
				Name:            "warmer",
				Image:           c.image,
				ImagePullPolicy: corev1.PullIfNotPresent,
				Command:         []string{"/bin/sh", "-lc"},
				Args: []string{
					"set -eu; rm -f " + workbuddyPrewarmMarker + "; dd if=/storage/data.qcow2 of=/dev/null bs=16M status=none; sync; touch " + workbuddyPrewarmMarker + "; exec sleep infinity",
				},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("512Mi"),
					},
				},
				ReadinessProbe: &corev1.Probe{
					ProbeHandler:     corev1.ProbeHandler{Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", "test -f " + workbuddyPrewarmMarker}}},
					PeriodSeconds:    5,
					TimeoutSeconds:   2,
					FailureThreshold: 120,
				},
				VolumeMounts: []corev1.VolumeMount{{Name: "storage", MountPath: "/storage"}},
			}},
			Volumes: []corev1.Volume{{
				Name:         "storage",
				VolumeSource: corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: pvc.Name}},
			}},
		},
	}
	_, err = pods.Create(ctx, holder, metav1.CreateOptions{})
	if errors.IsAlreadyExists(err) {
		return nil
	}
	return err
}

func workbuddyPrewarmHolderName(pvcName string) string {
	return sanitizeK8sName(pvcName + "-holder")
}

func workbuddyPodReady(pod *corev1.Pod) bool {
	if pod == nil || pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// ClaimWorkbuddyPrewarmPVC atomically reserves a ready prewarmed disk and
// detaches its holder Pod so the instance Deployment can mount it immediately.
func (s *PVCService) ClaimWorkbuddyPrewarmPVC(ctx context.Context, userID, instanceID, storageSizeGB int, storageClass, sourcePVCName string) (*corev1.PersistentVolumeClaim, error) {
	if s == nil || s.client == nil || s.client.Clientset == nil {
		return nil, fmt.Errorf("k8s client not initialized")
	}
	namespace := s.client.GetNamespace(userID)
	selector := labels.Set{
		workbuddyPrewarmLabel:       "true",
		workbuddyPrewarmStateLabel:  workbuddyPrewarmStateReady,
		workbuddyPrewarmGoldenLabel: strings.TrimSpace(sourcePVCName),
	}.AsSelector().String()
	pool, err := s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
	if err != nil {
		return nil, fmt.Errorf("list WorkBuddy prewarm PVCs: %w", err)
	}
	sort.Slice(pool.Items, func(i, j int) bool {
		return pool.Items[i].CreationTimestamp.Before(&pool.Items[j].CreationTimestamp)
	})
	expectedSize := resource.MustParse(fmt.Sprintf("%dGi", storageSizeGB))
	for i := range pool.Items {
		pvc := &pool.Items[i]
		if pvc.Status.Phase != corev1.ClaimBound {
			continue
		}
		actualSize, ok := pvc.Spec.Resources.Requests[corev1.ResourceStorage]
		if !ok || actualSize.Cmp(expectedSize) != 0 {
			continue
		}
		if requested := strings.TrimSpace(storageClass); requested != "" && pvc.Spec.StorageClassName != nil && strings.TrimSpace(*pvc.Spec.StorageClassName) != requested {
			continue
		}
		holderName := workbuddyPrewarmHolderName(pvc.Name)
		holder, err := s.client.Clientset.CoreV1().Pods(namespace).Get(ctx, holderName, metav1.GetOptions{})
		if err != nil || !workbuddyPodReady(holder) {
			continue
		}

		claimed := pvc.DeepCopy()
		if claimed.Labels == nil {
			claimed.Labels = map[string]string{}
		}
		claimed.Labels[workbuddyPrewarmStateLabel] = workbuddyPrewarmStateClaim
		claimed.Labels["instance-id"] = strconv.Itoa(instanceID)
		claimed.Labels["user-id"] = strconv.Itoa(userID)
		claimed, err = s.client.Clientset.CoreV1().PersistentVolumeClaims(namespace).Update(ctx, claimed, metav1.UpdateOptions{})
		if errors.IsConflict(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("claim WorkBuddy prewarm PVC %s: %w", pvc.Name, err)
		}

		zero := int64(0)
		if err := s.client.Clientset.CoreV1().Pods(namespace).Delete(ctx, holderName, metav1.DeleteOptions{GracePeriodSeconds: &zero}); err != nil && !errors.IsNotFound(err) {
			return claimed, fmt.Errorf("detach WorkBuddy prewarm PVC %s: %w", pvc.Name, err)
		}
		if err := waitForPodDeletion(ctx, s.client, namespace, holderName); err != nil {
			return claimed, err
		}
		log.Printf("WorkBuddy prewarm clone claimed: %s/%s instance=%d", namespace, claimed.Name, instanceID)
		return claimed, nil
	}
	return nil, nil
}

func waitForPodDeletion(ctx context.Context, client *Client, namespace, name string) error {
	deadline := time.NewTimer(60 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for prewarm holder %s/%s deletion", namespace, name)
		case <-ticker.C:
			_, err := client.Clientset.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
		}
	}
}
