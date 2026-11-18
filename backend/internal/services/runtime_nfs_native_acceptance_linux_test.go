//go:build linux

package services

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

// This opt-in companion runs all six public CM native maintenance methods
// against installed OpenClaw and the real NFS, Kubernetes and slot workers.
func nfsAcceptanceNativeMaintenance(t *testing.T, ctx context.Context, cfg *rest.Config, kube kubernetes.Interface, pod *corev1.Pod, client *slotRuntimeAgentClient, authority *RuntimeSecurityAuthority, endpoint string, id, generation, port int, stop func(context.Context, int) error) {
	t.Helper()
	const namespace = "clawmanager-nfs-lab"
	workspace := fmt.Sprintf("/workspaces/openclaw/user-9001/instance-%d", id)
	runAs := func(script string) string {
		t.Helper()
		output, err := nfsAcceptanceExec(ctx, cfg, kube, pod.Name, "slot-0", []string{"/usr/bin/setpriv", "--reuid=" + strconv.Itoa(200000+id), "--regid=" + strconv.Itoa(200000+id), "--clear-groups", "/usr/local/bin/node", "-e", script, workspace})
		if err != nil {
			t.Fatalf("private native fixture failed: %v %s", err, output)
		}
		return strings.TrimSpace(output)
	}
	runAs(`const fs=require('fs'),p=require('path'),root=process.argv[1],dir=p.join(root,'home/.openclaw/agents/main/sessions');fs.mkdirSync(dir,{recursive:true,mode:0o700});const id='c0a11ace-2026-4000-8000-000000000001';const file=p.join(dir,id+'.jsonl');const records=[{type:'session',version:3,id,timestamp:'2026-11-05T00:00:00.000Z',cwd:root},{type:'message',id:'message1',timestamp:'2026-11-05T00:00:01.000Z',message:{role:'user',content:[{type:'text',text:'private acceptance question'}],timestamp:1793836801000}},{type:'message',id:'message2',parentId:'message1',timestamp:'2026-11-05T00:00:02.000Z',message:{role:'assistant',content:[{type:'text',text:'private acceptance reply'}],api:'openai-responses',provider:'auto',model:'auto',stopReason:'stop',usage:{input:1,output:1,totalTokens:2,cost:{input:0,output:0,total:0}},timestamp:1793836802000}}];fs.writeFileSync(file,records.map(x=>JSON.stringify(x)).join('\n')+'\n',{mode:0o600});fs.writeFileSync(p.join(dir,'sessions.json'),JSON.stringify({'agent:main:main':{sessionId:id,updatedAt:1793836802000,sessionFile:file,chatType:'direct'}}),{mode:0o600});`)
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	req := RuntimeAgentWorkspaceRequest{InstanceID: id, UserID: 9001, Generation: generation, RolloutID: "native-" + hex.EncodeToString(random[:]), UID: 200000 + id, GID: 200000 + id}
	cid := func(p *corev1.Pod, name string) string {
		for _, s := range p.Status.ContainerStatuses {
			if s.Name == name {
				return s.ContainerID
			}
		}
		return ""
	}
	fresh, err := kube.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	sourceCID, idleCID := cid(fresh, "slot-0"), cid(fresh, "slot-1")
	target, _ := url.Parse("http://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(port)) + "/readyz")
	probe := func(c context.Context) error {
		credential, e := authority.NativeGatewayCredentialForTarget(c, id, target)
		if e != nil {
			return e
		}
		request, e := http.NewRequestWithContext(c, http.MethodGet, target.String(), nil)
		if e != nil {
			return e
		}
		request.Header.Set("X-ClawManager-Ingress-Token", credential)
		response, e := (&http.Client{Timeout: 2 * time.Second}).Do(request)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return fmt.Errorf("source readiness HTTP %d", response.StatusCode)
		}
		return nil
	}
	if err = probe(ctx); err != nil {
		t.Fatal(err)
	}
	probeCtx, cancelProbes := context.WithCancel(ctx)
	done := make(chan struct{})
	var probes, failures atomic.Int64
	go func() {
		defer close(done)
		ticker := time.NewTicker(100 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-probeCtx.Done():
				return
			case <-ticker.C:
			}
			e := probe(probeCtx)
			if probeCtx.Err() != nil {
				return
			}
			probes.Add(1)
			if e != nil {
				failures.Add(1)
			}
		}
	}()
	t.Cleanup(func() { cancelProbes(); <-done })
	inventory, err := client.PreflightWorkspace(ctx, endpoint, req)
	if err != nil {
		t.Fatal("actual inventory:", err)
	}
	if inventory.FileCount < 2 {
		t.Fatal("native inventory omitted private fixture")
	}
	compatibility, err := client.PreflightUpgradeCompatibility(ctx, endpoint, req)
	if err != nil {
		t.Fatal("actual compatibility:", err)
	}
	if compatibility.Status != "compatible" || !compatibility.ConfigValidated || !compatibility.DoctorValidated || !compatibility.SessionDryRunValid {
		t.Fatalf("incomplete native compatibility: %+v", compatibility)
	}
	cancelProbes()
	<-done
	fresh, err = kube.CoreV1().Pods(namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if cid(fresh, "slot-0") != sourceCID || cid(fresh, "slot-1") == idleCID || probes.Load() < 3 || failures.Load() != 0 {
		t.Fatalf("readonly maintenance disrupted active source: probes=%d failures=%d", probes.Load(), failures.Load())
	}
	t.Logf("inventory + compatibility passed; active source CID unchanged; idle slot reset; native source probes=%d failures=0", probes.Load())
	if err = stop(ctx, id); err != nil {
		t.Fatal(err)
	}
	req.LeaseToken = hex.EncodeToString(random[:])
	lease := RuntimeAgentWriterLeaseRequest{RuntimeAgentWorkspaceRequest: req, Token: req.LeaseToken, TTLSeconds: 600}
	if err = client.AcquireWriterLease(ctx, endpoint, lease); err != nil {
		t.Fatal("native writer lease:", err)
	}
	migration, err := client.MigrateSessionSQLite(ctx, endpoint, req)
	if err != nil {
		t.Fatal("actual migration:", err)
	}
	if migration.Status != "validated" || migration.SessionCount < 1 || !migration.RollbackAvailable || migration.ArchiveFiles < 1 || migration.OutputSHA256 == "" || migration.SessionCatalogSHA256 == "" {
		t.Fatalf("migration lacks real session/archive proof: %+v", migration)
	}
	status, err := client.SessionSQLiteMigrationStatus(ctx, endpoint, req)
	if err != nil || status.OutputSHA256 != migration.OutputSHA256 || status.SessionCatalogSHA256 != migration.SessionCatalogSHA256 {
		t.Fatalf("migration durable status mismatch: %+v %v", status, err)
	}
	t.Logf("actual native migration validated sessions=%d archive_files=%d archive_bytes=%d output_sha256=%s catalog_sha256=%s", migration.SessionCount, migration.ArchiveFiles, migration.ArchiveBytes, migration.OutputSHA256, migration.SessionCatalogSHA256)
	restored, err := client.RestoreSessionSQLite(ctx, endpoint, req)
	if err != nil {
		t.Fatal("actual restore:", err)
	}
	if restored.Status != "restored" || !restored.ConfigRestored || !restored.StateRestored || restored.PreservedSessionSQLite || restored.PhaseDurationsMS["session_restore"] <= 0 {
		t.Fatalf("restore did not execute real archive rollback: %+v", restored)
	}
	restoreStatus, err := client.SessionSQLiteRestoreStatus(ctx, endpoint, req)
	if err != nil || restoreStatus.OutputSHA256 != restored.OutputSHA256 || restoreStatus.Status != "restored" {
		t.Fatalf("restore durable status mismatch: %+v %v", restoreStatus, err)
	}
	runAs(`const fs=require('fs'),p=require('path'),dir=p.join(process.argv[1],'home/.openclaw/agents/main/sessions'),f=p.join(dir,'c0a11ace-2026-4000-8000-000000000001.jsonl');if(!fs.readFileSync(f,'utf8').includes('private acceptance question'))throw Error('legacy transcript missing after restore');if((fs.statSync(f).mode&0o777)!==0o600)throw Error('private transcript permissions changed');`)
	nfsAcceptanceVerifyNativeRecords(t, ctx, kube, id, generation)
	if err = client.ReleaseWriterLease(ctx, endpoint, lease); err != nil {
		t.Fatal(err)
	}
	t.Log("all six CM native maintenance methods completed; real session migrated and restored with private transcript preserved")
}

