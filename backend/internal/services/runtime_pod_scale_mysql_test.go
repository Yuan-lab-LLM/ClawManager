package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
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
	for _, file := range []string{"124_runtime_scale_approvals.sql", "068_add_runtime_rollout_checkpoints.sql"} {
		migration, err := os.ReadFile("../db/migrations/" + file)
		if err != nil {
			t.Fatal(err)
		}
		schema := string(migration)
		for _, statement := range strings.Split(schema[strings.Index(schema, "CREATE TABLE"):], ";") {
			if strings.TrimSpace(statement) == "" {
				continue
			}
			if _, err := sess.SQL().Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	scheduler := runtimeManagementMySQLService(sess).scheduler
	scheduler.runtimeNamespace = "runtime"
	image := "repo/runtime@sha256:" + strings.Repeat("a", 64)
	objects := []runtime.Object{}
	yes := true
	for i, name := range names {
		kind := runtimeType
		for _, candidate := range []string{RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
			if name == candidate+"-runtime" {
				kind = candidate
				break
			}
		}
		ip := fmt.Sprintf("10.1.0.%d", i+1)
		d := k8s.BuildRuntimeDeployment(k8s.RuntimeDeploymentSpec{Name: name, Namespace: "runtime", RuntimeType: kind, Image: image, Replicas: 2, SlotCount: 8, AgentTokenSecretName: "agent", WorkspacePVCClaimName: "workspace"})
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
		rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name + "-rs", Namespace: "runtime", UID: types.UID(name + "-rs-uid"), Labels: d.Spec.Template.Labels, OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: name, UID: d.UID, Controller: &yes}}}, Spec: appsv1.ReplicaSetSpec{Template: *d.Spec.Template.DeepCopy()}}
		pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-pod", Namespace: "runtime", UID: types.UID(name + "-pod-uid"), Labels: d.Spec.Template.Labels, Annotations: d.Spec.Template.Annotations, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: *d.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: ip, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}
		for i, c := range pod.Spec.Containers {
			if pod.Annotations["container.apparmor.security.beta.kubernetes.io/"+c.Name] == "unconfined" {
				pod.Spec.Containers[i].SecurityContext.AppArmorProfile = &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}
			}
			pod.Status.ContainerStatuses = append(pod.Status.ContainerStatuses, corev1.ContainerStatus{Name: c.Name, Ready: true, ImageID: image})
		}
		objects = append(objects, d, rs, pod)
		if _, err := sess.SQL().Exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,pod_ip,agent_endpoint,deployment_name,image_ref,state,capacity,used_slots,last_seen_at) VALUES(?,?,'runtime',?,?,?,?,?,?,'ready',8,0,?)", i+1, kind, pod.Name, string(pod.UID), ip, "https://"+ip+":19090", name, image, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	claim := &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "workspace", Namespace: "runtime", UID: "workspace-uid"}, Spec: corev1.PersistentVolumeClaimSpec{VolumeName: "workspace-pv", AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteMany}}, Status: corev1.PersistentVolumeClaimStatus{Phase: corev1.ClaimBound}}
	pv := &corev1.PersistentVolume{ObjectMeta: metav1.ObjectMeta{Name: claim.Spec.VolumeName, UID: "workspace-pv-uid"}, Spec: corev1.PersistentVolumeSpec{ClaimRef: &corev1.ObjectReference{Namespace: claim.Namespace, Name: claim.Name, UID: claim.UID}}}
	objects = append(objects, claim, pv)
	kube := fake.NewSimpleClientset(objects...)
	scheduler.deployments = k8s.NewRuntimeDeploymentService(kube)
	scheduler.agentClient = scaleFixtureNativeAgent(kube, nil)
	return sess, NewRuntimePodMaintenanceService(sess, scheduler), kube
}

