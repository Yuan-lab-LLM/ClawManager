package services

import (
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"database/sql"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/upper/db/v4"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/fake"
)

func runtimeScaleMySQLFixture(t *testing.T, runtimeType string, names ...string) (db.Session, *RuntimePodMaintenanceService, *fake.Clientset) {
	t.Helper()
	sess := runtimeManagementMySQLDB(t)
	scheduler := runtimeManagementMySQLService(sess).scheduler
	scheduler.runtimeNamespace = "runtime"
	image := "repo/runtime@sha256:" + strings.Repeat("a", 64)
	objects := []runtime.Object{}
	yes := true
	for i, name := range names {
		d := k8s.BuildRuntimeDeployment(k8s.RuntimeDeploymentSpec{Name: name, Namespace: "runtime", RuntimeType: runtimeType, Image: image, Replicas: 2, SlotCount: 8, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspace"})
		d.UID = types.UID(name + "-uid")
		d.ResourceVersion = "1"
		if strings.HasPrefix(name, "aaa-custom") {
			d.Labels["clawmanager.io/managed-pool"] = "true"
			d.Labels["clawmanager.io/purpose"] = "runtime-managed-pool"
			replicas := int32(9)
			d.Spec.Replicas = &replicas
		}
		if strings.HasPrefix(name, "openclaw-runtime-u") {
			id := "5"
			source := "openclaw-runtime"
			if strings.HasSuffix(name, "u6") {
				id = "6"
				if strings.Contains(name, "u5-u6") {
					source = "openclaw-runtime-u5"
				}
			}
			d.Labels["clawmanager.io/pool-role"] = "upgrade-target"
			d.Labels["clawmanager.io/upgrade-id"] = id
			d.Labels["clawmanager.io/source-deployment"] = source
			d.Labels["clawmanager.io/scheduling-enabled"] = "true"
		}
		rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name + "-rs", Namespace: "runtime", UID: types.UID(name + "-rs-uid"), OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: name, UID: d.UID, Controller: &yes}}}}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-pod", Namespace: "runtime", UID: types.UID(name + "-pod-uid"), Labels: d.Spec.Template.Labels, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: *d.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
		for _, c := range pod.Spec.Containers {
			pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: c.Name, Ready: true, ImageID: image})
		}
		objects = append(objects, d, rs, pod)
		if _, err := sess.SQL().Exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,state,capacity,used_slots,last_seen_at) VALUES(?,?,'runtime',?,?,?,?,'ready',8,0,?)", i+1, runtimeType, pod.Name, string(pod.UID), name, image, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "runtime", UID: "workspace-uid"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "workspace-pv", AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	objects = append(objects, claim)
	kube := fake.NewSimpleClientset(objects...)
	scheduler.deployments = k8s.NewRuntimeDeploymentService(kube)
	return sess, NewRuntimePodMaintenanceService(sess, scheduler), kube
}

func TestDefaultRuntimeScaleMySQLAdmission(t *testing.T) {
	for _, scenario := range []string{"busy-operation", "unrelated-upgrade", "same-type-pending", "same-type-running", "scale-in-pending", "scale-in-attention", "deletion-intent"} {
		t.Run(scenario, func(t *testing.T) {
			sess, service, kube := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime", "aaa-custom")
			exec := func(q string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(q, args...); err != nil {
					t.Fatal(err)
				}
			}
			switch scenario {
			case "busy-operation":
				exec("INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('busy','balance','needs_attention','{}','[]',NOW(),NOW())")
			case "unrelated-upgrade":
				exec("INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status) VALUES('openclaw','image','running')")
			case "same-type-pending", "same-type-running":
				exec("INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status) VALUES('hermes','image',?)", strings.TrimPrefix(scenario, "same-type-"))
			case "scale-in-pending", "scale-in-attention":
				status := "pending"
				if scenario == "scale-in-attention" {
					status = "needs_attention"
				}
				exec(`INSERT INTO runtime_management_operations(id,kind,status,request_json,items_json,created_at,updated_at) VALUES('shrink','scale_in',?,'{"runtime_type":"hermes"}','[]',NOW(),NOW())`, status)
			case "deletion-intent":
				exec("INSERT INTO runtime_pod_deletion_intents(namespace,deployment_name,deployment_uid) VALUES('runtime','hermes-runtime','hermes-runtime-uid')")
			}
			result, err := service.Scale(context.Background(), k8s.RuntimePodScaleRequest{RuntimeType: "hermes", PodCount: 3})
			blocked := strings.HasPrefix(scenario, "same-type") || strings.HasPrefix(scenario, "scale-in-") || scenario == "deletion-intent"
			if blocked {
				if err == nil {
					t.Fatal("unsafe default expansion admitted")
				}
			} else if err != nil || result.DeploymentName != "hermes-runtime" || result.PreviousReplicas != 2 || result.Replicas != 5 {
				t.Fatalf("safe expansion blocked or chose custom: %+v %v", result, err)
			}
			custom, _ := kube.AppsV1().Deployments("runtime").Get(context.Background(), "aaa-custom", metav1.GetOptions{})
			if *custom.Spec.Replicas != 9 {
				t.Fatal("expanded a custom pool")
			}
			if blocked {
				for _, a := range kube.Actions() {
					if a.GetVerb() == "patch" {
						t.Fatal("denied request changed Kubernetes")
					}
				}
			}
		})
	}
}