func nfsAcceptanceVerifyNativeRecords(t *testing.T, ctx context.Context, kube kubernetes.Interface, id, generation int) {
	t.Helper()
	const namespace = "clawmanager-nfs-lab"
	records, err := kube.CoreV1().ConfigMaps(namespace).List(ctx, metav1.ListOptions{LabelSelector: "clawmanager.io/purpose=lite-native-maintenance"})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, record := range records.Items {
		if record.Data["instance_id"] != strconv.Itoa(id) || record.Data["generation"] != strconv.Itoa(generation) {
			continue
		}
		var intent runtimeSlotStopIntent
		var proof runtimeSlotContainerProof
		if record.Data["state"] != "released" || decodeContainerProof([]byte(record.Data["intent"]), &intent) != nil || decodeContainerProof([]byte(record.Data["termination_proof"]), &proof) != nil || proof.ContainerID == "" || proof.runtimeSlotStopIntent != intent {
			t.Fatal("maintenance record lacks released exact termination proof")
		}
		secret, e := kube.CoreV1().Secrets(namespace).Get(ctx, record.Data["result_secret"], metav1.GetOptions{})
		if e != nil {
			t.Fatal(e)
		}
		var result runtimeNativeMaintenanceResult
		if secret.Immutable == nil || !*secret.Immutable || json.Unmarshal(secret.Data["result"], &result) != nil || !result.Confirmed || result.Uncertain || result.Error != "" || result.Intent != intent {
			t.Fatal("maintenance result is not durable and bound to old-container proof")
		}
		seen[record.Data["action"]] = true
		t.Logf("durable native %s operation=%s oldCID=%s result_secret=%s", record.Data["action"], intent.OperationID, proof.ContainerID, secret.Name)
	}
	for _, action := range []string{"inventory", "compatibility", "migrate", "migration-status", "restore", "restore-status"} {
		if !seen[action] {
			t.Fatalf("missing real native method %s", action)
		}
	}
}
