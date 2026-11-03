package services

import (
	"context"
	"encoding/base64"
	"net/http"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
	cmk8s "clawreef/internal/services/k8s"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

type runtimeBrowserConfigSource struct{ config *models.InstanceBrowserWorker }

func (s *runtimeBrowserConfigSource) GetContext(ctx context.Context, id int) (*models.InstanceBrowserWorker, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.config == nil {
		return nil, nil
	}
	copy := *s.config
	return &copy, nil
}

func TestRuntimeBrowserTargetBoundToOwnerGenerationAndWorker(t *testing.T) {
	id := runtimeauth.Identity{InstanceID: 7, UserID: 3, Generation: 2, PodUID: "pod"}
	instances := &securityInstances{instance: &models.Instance{ID: 7, UserID: 3, Type: RuntimeTypeOpenClaw, InstanceMode: InstanceModeLite, RuntimeType: "gateway", RuntimeGeneration: 2, RuntimeSecurityProfile: RuntimeSecurityProfile, Status: "running"}}
	configs := &runtimeBrowserConfigSource{config: &models.InstanceBrowserWorker{InstanceID: 7, Enabled: true, Generation: 4, ObservedGeneration: 4, Status: models.BrowserWorkerStatusReady}}
	kube := fake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "clawbrowser-7", Namespace: "clawmanager-system"}, Data: map[string][]byte{"access-token": []byte(strings.Repeat("worker-key-", 4))}})
	worker := &BrowserWorkerService{client: &cmk8s.Client{Clientset: kube, Namespace: "clawmanager"}}
	adapter, err := NewRuntimeBrowserAdapter(instances, configs, worker)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := adapter.Resolve(context.Background(), id); err == nil {
		t.Fatal("browser released credential without protected network policy")
	}
	if err := ConfigureBrowserRuntimeSecurity(worker, func(context.Context, int) (runtimeauth.Identity, error) {
		return id, nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := worker.ensureResources(context.Background(), instances.instance, configs.config); err != nil {
		t.Fatal(err)
	}
	deployment, err := kube.AppsV1().Deployments(worker.namespace()).Get(context.Background(), worker.name(7), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	deployment.Status.ObservedGeneration = deployment.Generation
	deployment.Status.Replicas, deployment.Status.UpdatedReplicas, deployment.Status.ReadyReplicas = 1, 1, 1
	if _, err := kube.AppsV1().Deployments(worker.namespace()).UpdateStatus(context.Background(), deployment, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	_, err = kube.CoreV1().Pods(worker.namespace()).Create(context.Background(), &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "worker-7", Namespace: worker.namespace(), UID: "worker-pod-uid", Labels: deployment.Spec.Template.Labels, Annotations: deployment.Spec.Template.Annotations}, Spec: deployment.Spec.Template.Spec, Status: corev1.PodStatus{Phase: corev1.PodRunning, PodIP: "10.244.1.7", Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}}}, metav1.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	bound, err := adapter.Resolve(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if got := bound.TargetURL().String(); got != "http://10.244.1.7:9222" {
		t.Fatal("worker target is not fixed")
	}
	r, _ := http.NewRequest("GET", bound.TargetURL().String(), nil)
	bound.Authenticate(r)
	user, password, ok := r.BasicAuth()
	if !ok || user != "openclaw" || password != strings.Repeat("worker-key-", 4) {
		t.Fatal("trusted worker credential unavailable")
	}
	gui, err := worker.ProtectedGUITarget(context.Background(), instances.instance)
	if err != nil || gui.String() != "http://10.244.1.7:5801" {
		t.Fatal("GUI credential target not pinned to own worker")
	}
	verifier, err := NewRuntimeBrowserEgressVerifier(worker.networkIdentity, adapter)
	if err != nil {
		t.Fatal(err)
	}
	good := "bw1." + base64.RawURLEncoding.EncodeToString([]byte(browserRuntimeScope(id))) + ".worker-pod-uid." + strings.Repeat("worker-key-", 4)
	if got, err := verifier.Verify(context.Background(), good, runtimeauth.AudienceEgress); err != nil || got != id {
		t.Fatal("current worker egress refused")
	}
	stale := id
	stale.Generation++
	for _, candidate := range []string{good + "wrong", strings.Replace(good, "bw1.", "v1.", 1), strings.Replace(good, ".worker-pod-uid.", ".retired-worker-pod.", 1), "bw1.e30.worker-pod-uid." + strings.Repeat("worker-key-", 4), "bw1." + base64.RawURLEncoding.EncodeToString([]byte(browserRuntimeScope(stale))) + ".worker-pod-uid." + strings.Repeat("worker-key-", 4)} {
		if _, err := verifier.Verify(context.Background(), candidate, runtimeauth.AudienceEgress); err == nil {
			t.Fatal("wrong worker credential accepted")
		}
	}
	if _, err := verifier.Verify(context.Background(), good, runtimeauth.AudienceServices); err == nil {
		t.Fatal("worker key granted service audience")
	}
	service, err := kube.CoreV1().Services(worker.namespace()).Get(context.Background(), worker.name(7), metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	service.Spec.Selector["app"] = "unrelated-worker"
	if _, err := kube.CoreV1().Services(worker.namespace()).Update(context.Background(), service, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if bound.Check(context.Background()) == nil {
		t.Fatal("worker service routing changed without credential revocation")
	}
	if _, err := worker.ProtectedGUITarget(context.Background(), instances.instance); err == nil {
		t.Fatal("GUI still injects credential into altered service")
	}
	service.Spec.Selector["app"] = "clawbrowser"
	if _, err := kube.CoreV1().Services(worker.namespace()).Update(context.Background(), service, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	instances.instance.RuntimeGeneration++
	if bound.Check(context.Background()) == nil {
		t.Fatal("old generation retained browser authority")
	}
	instances.instance.RuntimeGeneration--
	configs.config.Enabled = false
	if bound.Check(context.Background()) == nil {
		t.Fatal("disabled browser retained authority")
	}
	configs.config.Enabled = true
	wrong := id
	wrong.UserID++
	if _, err := adapter.Resolve(context.Background(), wrong); err == nil {
		t.Fatal("wrong owner obtained worker credential")
	}
	wrong = id
	wrong.Generation++
	if _, err := adapter.Resolve(context.Background(), wrong); err == nil {
		t.Fatal("wrong generation obtained worker credential")
	}
	pod, err := kube.CoreV1().Pods(worker.namespace()).Get(context.Background(), "worker-7", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := kube.CoreV1().Pods(worker.namespace()).Delete(context.Background(), pod.Name, metav1.DeleteOptions{}); err != nil {
		t.Fatal(err)
	}
	pod.ResourceVersion = ""
	pod.UID = "replacement-worker"
	if _, err := kube.CoreV1().Pods(worker.namespace()).Create(context.Background(), pod, metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	if bound.Check(context.Background()) == nil {
		t.Fatal("replacement worker retained previous worker connection")
	}
	if _, err := verifier.Verify(context.Background(), good, runtimeauth.AudienceEgress); err == nil {
		t.Fatal("retired worker retained egress credential")
	}
}