func scaleFixtureNativeAgent(kube *fake.Clientset, before func(*http.Request) error) RuntimeAgentClient {
	return NewSlotRuntimeAgentClientWithHTTPClient("control", kube, "runtime", &http.Client{Transport: slotRoundTripper(func(req *http.Request) (*http.Response, error) {
		if before != nil {
			if err := before(req); err != nil {
				return nil, err
			}
		}
		pods, err := kube.CoreV1().Pods("runtime").List(req.Context(), metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		for _, pod := range pods.Items {
			if pod.Status.PodIP != req.URL.Hostname() {
				continue
			}
			snapshot := auditFreeSnapshot()
			switch pod.Annotations["test.runtime/worker"] {
			case "legacy":
				delete(snapshot, "worker_epoch")
			case "quarantined":
				snapshot["quarantined"] = true
			case "running":
				snapshot = auditOccupiedSnapshot("running")
				owner := snapshot["assignment"].(map[string]any)
				owner["pod_uid"] = string(pod.UID)
				snapshot["gateways"] = []map[string]any{{"gateway_id": "gw-7-1", "state": "running"}}
			}
			snapshot["pod_uid"] = string(pod.UID)
			raw, err := json.Marshal(snapshot)
			if err != nil {
				return nil, err
			}
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(string(raw)))}, nil
		}
		return nil, errors.New("unknown worker endpoint")
	})})
}

