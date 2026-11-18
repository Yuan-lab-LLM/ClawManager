package services

import (
	"bytes"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeauth"
	"clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func validationLeaseFixture(t *testing.T) (*RuntimeSecurityAuthority, *RuntimeScheduler, *fake.Clientset, *models.RuntimeRollout, k8s.RuntimePoolValidationScope) {
	t.Helper()
	ctx := context.Background()
	sess := upgradeFlowDB(t)
	repo := repository.NewRuntimeRolloutRepository(sess)
	image := "repo@sha256:" + strings.Repeat("a", 64)
	sources := `{"runtime/source":"` + image + `"}`
	r := &models.RuntimeRollout{RuntimeType: RuntimeTypeOpenClaw, TargetImageRef: image, SourceImagesJSON: &sources, Status: "running", Phase: RuntimeUpgradePhaseEmptyPoolReset}
	if e := repo.Create(ctx, r); e != nil {
		t.Fatal(e)
	}
	source := k8s.BuildRuntimeDeployment(k8s.RuntimeDeploymentSpec{Name: "source", Namespace: "runtime", RuntimeType: "openclaw", Image: image, Replicas: 1, SlotCount: 8, AgentTokenSecretName: "clawmanager-lite-agent", WorkspaceMountPath: "/workspaces", PodUserNamespaces: true, NetworkIsolation: true})
	source.UID = "source-uid"
	kube := fake.NewSimpleClientset(source)
	deployments := k8s.NewRuntimeDeploymentService(kube).(k8s.RuntimePoolValidationService)
	name := replacementProbeName(r.ID, 0)
	if e := deployments.EnsurePoolValidation(ctx, k8s.RuntimeRolloutTarget{Namespace: "runtime", Name: source.Name, UID: string(source.UID)}, name, image, strconv.FormatInt(r.ID, 10)); e != nil {
		t.Fatal(e)
	}
	d, _ := kube.AppsV1().Deployments("runtime").Get(ctx, name, metav1.GetOptions{})
	d.UID = "probe-controller-uid"
	kube.AppsV1().Deployments("runtime").Update(ctx, d, metav1.UpdateOptions{})
	yes := true
	rs := &appsv1.ReplicaSet{ObjectMeta: metav1.ObjectMeta{Name: name + "-rs", Namespace: "runtime", UID: "probe-rs-uid", OwnerReferences: []metav1.OwnerReference{{Kind: "Deployment", Name: name, UID: d.UID, Controller: &yes}}}, Spec: appsv1.ReplicaSetSpec{Template: *d.Spec.Template.DeepCopy()}}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: name + "-pod", Namespace: "runtime", UID: "probe-pod-uid", Labels: d.Spec.Template.Labels, Annotations: d.Spec.Template.Annotations, OwnerReferences: []metav1.OwnerReference{{Kind: "ReplicaSet", Name: rs.Name, UID: rs.UID, Controller: &yes}}}, Spec: *d.Spec.Template.Spec.DeepCopy(), Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "127.0.0.1"}}
	kube.AppsV1().ReplicaSets("runtime").Create(ctx, rs, metav1.CreateOptions{})
	kube.CoreV1().Pods("runtime").Create(ctx, pod, metav1.CreateOptions{})
	if _, e := sess.SQL().Exec(`CREATE TABLE runtime_rollout_checkpoints(rollout_id BIGINT PRIMARY KEY,state_json MEDIUMTEXT NOT NULL,updated_at DATETIME(6))`); e != nil {
		t.Fatal(e)
	}
	checkpoint := map[string]any{"kind": "openclaw-pool-replacement", "version": 2, "stage": "validate", "stage_started": time.Now().UTC(), "targets": []k8s.RuntimeRolloutTarget{{Namespace: "runtime", Name: source.Name, UID: string(source.UID), Image: image}}, "probe_pod_uid": string(pod.UID), "probe_deployment_uid": string(d.UID)}
	raw, _ := json.Marshal(checkpoint)
	if _, e := sess.SQL().Exec(`INSERT INTO runtime_rollout_checkpoints(rollout_id,state_json,updated_at)VALUES(?,?,NOW())`, r.ID, string(raw)); e != nil {
		t.Fatal(e)
	}
	a, e := NewRuntimeSecurityAuthority(kube, "runtime", &securityInstances{}, []byte(strings.Repeat("k", 32)))
	if e != nil {
		t.Fatal(e)
	}
	s := &RuntimeScheduler{rolloutRepo: repo, runtimeNamespace: "runtime", workspaceRoot: "/workspaces"}
	return a, s, kube, r, k8s.RuntimePoolValidationScope{Namespace: "runtime", DeploymentName: name, DeploymentUID: string(d.UID), SourceName: source.Name, SourceUID: string(source.UID), Operation: strconv.FormatInt(r.ID, 10), PodUID: string(pod.UID), Endpoint: "https://127.0.0.1:19090", ImageRef: image, WorkspaceRoot: "/workspaces"}
}

