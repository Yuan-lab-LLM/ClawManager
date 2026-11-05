//go:build linux

package services

import (
	"clawreef/internal/models"
	"clawreef/internal/repository"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	"errors"
	"github.com/upper/db/v4/adapter/mysql"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type browserAcceptanceSQLRegistry struct {
	memoryBrowserWorkerRepo
	repository.BrowserWriterRegistry
}

// Only fresh lab identity 75202 is allowed; uncertain prior fixtures are never
// cleared. The real SQL registry and Kubernetes controllers drive this test.
func TestNFSBrowserStatefulControllerRealKubernetes(t *testing.T) {
	kubePath := os.Getenv("CM_NFS_BROWSER_CONTROLLER_KUBECONFIG")
	if kubePath == "" {
		t.Skip("isolated Kubernetes/NFS lab required")
	}
	const namespace = "clawmanager-nfs-lab"
	const root = "/srv/cm-nfs-storage-client"
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", namespace)
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Minute)
	defer cancel()
	cfg, err := clientcmd.BuildConfigFromFlags("", kubePath)
	if err != nil {
		t.Fatal(err)
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	port := os.Getenv("TEST_RUNTIME_MYSQL_PORT")
	if port == "" {
		t.Skip("isolated MySQL required")
	}
	sqlURL := mysql.ConnectionURL{Host: "127.0.0.1:" + port, User: "root", Password: os.Getenv("TEST_RUNTIME_MYSQL_PASSWORD"), Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	admin, err := mysql.Open(sqlURL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = admin.SQL().Exec("CREATE DATABASE browser_controller_tombstone_75202"); err != nil {
		t.Fatal("fresh durable lab registry required", err)
	}
	admin.Close()
	sqlURL.Database = "browser_controller_tombstone_75202"
	sess, err := mysql.Open(sqlURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close() }) // No DROP: retained names require retained SQL history.
	migration, err := os.ReadFile("../db/migrations/100_add_nfs_external_writer_tracking.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err = sess.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	instance := &models.Instance{ID: 75202, UserID: 9001, Status: "running", RuntimeSecurityProfile: RuntimeSecurityProfile, RuntimeGeneration: 1}
	workspace := RuntimeWorkspacePathWithRoot(root, RuntimeTypeOpenClaw, instance.UserID, instance.ID)
	instance.WorkspacePath = &workspace
	config := browserWorkerDefaults(instance.ID)
	config.BrowserImage = "clawmanager.local/browser-worker-lite@sha256:415908808594f2d1688f68da5b548d878ffb9d9043032553bad4b0c155fdd9cb"
	config.CDPProxyImage = "clawmanager.local/browser-cdp-lite@sha256:3de0666663622b98d44a7965143e1fd1565b3121dc30487e72e73585ea0c814b"
	registry := repository.NewBrowserWorkerRepository(sess).(repository.BrowserWriterRegistry)
	repo := &browserAcceptanceSQLRegistry{memoryBrowserWorkerRepo: memoryBrowserWorkerRepo{}, BrowserWriterRegistry: registry}
	worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile, WorkspacePVCClaimName: "workspace"}}
	name := worker.name(instance.ID)
	stop := func() error {
		for {
			err := worker.stopNFSBrowserWriter(ctx, instance.ID, false)
			if err == nil {
				return nil
			}
			if ctx.Err() != nil || (!errors.Is(err, context.DeadlineExceeded) && !strings.Contains(err.Error(), "appeared during stop")) {
				return err
			}
			time.Sleep(100 * time.Millisecond)
		}
	}

	if _, err = kube.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("fresh lab Deployment identity required", err)
	}
	if _, err = kube.AppsV1().StatefulSets(namespace).Get(ctx, name, metav1.GetOptions{}); !apierrors.IsNotFound(err) {
		t.Fatal("fresh lab StatefulSet identity required", err)
	}

	if _, _, err = worker.loadNFSBrowserRegistry(ctx, instance.ID, true); err != nil {
		t.Fatal(err)
	}
	repo.config = config
	// Provision only a new lab leaf on the isolated server export. Existing homes
	// and browser journals are untouched; all new directories start private.
	provision := exec.CommandContext(ctx, "python3", "-c", `import os
p="/data/clawmanager-nfs-lab/openclaw/user-9001/instance-75202"
os.mkdir(p,0o700);os.chown(p,275202,275202)
`)
	if out, err := provision.CombinedOutput(); err != nil {
		t.Fatalf("fresh fixture provisioning: %v %s", err, out)
	}
	business := func(script string) string {
		t.Helper()
		command := exec.CommandContext(ctx, "setpriv", "--reuid=275202", "--regid=275202", "--clear-groups", "--", "python3", "-c", script)
		out, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("business credential fixture: %v %s", err, out)
		}
		return strings.TrimSpace(string(out))
	}
	business(`import os
p="/srv/cm-nfs-storage-client/openclaw/user-9001/instance-75202/home/.openclaw/browser-worker"
for name in ["chromium/Default/Extensions","Downloads"]: os.makedirs(p+"/"+name,mode=0o700)
for name in ["chromium/cookie-retention-marker","chromium/Default/Extensions/extension-marker","Downloads/download-marker"]:
 with open(p+"/"+name,"w") as f:f.write("private-retained")
 os.chmod(p+"/"+name,0o600)
`)
	oldPrefix := "openclaw/user-9001/instance-75202/home/.openclaw/browser-worker"
	desired := desiredBrowserWorkerDeployment(name, namespace, browserWorkerLabels(instance), oldPrefix, "workspace", 1, config)
	if err = configureNFSBrowserWorker(desired, instance); err != nil {
		t.Fatal(err)
	}
	desired.Spec.Template.Spec.Containers[0].SecurityContext = &corev1.SecurityContext{AllowPrivilegeEscalation: boolptr(false), Capabilities: &corev1.Capabilities{Drop: []corev1.Capability{"ALL"}, Add: []corev1.Capability{"SETUID", "SETGID", "CHOWN", "DAC_OVERRIDE", "FOWNER", "KILL"}}}
	if _, err = worker.ensureAuthSecret(ctx, instance); err != nil {
		t.Fatal(err)
	}
	waitPod := func(expectedName string) *corev1.Pod {
		t.Helper()
		for ctx.Err() == nil {
			pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=clawbrowser,instance-id=75202"})
			if err != nil {
				t.Fatal(err)
			}
			for _, pod := range pods.Items {
				if expectedName != "" && pod.Name != expectedName {
					continue
				}
				if pod.DeletionTimestamp != nil {
					continue
				}
				ready := pod.Status.Phase == corev1.PodRunning
				for _, c := range pod.Status.ContainerStatuses {
					ready = ready && c.Ready
				}
				if ready && len(pod.Status.ContainerStatuses) == len(pod.Spec.Containers) {
					return pod.DeepCopy()
				}
			}
			time.Sleep(time.Second)
		}
		t.Fatal("browser readiness timed out", ctx.Err())
		return nil
	}
	originalToken, err := worker.AuthToken(ctx, instance.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Offline private data has never been exposed to a historical controller.
	// The explicit never-admitted registry allows its scoped import before start.
	if err = stop(); err != nil {
		t.Fatal("new controlled profile stop", err)
	}
	t.Log("explicit never-admitted profile; offline private data import requires no historical-controller inference")
	beforeMigration := business(`import os,json,hashlib
p="/srv/cm-nfs-storage-client/openclaw/user-9001/instance-75202/home/.openclaw/browser-worker"
r={n:os.stat(p+"/"+n).st_ino for n in ["chromium","Downloads"]}
for base,dirs,files in os.walk(p+"/chromium"):
 for name in files:
  if name=="Cookies":r["cookies_sha256"]=hashlib.sha256(open(base+"/"+name,"rb").read()).hexdigest()
print(json.dumps(r,sort_keys=True))
`)

	// Migration uses the same credential-scoped atomic rename as production.
	if ended, err := PrepareNFSBrowserProfile(ctx, root, instance); err != nil || !ended {
		t.Fatalf("atomic scoped migration: %v %v", ended, err)
	}
	afterMigration := business(`import os,json,hashlib
p="/srv/cm-nfs-storage-client/browsers/user-9001/instance-75202"
r={"chromium":os.stat(p+"/profile").st_ino,"Downloads":os.stat(p+"/downloads").st_ino}
for base,dirs,files in os.walk(p+"/profile"):
 for name in files:
  if name=="Cookies":r["cookies_sha256"]=hashlib.sha256(open(base+"/"+name,"rb").read()).hexdigest()
print(json.dumps(r,sort_keys=True))
`)
	if beforeMigration != afterMigration {
		t.Fatal("atomic migration changed directory identity or cookies bytes")
	}

	desired.Spec.Template.Spec.Containers[0].Lifecycle = &corev1.Lifecycle{PreStop: &corev1.LifecycleHandler{Exec: &corev1.ExecAction{Command: []string{"/bin/sh", "-c", "sleep 25"}}}}
	for {
		err = worker.applyNFSBrowserWorkload(ctx, instance, desired.DeepCopy())
		if err == nil {
			break
		}
		if !strings.Contains(err.Error(), "removal is pending") || ctx.Err() != nil {
			t.Fatal(err)
		}
		time.Sleep(time.Second)
	}
	firstOwner, err := worker.getBrowserWorkload(ctx, instance.ID, "StatefulSet")
	if err != nil {
		t.Fatal(err)
	}
	first := waitPod(firstOwner.Name + "-0")
	if err = worker.observeNFSBrowserPods(ctx, instance.ID); err != nil {
		t.Fatal(err)
	}
	firstProof, _, err := worker.loadNFSBrowserRegistry(ctx, instance.ID, false)
	if err != nil {
		t.Fatal(err)
	}
	set, err := kube.AppsV1().StatefulSets(namespace).Get(ctx, firstOwner.Name, metav1.GetOptions{})
	if err != nil || set.Spec.UpdateStrategy.Type != appsv1.OnDeleteStatefulSetStrategyType {
		t.Fatal("missing OnDelete StatefulSet", err)
	}
	// A template revision must leave the active Pod untouched.
	set.Spec.Template.Annotations["lab-only-template-update"] = "on-delete"
	if _, err = kube.AppsV1().StatefulSets(namespace).Update(ctx, set, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(3 * time.Second)
	current, err := kube.CoreV1().Pods(namespace).Get(ctx, first.Name, metav1.GetOptions{})
	if err != nil || current.UID != first.UID || current.DeletionTimestamp != nil {
		t.Fatal("template update autonomously rolled active writer", err)
	}
	if err = kube.CoreV1().Pods(namespace).Delete(ctx, first.Name, metav1.DeleteOptions{Preconditions: &metav1.Preconditions{UID: &first.UID}}); err != nil {
		t.Fatal(err)
	}
	// Allow multiple controller reconciliations while preStop retains a live CID.
	for i := 0; i < 8; i++ {
		pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=clawbrowser,instance-id=75202"})
		if err != nil {
			t.Fatal(err)
		}
		if len(pods.Items) != 1 || pods.Items[0].UID != first.UID || pods.Items[0].DeletionTimestamp == nil {
			t.Fatalf("autonomous replacement before old proof: %+v", pods.Items)
		}
		if pods.Items[0].Status.ContainerStatuses[0].State.Running == nil {
			t.Fatal("retained test writer was not live during hold")
		}
		time.Sleep(time.Second)
	}
	t.Log("Kubernetes1.35 retained terminating ordinal with live CID for 8 controller-observation seconds; no replacement")
	// Reconstruct CM and SQL repository to prove this is not process-local fencing.
	repo.BrowserWriterRegistry = repository.NewBrowserWorkerRepository(sess).(repository.BrowserWriterRegistry)
	worker = &BrowserWorkerService{repo: repo, client: worker.client}
	if err = stop(); err != nil {
		t.Fatal("positive ordinal stop after restart", err)
	}
	stopped, _, err := worker.loadNFSBrowserRegistry(ctx, instance.ID, false)
	if err != nil || !stopped.Stopped || !stopped.Pods[string(first.UID)].Complete {
		t.Fatal("ordinal termination not persisted", err)
	}
	tombstone, err := kube.CoreV1().Pods(namespace).Get(ctx, first.Name, metav1.GetOptions{})
	if err != nil || tombstone.UID != first.UID || len(tombstone.Finalizers) == 0 {
		t.Fatal("retirement freed old ordinal name", err)
	}
	replay := first.DeepCopy()
	replay.ResourceVersion = ""
	replay.UID = ""
	replay.CreationTimestamp = metav1.Time{}
	replay.DeletionTimestamp = nil
	replay.Status = corev1.PodStatus{}
	if _, err = kube.CoreV1().Pods(namespace).Create(ctx, replay, metav1.CreateOptions{}); !apierrors.IsAlreadyExists(err) {
		t.Fatal("late old-epoch Create was not rejected by occupied name", err)
	}

	if err = worker.applyNFSBrowserWorkload(ctx, instance, desired.DeepCopy()); err != nil {
		t.Fatal(err)
	}
	secondOwner, err := worker.getBrowserWorkload(ctx, instance.ID, "StatefulSet")
	if err != nil {
		t.Fatal(err)
	}
	second := waitPod(secondOwner.Name + "-0")
	if second.UID == first.UID {
		t.Fatal("no new Pod incarnation")
	}
	if err = worker.observeNFSBrowserPods(ctx, instance.ID); err != nil {
		t.Fatal(err)
	}
	next, _, err := worker.loadNFSBrowserRegistry(ctx, instance.ID, false)
	if err != nil || next.Epoch == firstProof.Epoch || next.DeploymentUID == firstProof.DeploymentUID || len(next.Retired) != 1 || next.Retired[0].ControllerName != firstOwner.Name {
		t.Fatal("new execution admission identity invalid", err)
	}
	if token, err := worker.AuthToken(ctx, instance.ID); err != nil || token != originalToken {
		t.Fatal("migration changed authentication")
	}
	business(`import os
p="/srv/cm-nfs-storage-client/browsers/user-9001/instance-75202"
for name in ["profile/cookie-retention-marker","profile/Default/Extensions/extension-marker","downloads/download-marker"]:
 st=os.stat(p+"/"+name)
 assert st.st_uid==275202 and st.st_gid==275202 and st.st_mode&0o777==0o600
 assert open(p+"/"+name).read()=="private-retained"
print("private profile/extension/download data preserved")
`)
	t.Log("new ordinal UID/epoch admitted only after persisted old CID proof; scoped offline-imported data and auth retained")
	if err = worker.deleteResources(ctx, instance.ID); err != nil {
		t.Fatal("positive fixture retirement", err)
	}
	evidence, _, err := worker.loadNFSBrowserRegistry(ctx, instance.ID, false)
	if err != nil || !evidence.Stopped {
		t.Fatal("retirement lost durable receipt", err)
	}
	sets, err := kube.AppsV1().StatefulSets(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=clawbrowser,instance-id=75202"})
	if err != nil || len(sets.Items) != 2 {
		t.Fatal("retired controllers were garbage-collected", err)
	}
	pods, err := kube.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{LabelSelector: "app=clawbrowser,instance-id=75202"})
	if err != nil || len(pods.Items) != 2 {
		t.Fatal("retired name tombstones were garbage-collected", err)
	}
	if err = worker.verifyNFSBrowserTombstones(ctx, instance.ID, evidence); err != nil {
		t.Fatal(err)
	}
	t.Log("two terminal deleting ordinal tombstones and zero-replica controllers retained; delayed old-name Create returns AlreadyExists; SQL database browser_controller_tombstone_75202 retained")
	encoded, _ := json.Marshal(evidence)
	if err = os.WriteFile("/root/cm-nfs-multislot-20261105/browser-controller-tombstone-actual-receipt.json", encoded, 0600); err != nil {
		t.Fatal(err)
	}
}

// Read/reconcile existing positive tombstones after a fresh CM process starts.
// Unlike the creation fixture this never drops/recreates its durable SQL rows.
func TestNFSBrowserRetainedTombstonesRealKubernetes(t *testing.T) {
	path := os.Getenv("CM_NFS_BROWSER_CONTROLLER_KUBECONFIG")
	if path == "" {
		t.Skip("isolated retained browser fixture required")
	}
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "clawmanager-nfs-lab")
	cfg, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		t.Fatal(err)
	}
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: "127.0.0.1:" + os.Getenv("TEST_RUNTIME_MYSQL_PORT"), User: "root", Password: os.Getenv("TEST_RUNTIME_MYSQL_PASSWORD"), Database: "browser_controller_tombstone_75202", Options: map[string]string{"parseTime": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	repo := &browserAcceptanceSQLRegistry{BrowserWriterRegistry: repository.NewBrowserWorkerRepository(sess).(repository.BrowserWriterRegistry)}
	worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
	proof, revision, err := worker.loadNFSBrowserRegistry(t.Context(), 75202, false)
	if err != nil || !proof.Stopped || len(proof.Retired) != 1 {
		t.Fatal("retained SQL history unavailable", err)
	}
	if err = worker.stopNFSBrowserWriter(t.Context(), 75202, false); err != nil {
		t.Fatal(err)
	}
	_, after, err := worker.loadNFSBrowserRegistry(t.Context(), 75202, false)
	if err != nil || after != revision {
		t.Fatal("already-proven tombstones mutated during restart reconciliation", err)
	}
	t.Log("fresh CM reload validated both permanent terminal+deleting names, controllerUID/PodUID/CID hashes, zero replicas, SQL history without mutation")
}