func TestDefaultOpenClawScaleAfterInPlacePoolResetMySQL(t *testing.T) {
	ctx := context.Background()
	sess, service, kube := runtimeScaleMySQLFixture(t, RuntimeTypeOpenClaw, "openclaw-runtime", "aaa-custom")
	sourceImage := "repo/runtime@sha256:" + strings.Repeat("b", 64)
	targetImage := "repo/runtime@sha256:" + strings.Repeat("a", 64)
	sources := fmt.Sprintf(`{"runtime/openclaw-runtime":%q}`, sourceImage)
	_, err := sess.SQL().Exec(`INSERT INTO runtime_rollouts(id,runtime_type,status,phase,target_image_ref,preflight_json,source_images_json)
		VALUES(9,'openclaw','finished','empty_pool_reset',?,'{"cold_successor":null,"pool_switch_waiting_instance_ids":[]}',?)`, targetImage, sources)
	if err != nil {
		t.Fatal(err)
	}
	p := poolReplacementProgress{
		Kind: "openclaw-pool-replacement", Version: 2, Stage: "cleanup_success",
		ProbeIndex: 1, PoolIndex: 1, Changed: true,
		Targets: []k8s.RuntimeRolloutTarget{{Namespace: "runtime", Name: "openclaw-runtime", UID: "openclaw-runtime-uid", Image: sourceImage, Ready: true}},
	}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.NewRuntimeRolloutRepository(sess).(rolloutCheckpointStore).SaveRolloutCheckpoint(ctx, 9, string(raw)); err != nil {
		t.Fatal(err)
	}
	result, err := service.Scale(ctx, k8s.RuntimePodScaleRequest{RuntimeType: RuntimeTypeOpenClaw, PodCount: 2})
	if err != nil {
		t.Fatal(err)
	}
	if result.DeploymentName != "openclaw-runtime" || result.PreviousReplicas != 2 || result.Replicas != 4 {
		t.Fatalf("in-place reset did not expand the original default: %+v", result)
	}
	custom, err := kube.AppsV1().Deployments("runtime").Get(ctx, "aaa-custom", metav1.GetOptions{})
	if err != nil || *custom.Spec.Replicas != 9 {
		t.Fatalf("custom pool changed: %v", err)
	}
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
	rs, err := kube.AppsV1().ReplicaSets("runtime").Get(context.Background(), "openclaw-runtime-u6-rs", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rs.Spec.Template = alternative.Spec.Template
	if _, err := kube.AppsV1().ReplicaSets("runtime").Update(context.Background(), rs, metav1.UpdateOptions{}); err != nil {
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

func TestDefaultRuntimeScaleMySQLPinsCommittedColdDeploymentUID(t *testing.T) {
	_, service, _ := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime")
	inspector := service.scheduler.deployments.(k8s.RuntimePodScaleDeploymentService)
	target := k8s.RuntimePodScaleTarget{RuntimeType: "hermes", Namespace: "runtime", DeploymentName: "hermes-runtime", DeploymentUID: "hermes-runtime-uid", PodName: "hermes-runtime-pod", PodUID: "hermes-runtime-pod-uid"}
	if _, err := inspector.InspectRuntimeScale(context.Background(), target); err != nil {
		t.Fatal(err)
	}
	target.DeploymentUID = "replaced-controller"
	if _, err := inspector.InspectRuntimeScale(context.Background(), target); err == nil {
		t.Fatal("replaced cold default controller was expanded")
	}
}

func TestDefaultRuntimeScaleMySQLApprovedPoolSurvivesUnknownPods(t *testing.T) {
	ctx := context.Background()
	for _, scenario := range []string{"manual", "automatic", "automatic-create", "unapproved", "uid", "image", "mount", "resources", "security", "protocol", "storage", "source"} {
		t.Run(scenario, func(t *testing.T) {
			sess, service, kube := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime")
			if scenario != "unapproved" {
				options, err := service.ScaleOptions(ctx)
				if err != nil || len(options.Items) != 1 {
					t.Fatalf("healthy source was not approved: %+v %v", options, err)
				}
			}
			if _, err := sess.SQL().Exec("UPDATE runtime_pods SET state='unhealthy' WHERE id=1"); err != nil {
				t.Fatal(err)
			}
			live, err := kube.CoreV1().Pods("runtime").Get(ctx, "hermes-runtime-pod", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			live.Status.Phase = corev1.PodUnknown
			live.Status.Conditions = nil
			if _, err := kube.CoreV1().Pods("runtime").UpdateStatus(ctx, live, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			d, err := kube.AppsV1().Deployments("runtime").Get(ctx, "hermes-runtime", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "uid":
				d.UID = "replacement"
			case "image":
				d.Spec.Template.Spec.Containers[0].Image = "repo/runtime@sha256:" + strings.Repeat("b", 64)
			case "mount":
				d.Spec.Template.Spec.Containers[0].VolumeMounts[0].ReadOnly = true
			case "resources":
				d.Spec.Template.Spec.Containers[0].Resources.Requests[corev1.ResourceCPU] = resource.MustParse("800m")
			case "security":
				d.Spec.Template.Spec.SecurityContext = &corev1.PodSecurityContext{RunAsNonRoot: ptrBool(false)}
			case "protocol":
				d.Spec.Template.Labels["clawmanager.io/runtime-protocol"] = "unsupported"
			case "storage":
				claim, err := kube.CoreV1().PersistentVolumeClaims("runtime").Get(ctx, "workspace", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				claim.Spec.VolumeName = "different-pv"
				if _, err := kube.CoreV1().PersistentVolumeClaims("runtime").Update(ctx, claim, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			case "source":
				d.Labels["clawmanager.io/source-deployment"] = "unknown-source"
			}
			if _, err := kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			// Reconstruct the service to exercise persistent configuration proof.
			service = NewRuntimePodMaintenanceService(sess, service.scheduler)
			if scenario == "automatic-create" {
				instance := models.Instance{ID: 17, UserID: 1, Type: RuntimeTypeHermes, RuntimeType: RuntimeBackendGateway, InstanceMode: InstanceModeLite, Status: "creating", RuntimeGeneration: 1, WorkspacePath: ptrString("/workspace")}
				if err := service.scheduler.assignInstance(ctx, instance); !errors.Is(err, errRuntimeScaleOutPending) {
					t.Fatalf("ordinary creation did not wait for the approved replacement capacity: %v", err)
				}
			} else if scenario == "automatic" {
				scaled, err := service.scheduler.scaleOutIfBacklogExceedsCapacity(ctx, "hermes", 1)
				if err != nil || !scaled {
					t.Fatalf("approved unknown pool did not expand automatically: scaled=%v err=%v", scaled, err)
				}
			} else {
				result, err := service.Scale(ctx, k8s.RuntimePodScaleRequest{RuntimeType: "hermes", PodCount: 1})
				if scenario == "manual" && (err != nil || result.Replicas != 3) {
					t.Fatalf("approved unknown pool did not expand manually: %+v %v", result, err)
				}
				if scenario != "manual" {
					if err == nil {
						t.Fatal("unapproved configuration was expanded manually")
					}
					if scaled, err := service.scheduler.scaleOutIfAtCapacity(ctx, "hermes"); err == nil || scaled {
						t.Fatalf("unapproved configuration was expanded automatically: scaled=%v err=%v", scaled, err)
					}
				}
			}
			latest, err := kube.AppsV1().Deployments("runtime").Get(ctx, d.Name, metav1.GetOptions{})
			want := int32(2)
			if scenario == "manual" || strings.HasPrefix(scenario, "automatic") {
				want = 3
			}
			if err != nil || *latest.Spec.Replicas != want {
				t.Fatalf("unexpected desired replicas: %+v %v", latest.Spec.Replicas, err)
			}
		})
	}
}

func TestDefaultRuntimeScaleMySQLBoundsUnverifiedStartup(t *testing.T) {
	ctx := context.Background()
	sess, service, kube := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime")
	scheduler := service.scheduler
	options, err := service.ScaleOptions(ctx)
	if err != nil || len(options.Items) != 1 {
		t.Fatalf("failed to establish source approval: %+v %v", options, err)
	}
	if _, err := sess.SQL().Exec("UPDATE runtime_pods SET state='unhealthy'"); err != nil {
		t.Fatal(err)
	}
	deployments := kube.AppsV1().Deployments("runtime")
	pods := kube.CoreV1().Pods("runtime")
	const name = "hermes-runtime"
	old, err := pods.Get(ctx, name+"-pod", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	old.Status.Phase = corev1.PodUnknown
	old.Status.Conditions = nil
	if _, err := pods.UpdateStatus(ctx, old, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	// An already sizable Deployment can keep growing when demand calls for it.
	d, err := deployments.Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	*d.Spec.Replicas = 80
	if _, err := deployments.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	checkScale := func(wantReplicas int32, wantPendingReason string) *appsv1.Deployment {
		t.Helper()
		waiting, err := scheduler.scaleOutIfBacklogExceedsCapacity(ctx, "hermes", 100)
		if wantPendingReason == "" {
			if err != nil || !waiting {
				t.Fatalf("expected capacity work to be pending: waiting=%v err=%v", waiting, err)
			}
		} else if !errors.Is(err, errRuntimeControlPlanePending) || !strings.Contains(err.Error(), wantPendingReason) {
			t.Fatalf("expected bounded wait %q: %v", wantPendingReason, err)
		}
		got, err := deployments.Get(ctx, name, metav1.GetOptions{})
		if err != nil || *got.Spec.Replicas != wantReplicas {
			t.Fatalf("desired replicas got=%d want=%d err=%v", *got.Spec.Replicas, wantReplicas, err)
		}
		return got
	}
	expire := func(d *appsv1.Deployment) {
		t.Helper()
		annotation := "clawmanager.io/runtime-expansion"
		var attempt map[string]any
		if err := json.Unmarshal([]byte(d.Annotations[annotation]), &attempt); err != nil {
			t.Fatal(err)
		}
		attempt["deadline"] = time.Now().UTC().Add(-time.Second)
		raw, err := json.Marshal(attempt)
		if err != nil {
			t.Fatal(err)
		}
		d.Annotations[annotation] = string(raw)
		if _, err := deployments.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
			t.Fatal(err)
		}
	}
	first := checkScale(81, "")
	checkScale(81, "") // no new Pod UID: wait for the controller first

	starting := old.DeepCopy()
	starting.Name = name + "-new"
	starting.UID = "startup-uid"
	starting.CreationTimestamp = metav1.Now()
	starting.Status = corev1.PodStatus{Phase: corev1.PodPending}
	if _, err := pods.Create(ctx, starting, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	checkScale(81, "") // confirmed startup in the exact attempt
	starting.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodScheduled, Status: corev1.ConditionFalse, Reason: corev1.PodReasonUnschedulable, Message: "Insufficient cpu"}}
	if _, err := pods.UpdateStatus(ctx, starting, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	expire(first)
	checkScale(81, "Insufficient cpu")

	starting.Status.Phase = corev1.PodUnknown
	starting.Status.Conditions = nil
	if _, err := pods.UpdateStatus(ctx, starting, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	second := checkScale(82, "")
	expire(second)
	third := checkScale(83, "")
	expire(third)
	checkScale(83, "3 consecutive Runtime expansion attempts")
	checkScale(83, "3 consecutive Runtime expansion attempts")
}

func TestDefaultRuntimeScaleMySQLRequiresNativeProtocolApproval(t *testing.T) {
	ctx := context.Background()
	for _, mode := range []string{"no-native-client", "old-worker"} {
		t.Run(mode, func(t *testing.T) {
			sess, service, kube := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime")
			if mode == "no-native-client" {
				service.scheduler.agentClient = &fakeRuntimeAgentClient{}
			} else {
				pods := kube.CoreV1().Pods("runtime")
				pod, err := pods.Get(ctx, "hermes-runtime-pod", metav1.GetOptions{})
				if err != nil {
					t.Fatal(err)
				}
				pod.Annotations["test.runtime/worker"] = "legacy"
				if _, err := pods.Update(ctx, pod, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			options, err := service.ScaleOptions(ctx)
			if err != nil || len(options.Items) != 0 {
				t.Fatalf("legacy protocol established native approval: %+v %v", options, err)
			}
			row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_scale_approvals")
			if err != nil {
				t.Fatal(err)
			}
			var count int
			if err := row.Scan(&count); err != nil || count != 0 {
				t.Fatalf("unverified approval persisted: %d %v", count, err)
			}
		})
	}
}

func TestDefaultRuntimeScaleMySQLCommittedInPlaceApproval(t *testing.T) {
	ctx := context.Background()
	for _, runtimeType := range []string{"hermes", "deepseek-harness"} {
		t.Run(runtimeType, func(t *testing.T) {
			name := runtimeType + "-runtime"
			sess, service, kube := runtimeScaleMySQLFixture(t, runtimeType, name)
			assertOption := func(want bool) {
				t.Helper()
				options, err := service.ScaleOptions(ctx)
				if err != nil || (len(options.Items) > 0) != want {
					t.Fatalf("want option=%v: %+v %v", want, options, err)
				}
			}
			assertOption(true)
			deployments, pods, sets := kube.AppsV1().Deployments("runtime"), kube.CoreV1().Pods("runtime"), kube.AppsV1().ReplicaSets("runtime")
			d, _ := deployments.Get(ctx, name, metav1.GetOptions{})
			old, _ := pods.Get(ctx, name+"-pod", metav1.GetOptions{})
			rs, _ := sets.Get(ctx, name+"-rs", metav1.GetOptions{})
			newImage := "repo/runtime@sha256:" + strings.Repeat("b", 64)
			newD, newP, newRS := d.DeepCopy(), old.DeepCopy(), rs.DeepCopy()
			for i := range newD.Spec.Template.Spec.Containers {
				newD.Spec.Template.Spec.Containers[i].Image = newImage
				newP.Spec.Containers[i].Image = newImage
				newP.Status.ContainerStatuses[i].ImageID = newImage
			}
			newRS.Spec.Template = *newD.Spec.Template.DeepCopy()
			if _, err := deployments.Update(ctx, newD, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := pods.Update(ctx, newP, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if _, err := sets.Update(ctx, newRS, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			exec := func(query string, params ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(query, params...); err != nil {
					t.Fatal(err)
				}
			}
			exec("UPDATE runtime_pods SET image_ref=?", newImage)
			exec("INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status,preflight_json) VALUES(?,?,'finished','{}')", runtimeType, newImage)
			assertOption(true)
			exec("INSERT INTO runtime_rollouts(runtime_type,target_image_ref,status,preflight_json) VALUES(?,?,'finished','{\"recovery_mode\":\"rollback\"}'),(?,?,'finished','{\"upgrade_lab_run_id\":23}')", runtimeType, old.Spec.Containers[0].Image, runtimeType, old.Spec.Containers[0].Image)
			exec("UPDATE runtime_pods SET state='unhealthy'")
			newP.Status.Phase = corev1.PodUnknown
			if _, err := pods.UpdateStatus(ctx, newP, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			assertOption(true)
			if _, err := deployments.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
				t.Fatal(err)
			}
			if result, err := service.Scale(ctx, k8s.RuntimePodScaleRequest{RuntimeType: runtimeType, PodCount: 1}); err == nil {
				t.Fatalf("stale same-UID approval survived committed image change: %+v", result)
			}
		})
	}
}

func TestDefaultRuntimeScaleMySQLWarmupDoesNotBlockScheduling(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sess, service, kube := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime", "opencode-runtime")
	entered, unblocked, returned := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var first sync.Once
	service.scheduler.agentClient = scaleFixtureNativeAgent(kube, func(req *http.Request) error {
		if req.URL.Hostname() != "10.1.0.1" {
			return nil
		}
		first.Do(func() { close(entered) })
		<-req.Context().Done()
		closeOnce(unblocked)
		return req.Context().Err()
	})
	t.Cleanup(func() {
		cancel()
		select {
		case <-unblocked:
		case <-time.After(3 * time.Second):
		}
		select {
		case <-returned:
		case <-time.After(3 * time.Second):
		}
	})
	go func() {
		service.scheduler.refreshRuntimeScaleApprovals(ctx, time.Now())
		close(returned)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("did not start Hermes worker inspection")
	}
	select {
	case <-returned:
	case <-time.After(150 * time.Millisecond):
		t.Fatal("Hermes worker probe blocked the main scheduler pass")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM runtime_scale_approvals WHERE runtime_type='opencode'")
		if err != nil {
			t.Fatal(err)
		}
		var count int
		if err := row.Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count == 1 {
			select {
			case <-unblocked:
				t.Fatal("OpenCode approval waited for the Hermes probe")
			default:
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("OpenCode healthy proof did not progress independently")
}

func closeOnce(done chan struct{}) {
	select {
	case <-done:
	default:
		close(done)
	}
}

func TestDefaultRuntimeScaleMySQLRetryBudgetRequiresUsableNewPod(t *testing.T) {
	ctx := context.Background()
	for _, worker := range []string{"quarantined", "running"} {
		t.Run(worker, func(t *testing.T) {
			sess, service, kube := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime")
			const name = "hermes-runtime"
			options, err := service.ScaleOptions(ctx)
			if err != nil || len(options.Items) != 1 {
				t.Fatalf("missing source approval: %+v %v", options, err)
			}
			exec := func(query string, args ...any) {
				t.Helper()
				if _, err := sess.SQL().Exec(query, args...); err != nil {
					t.Fatal(err)
				}
			}
			exec("UPDATE runtime_pods SET state='unhealthy' WHERE id=1")
			deployments := kube.AppsV1().Deployments("runtime")
			pods := kube.CoreV1().Pods("runtime")
			template, err := pods.Get(ctx, name+"-pod", metav1.GetOptions{})
			if err != nil {
				t.Fatal(err)
			}
			advance := func(want int32, blocked bool) *appsv1.Deployment {
				t.Helper()
				waiting, err := service.scheduler.scaleOutIfBacklogExceedsCapacity(ctx, "hermes", 100)
				if blocked {
					if !errors.Is(err, errRuntimeControlPlanePending) || !strings.Contains(err.Error(), "consecutive Runtime expansion attempts") {
						t.Fatalf("failed capacity reset the retry budget: waiting=%v err=%v", waiting, err)
					}
				} else if err != nil || !waiting {
					t.Fatalf("expected automatic expansion: waiting=%v err=%v", waiting, err)
				}
				d, err := deployments.Get(ctx, name, metav1.GetOptions{})
				if err != nil || *d.Spec.Replicas != want {
					t.Fatalf("desired replicas %+v, expected %d: %v", d.Spec.Replicas, want, err)
				}
				return d
			}
			publishPod := func(d *appsv1.Deployment, index int) {
				t.Helper()
				pod := template.DeepCopy()
				pod.Name = fmt.Sprintf("%s-extra-%d", name, index)
				pod.UID = types.UID(pod.Name)
				pod.Status.PodIP = fmt.Sprintf("10.1.1.%d", index)
				pod.CreationTimestamp = metav1.Now()
				pod.Annotations["test.runtime/worker"] = worker
				if _, err := pods.Create(ctx, pod, metav1.CreateOptions{}); err != nil {
					t.Fatal(err)
				}
				used := 0
				if worker == "running" {
					used = 8
				}
				exec("INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,pod_ip,agent_endpoint,deployment_name,image_ref,state,capacity,used_slots,last_seen_at) VALUES(?,'hermes','runtime',?,?,?,?,?,?,'ready',8,?,?)",
					index+10, pod.Name, string(pod.UID), pod.Status.PodIP, "https://"+pod.Status.PodIP+":19090", name, template.Spec.Containers[0].Image, used, time.Now().UTC())
				if worker == "running" && index == 3 {
					exec("INSERT INTO instances(id,user_id,type,runtime_generation) VALUES (7,2,'hermes',1)")
					exec("INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(7,?,'hermes','gw-7-1',20000,'/workspace','running',1)", index+10)
				}
				const annotation = "clawmanager.io/runtime-expansion"
				var attempt map[string]any
				if err := json.Unmarshal([]byte(d.Annotations[annotation]), &attempt); err != nil {
					t.Fatal(err)
				}
				attempt["deadline"] = time.Now().UTC().Add(-time.Second)
				raw, err := json.Marshal(attempt)
				if err != nil {
					t.Fatal(err)
				}
				d.Annotations[annotation] = string(raw)
				if _, err := deployments.Update(ctx, d, metav1.UpdateOptions{}); err != nil {
					t.Fatal(err)
				}
			}
			for replica := int32(3); replica <= 5; replica++ {
				d := advance(replica, false)
				publishPod(d, int(replica-2))
			}
			if worker == "quarantined" {
				advance(5, true)
			} else {
				advance(6, false)
			}
		})
	}
}

func TestDefaultRuntimeScaleMySQLLargeBaselineRoundTrip(t *testing.T) {
	ctx := context.Background()
	sess, _, _ := runtimeScaleMySQLFixture(t, "hermes", "hermes-runtime")
	uids := make([]string, 10000)
	for i := range uids {
		uids[i] = fmt.Sprintf("10000000-0000-0000-0000-%012d", i)
	}
	if err := sess.TxContext(ctx, func(tx db.Session) error {
		return repository.RecordRuntimeScaleStartupBaseline(ctx, tx, "deployment", "", "00000000-0000-0000-0000-000000000001", uids)
	}, nil); err != nil {
		t.Fatal(err)
	}
	members, found, err := repository.RuntimeScaleStartupBaseline(ctx, sess, "deployment", "00000000-0000-0000-0000-000000000001")
	if err != nil || !found || !reflect.DeepEqual(members, uids) {
		t.Fatalf("10,000-member baseline failed round trip: found=%v members=%d err=%v", found, len(members), err)
	}
}

func TestRuntimeScaleSourceCursorBoundsOnePass(t *testing.T) {
	s := &RuntimeScheduler{}
	target := k8s.RuntimePodScaleTarget{Namespace: "runtime", DeploymentName: "hermes-runtime"}
	candidates := []models.RuntimePod{{PodName: "a"}, {PodName: "b"}, {PodName: "c"}, {PodName: "d"}, {PodName: "e"}}
	for index, want := range []string{"ab", "cd", "ea", "bc"} {
		probes := s.nextScaleSourceCandidates(target, candidates)
		if len(probes) != 2 || probes[0].PodName+probes[1].PodName != want {
			t.Fatalf("pass %d selected %+v, wanted %s", index, probes, want)
		}
	}
}