func TestPoolValidationLeaseAllowsNativeStartupButNeverBusinessAuthority(t *testing.T) {
	a, s, kube, r, scope := validationLeaseFixture(t)
	ctx := context.Background()
	configure, ok := any(a).(interface{ ConfigurePoolValidationLeases(*RuntimeScheduler) error })
	if !ok {
		t.Fatal("owned isolated probe cannot acquire deny-all startup broker credentials")
	}
	if e := configure.ConfigurePoolValidationLeases(s); e != nil {
		t.Fatal(e)
	}
	identity := runtimeauth.Identity{InstanceID: poolProbeInstanceID, UserID: 1, Generation: 1, PodUID: scope.PodUID}
	peer := netip.MustParseAddr("127.0.0.1")
	lease, e := a.IssueLease(ctx, identity, peer)
	if e != nil || lease.Identity() != identity {
		t.Fatal("valid native startup lease was denied", e)
	}
	if !lease.ExpiresAt.After(time.Now().Add(14*time.Minute)) || lease.ExpiresAt.After(time.Now().Add(15*time.Minute)) {
		t.Fatal("invalid bounded startup lease")
	}
	for _, token := range []string{lease.ModelToken, lease.EgressToken, lease.ServicesToken} {
		if !strings.HasPrefix(token, "v1.") || len(strings.Split(token, ".")) != 4 {
			t.Fatal("startup credential wire format drift")
		}
		for _, audience := range []string{runtimeauth.AudienceModel, runtimeauth.AudienceEgress, runtimeauth.AudienceServices} {
			if _, e = a.Capabilities().Verify(ctx, token, audience); e == nil {
				t.Fatal("startup-only credential gained business authority")
			}
		}
	}
	for _, audience := range []string{runtimeauth.AudienceModel, runtimeauth.AudienceEgress, runtimeauth.AudienceServices} {
		if _, e = a.Capabilities().Issue(ctx, identity, audience); e == nil {
			t.Fatal("validation changed normal capability issuance")
		}
	}
	if _, e = a.CurrentIdentity(ctx, identity.InstanceID); e == nil {
		t.Fatal("validation became a business assignment")
	}
	if _, e = a.NativeGatewayCredential(ctx, identity.InstanceID); e == nil {
		t.Fatal("validation enabled business credential exchange")
	}
	wrong := identity
	wrong.Generation++
	if _, e = a.IssueLease(ctx, wrong, peer); e == nil {
		t.Fatal("wrong generation admitted")
	}
	if _, e = a.IssueLease(ctx, identity, netip.MustParseAddr("127.0.0.2")); e == nil {
		t.Fatal("wrong peer admitted")
	}
	if _, e = s.rolloutRepo.GetByID(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	d, _ := kube.AppsV1().Deployments(scope.Namespace).Get(ctx, scope.DeploymentName, metav1.GetOptions{})
	d.Spec.Template.Spec.Volumes[0].VolumeSource = corev1.VolumeSource{PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{ClaimName: "business"}}
	kube.AppsV1().Deployments(scope.Namespace).Update(ctx, d, metav1.UpdateOptions{})
	if _, e = a.IssueLease(ctx, identity, peer); e == nil {
		t.Fatal("mutated probe received startup credentials")
	}
}

func TestPoolValidationLeaseRejectsInactiveAndUnpinnedProof(t *testing.T) {
	a, s, kube, r, scope := validationLeaseFixture(t)
	ctx := context.Background()
	if e := a.ConfigurePoolValidationLeases(s); e != nil {
		t.Fatal(e)
	}
	store := s.rolloutRepo.(rolloutCheckpointStore)
	original, e := store.LoadRolloutCheckpoint(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	identity := runtimeauth.Identity{InstanceID: poolProbeInstanceID, UserID: 1, Generation: 1, PodUID: scope.PodUID}
	peer := netip.MustParseAddr("127.0.0.1")
	for _, kind := range []string{"terminal-rollout", "wrong-stage", "no-pod-proof", "no-controller-proof", "wrong-controller-proof", "expired-proof", "future-proof", "bad-source", "bad-user", "bad-generation", "bad-peer", "cancelled"} {
		t.Run(kind, func(t *testing.T) {
			var p map[string]any
			if e = json.Unmarshal([]byte(original), &p); e != nil {
				t.Fatal(e)
			}
			request := identity
			remote := peer
			callCtx := ctx
			switch kind {
			case "terminal-rollout":
				if e = s.rolloutRepo.UpdateStatus(ctx, r.ID, "error", nil, nil, nil); e != nil {
					t.Fatal(e)
				}
			case "wrong-stage":
				p["stage"] = "cleanup_failed"
			case "no-pod-proof":
				p["probe_pod_uid"] = ""
			case "no-controller-proof":
				p["probe_deployment_uid"] = ""
			case "wrong-controller-proof":
				p["probe_deployment_uid"] = "replacement"
			case "expired-proof":
				p["stage_started"] = time.Now().Add(-6 * time.Minute)
			case "future-proof":
				p["stage_started"] = time.Now().Add(2 * time.Minute)
			case "bad-source":
				p["targets"] = []k8s.RuntimeRolloutTarget{{Namespace: "runtime", Name: "other", UID: "other", Image: scope.ImageRef}}
			case "bad-user":
				request.UserID++
			case "bad-generation":
				request.Generation++
			case "bad-peer":
				remote = netip.MustParseAddr("127.0.0.2")
			case "cancelled":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				callCtx = cancelled
			}
			raw, _ := json.Marshal(p)
			if e = store.SaveRolloutCheckpoint(ctx, r.ID, string(raw)); e != nil {
				t.Fatal(e)
			}
			lease, e := a.IssueLease(callCtx, request, remote)
			if e == nil || lease.ModelToken != "" || lease.EgressToken != "" || lease.ServicesToken != "" {
				t.Fatal("unverified startup scope obtained credentials", kind, e)
			}
			if e = s.rolloutRepo.UpdateStatus(ctx, r.ID, "running", nil, nil, nil); e != nil {
				t.Fatal(e)
			}
			if e = store.SaveRolloutCheckpoint(ctx, r.ID, original); e != nil {
				t.Fatal(e)
			}
		})
	}
	cms, _ := kube.CoreV1().ConfigMaps("runtime").List(ctx, metav1.ListOptions{})
	secrets, _ := kube.CoreV1().Secrets("runtime").List(ctx, metav1.ListOptions{})
	if len(cms.Items) != 0 || len(secrets.Items) != 0 {
		t.Fatal("validation authority published business objects")
	}
}

func TestPoolValidationLeaseActualNativeBrokerHandshake(t *testing.T) {
	helper := os.Getenv("TEST_RUNTIME_NETBROKER_HELPER")
	if helper == "" {
		t.Skip("actual native netbroker executable required")
	}
	a, s, _, _, scope := validationLeaseFixture(t)
	if e := a.ConfigurePoolValidationLeases(s); e != nil {
		t.Fatal(e)
	}
	control := strings.Repeat("c", 32)
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/api/v1/runtime/security/lease" || r.Header.Get("X-ClawManager-Control-Token") != control {
			http.Error(w, "denied", 403)
			return
		}
		var request RuntimeSecurityLease
		d := json.NewDecoder(r.Body)
		d.DisallowUnknownFields()
		if d.Decode(&request) != nil {
			http.Error(w, "invalid", 400)
			return
		}
		host, _, e := net.SplitHostPort(r.RemoteAddr)
		if e != nil {
			http.Error(w, "peer", 403)
			return
		}
		peer, e := netip.ParseAddr(host)
		if e != nil {
			http.Error(w, "peer", 403)
			return
		}
		lease, e := a.IssueLease(r.Context(), request.Identity(), peer)
		if e != nil {
			http.Error(w, "denied", 403)
			return
		}
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(lease)
	}))
	defer server.Close()
	input, _ := json.Marshal(map[string]any{"base": server.URL, "control_token": control, "identity": map[string]any{"instance_id": poolProbeInstanceID, "user_id": 1, "generation": 1, "pod_uid": scope.PodUID}})
	command := exec.Command(helper)
	command.Stdin = bytes.NewReader(input)
	raw, e := command.CombinedOutput()
	if e != nil {
		t.Fatalf("actual native broker could not acquire startup credentials: %v", e)
	}
	var result struct {
		OK          bool `json:"ok"`
		Acquire     bool `json:"acquire"`
		Credentials bool `json:"credentials"`
	}
	if json.Unmarshal(raw, &result) != nil || !result.OK || !result.Acquire || !result.Credentials || calls.Load() != 2 {
		t.Fatal("native broker handshake contract failed", calls.Load())
	}
}