func TestDefaultRuntimeScaleMySQLConcurrentCountsAndInheritedSpec(t *testing.T) {
	_, service, kube := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime", "aaa-custom")
	ctx := context.Background()
	before, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	var group sync.WaitGroup
	errs := make(chan error, 2)
	for _, count := range []int{2, 3} {
		group.Add(1)
		go func(n int) {
			defer group.Done()
			_, err := service.Scale(ctx, k8s.RuntimePodScaleRequest{RuntimeType: "hermes", PodCount: n})
			errs <- err
		}(count)
	}
	group.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	after, _ := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
	if *after.Spec.Replicas != 7 {
		t.Fatalf("lost concurrent increment: %d", *after.Spec.Replicas)
	}
	if !reflect.DeepEqual(before.Spec.Template, after.Spec.Template) {
		t.Fatal("scale changed inherited slots/resources/template")
	}
	options, err := service.ScaleOptions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Items) != 1 || options.Items[0].RuntimeType != "hermes" || options.Items[0].SlotCount != 8 || options.Items[0].Replicas != 7 || options.Limits.MinPods != 1 || options.Limits.MaxPods != 10 {
		t.Fatalf("bad verified options: %+v", options)
	}
}

func TestDefaultRuntimeScaleMySQLLatestOpenClawCanonicalTarget(t *testing.T) {
	sess, service, kube := runtimeScaleMySQLFixture(t, "openclaw", "openclaw-runtime", "openclaw-runtime-u5", "openclaw-runtime-u6", "openclaw-runtime-u5-u6", "aaa-custom")
	image := "repo/runtime@sha256:" + strings.Repeat("a", 64)
	for _, id := range []int{5, 6} {
		sources := `{"runtime/openclaw-runtime":"old"}`
		if id == 6 {
			sources = `{"runtime/openclaw-runtime":"old","runtime/openclaw-runtime-u5":"previous","runtime/aaa-custom":"custom"}`
		}
		if _, err := sess.SQL().Exec("INSERT INTO runtime_rollouts(id,runtime_type,target_image_ref,status,preflight_id,plan_fingerprint,source_images_json) VALUES(?,'openclaw',?,'finished',?, 'fingerprint',?)", id, image, "approved-"+strconv.Itoa(id), sources); err != nil {
			t.Fatal(err)
		}
	}
	result, err := service.Scale(context.Background(), k8s.RuntimePodScaleRequest{RuntimeType: "openclaw", PodCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeploymentName != "openclaw-runtime-u5-u6" || result.Replicas != 4 {
		t.Fatalf("wrong current default: %+v", result)
	}
	for _, name := range []string{"openclaw-runtime", "openclaw-runtime-u5", "openclaw-runtime-u6"} {
		d, _ := kube.AppsV1().Deployments("runtime").Get(context.Background(), name, metav1.GetOptions{})
		if *d.Spec.Replicas != 2 {
			t.Fatalf("resized retained/equivalent unselected pool %s", name)
		}
	}
	// Equivalent current targets may have different replica counts. A different
	// later-slot resource distribution makes the default ambiguous even when
	// all displayed aggregate/first-slot budgets remain equal.
	alternative, _ := kube.AppsV1().Deployments("runtime").Get(context.Background(), "openclaw-runtime-u6", metav1.GetOptions{})
	alternative.Spec.Template.Spec.Containers[2].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("150m")
	alternative.Spec.Template.Spec.Containers[3].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("50m")
	if _, err := kube.AppsV1().Deployments("runtime").Update(context.Background(), alternative, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	live, _ := kube.CoreV1().Pods("runtime").Get(context.Background(), "openclaw-runtime-u6-pod", metav1.GetOptions{})
	live.Spec.Containers[2].Resources = alternative.Spec.Template.Spec.Containers[2].Resources
	live.Spec.Containers[3].Resources = alternative.Spec.Template.Spec.Containers[3].Resources
	if _, err := kube.CoreV1().Pods("runtime").Update(context.Background(), live, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	actionsBefore := len(kube.Actions())
	if _, err := service.Scale(context.Background(), k8s.RuntimePodScaleRequest{RuntimeType: "openclaw", PodCount: 1}); err == nil || !strings.Contains(err.Error(), "conflicting") {
		t.Fatalf("silently selected conflicting default targets: %v", err)
	}
	for _, action := range kube.Actions()[actionsBefore:] {
		if action.GetVerb() == "patch" {
			t.Fatal("ambiguous default configurations changed replicas")
		}
	}
	options, err := service.ScaleOptions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(options.Items) != 0 {
		t.Fatalf("ambiguous default remains selectable: %+v", options.Items)
	}

}

func TestDefaultRuntimeScaleMySQLUsesAdmissionTransactionConnection(t *testing.T) {
	sess, service, _ := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime")
	sess.SetMaxOpenConns(1)
	if sess.Driver().(*sql.DB).Stats().MaxOpenConnections != 1 {
		t.Fatal("single connection cap was not applied")
	}
	// A base-session Pod query inside the admitted transaction would wait for the
	// connection held by that same transaction. Keep this regression bounded.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	service.scheduler.podRepo = repository.NewRuntimePodRepository(sess.WithContext(ctx))
	result, err := service.Scale(ctx, k8s.RuntimePodScaleRequest{RuntimeType: "hermes", PodCount: 1})
	if err != nil || result == nil || result.Replicas != 3 {
		t.Fatalf("admission read escaped its transaction: %+v %v", result, err)
	}
}
