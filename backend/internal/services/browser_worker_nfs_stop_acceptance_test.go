package services

import (
	"clawreef/internal/repository"
	cmk8s "clawreef/internal/services/k8s"
	"context"
	"encoding/json"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"os"
	"strings"
	"testing"
	"time"
)

func TestNFSBrowserHistoricalDeploymentRealKubernetesRemainsFenced(t *testing.T) {
	path := os.Getenv("CM_NFS_BROWSER_KUBECONFIG")
	if path == "" {
		t.Skip("requires isolated browser-profile-acceptance fixture in clawmanager-nfs-lab")
	}
	config, err := clientcmd.BuildConfigFromFlags("", path)
	if err != nil {
		t.Fatal(err)
	}
	kube, err := kubernetes.NewForConfig(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAWMANAGER_BROWSER_WORKER_NAMESPACE", "clawmanager-nfs-lab")
	sess := upgradeFlowDB(t)
	migration, err := os.ReadFile("../db/migrations/100_add_nfs_external_writer_tracking.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range strings.Split(string(migration), ";") {
		if strings.TrimSpace(q) != "" {
			if _, err := sess.SQL().Exec(q); err != nil {
				t.Fatal(err)
			}
		}
	}
	repo := repository.NewBrowserWorkerRepository(sess)
	registry := repo.(repository.BrowserWriterRegistry)
	admitted, err := kube.AppsV1().Deployments("clawmanager-nfs-lab").Get(t.Context(), "clawbrowser-12", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	inventory, err := kube.CoreV1().Pods("clawmanager-nfs-lab").List(t.Context(), metav1.ListOptions{LabelSelector: "app=clawbrowser,instance-id=12"})
	if err != nil {
		t.Fatal(err)
	}
	evidence := nfsBrowserStopEvidence{Admitted: true, DeploymentUID: string(admitted.UID), Pods: map[string]nfsBrowserPodEvidence{}}
	for _, pod := range inventory.Items {
		evidence.Pods[string(pod.UID)] = nfsBrowserPodEvidence{Name: pod.Name}
	}
	encoded, _ := json.Marshal(evidence)
	if err = registry.StoreBrowserWriter(t.Context(), 12, 0, string(encoded)); err != nil {
		t.Fatal(err)
	}
	worker := &BrowserWorkerService{repo: repo, client: &cmk8s.Client{Clientset: kube, RuntimeSandboxProfile: cmk8s.RuntimeNFSRootlessProfile}}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	if err = worker.stopNFSBrowserWriter(ctx, 12, false); err != errNFSBrowserLegacyFence {
		t.Fatal("historical Deployment was not fenced", err)
	}
	current, err := kube.AppsV1().Deployments("clawmanager-nfs-lab").Get(ctx, "clawbrowser-12", metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if current.UID != admitted.UID || *current.Spec.Replicas != *admitted.Spec.Replicas {
		t.Fatal("historical controller mutated")
	}
	raw, _, err := registry.LoadBrowserWriter(ctx, 12)
	if err != nil {
		t.Fatal(err)
	}
	var receipt nfsBrowserStopEvidence
	if json.Unmarshal([]byte(raw), &receipt) != nil || receipt.Stopped {
		t.Fatal("unknown historical writer was declared stopped")
	}
	t.Log("historical Deployment remains fenced; no controller mutation or completed writer receipt")
}
