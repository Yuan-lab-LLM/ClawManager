//go:build linux

package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/runtimeauth"
	k8sservice "clawreef/internal/services/k8s"
	upperdb "github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

type nfsAcceptanceInstances struct {
	mu        sync.RWMutex
	instances map[int]*models.Instance
}

func (s *nfsAcceptanceInstances) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	i := s.instances[id]
	if i == nil {
		return nil, errors.New("absent acceptance identity")
	}
	copy := *i
	return &copy, nil
}
func (s *nfsAcceptanceInstances) ProtectRuntimeGeneration(ctx context.Context, id, generation int, profile string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	i := s.instances[id]
	if i == nil || i.RuntimeGeneration != generation || profile != RuntimeSecurityProfile {
		return errRuntimeSecurity
	}
	i.RuntimeSecurityProfile = profile
	return ctx.Err()
}
func (s *nfsAcceptanceInstances) set(id, generation int, status string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.instances[id].RuntimeGeneration = generation
	s.instances[id].Status = status
}

func nfsAcceptanceDatabase(t *testing.T) (upperdb.Session, *bool) {
	t.Helper()
	port := os.Getenv("TEST_RUNTIME_SECURITY_MYSQL_PORT")
	if port != "33309" {
		t.Fatal("acceptance requires the dedicated diagnostic MySQL localhost33309")
	}
	u := mysql.ConnectionURL{Host: "127.0.0.1:" + port, User: "root", Password: os.Getenv("TEST_RUNTIME_SECURITY_MYSQL_PASSWORD"), Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, err := mysql.Open(u)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("cm_nfs_slot_accept_%d", time.Now().UnixNano())
	if _, err = root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		root.Close()
		t.Fatal(err)
	}
	u.Database = name
	s, err := mysql.Open(u)
	if err != nil {
		root.Close()
		t.Fatal(err)
	}
	retain := new(bool)
	t.Cleanup(func() {
		s.Close()
		if *retain {
			t.Logf("retaining diagnostic SQL evidence database %s", name)
		} else {
			root.SQL().Exec("DROP DATABASE " + name)
		}
		root.Close()
	})
	if _, err = s.SQL().Exec("CREATE TABLE instances(id INT PRIMARY KEY,user_id INT NOT NULL,runtime_generation INT NOT NULL,status VARCHAR(32) NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	for _, statement := range []string{"CREATE TABLE teams(id INT PRIMARY KEY,user_id INT,status VARCHAR(32))", "CREATE TABLE team_members(id INT PRIMARY KEY,team_id INT NOT NULL,instance_id INT,status VARCHAR(32))"} {
		if _, err = s.SQL().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range []string{"068_add_workspace_write_intents.sql", "099_add_workspace_write_fences.sql", "100_add_nfs_external_writer_tracking.sql"} {
		raw, err := os.ReadFile(filepath.Join("..", "db", "migrations", file))
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(statement) != "" {
				if _, err = s.SQL().Exec(statement); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return s, retain
}

func nfsAcceptanceExec(ctx context.Context, cfg *rest.Config, kube kubernetes.Interface, pod, container string, command []string) (string, error) {
	req := kube.CoreV1().RESTClient().Post().Namespace("clawmanager-nfs-lab").Resource("pods").Name(pod).SubResource("exec").VersionedParams(&corev1.PodExecOptions{Container: container, Command: command, Stdout: true, Stderr: true}, scheme.ParameterCodec)
	executor, err := remotecommand.NewSPDYExecutor(cfg, http.MethodPost, req.URL())
	if err != nil {
		return "", err
	}
	var stdout, stderr bytes.Buffer
	err = executor.StreamWithContext(ctx, remotecommand.StreamOptions{Stdout: &stdout, Stderr: &stderr})
	if stdout.Len()+stderr.Len() > 4096 {
		return "", errors.New("acceptance diagnostic output exceeded limit")
	}
	return stdout.String() + stderr.String(), err
}

func TestNFSRealSlotLifecycleAcceptance(t *testing.T) {
	if os.Getenv("CLAWMANAGER_NFS_LIFECYCLE_ACCEPT") != "1" {
		t.Skip("requires explicitly enabled ordinary-Pod NFS diagnostic environment")
	}
	const namespace = "clawmanager-nfs-lab"
	podName := os.Getenv("CLAWMANAGER_NFS_ACCEPT_POD")
	runtimeType := os.Getenv("CLAWMANAGER_NFS_ACCEPT_RUNTIME")
	if runtimeType == "" {
		runtimeType = RuntimeTypeOpenCode
	}
	switch runtimeType {
	case RuntimeTypeOpenCode, RuntimeTypeOpenClaw, RuntimeTypeHermes, RuntimeTypeDeepSeekHarness:
	default:
		t.Fatal("unsupported native acceptance runtime")
	}
	if !strings.HasPrefix(podName, runtimeType+"-runtime-") {
		t.Fatal("acceptance requires an explicitly selected matching lab runtime Pod")
	}
	credentialPath := "/root/cm-nfs-multislot-20261105/lab-credentials.json"
	info, err := os.Stat(credentialPath)
	if err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("node-private lab credentials are required")
	}
	raw, err := os.ReadFile(credentialPath)
	if err != nil {
		t.Fatal(err)
	}
	var credentials struct {
		Control string `json:"control"`
	}
	if json.Unmarshal(raw, &credentials) != nil || len(credentials.Control) < 32 {
		t.Fatal("invalid lab control credentials")
	}
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		kubeconfig = "/etc/kubernetes/admin.conf"
	}
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatal(err)
	}
	cfg.QPS, cfg.Burst = 50, 100
	kube, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Minute)
	defer cancel()
	pod, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if pod.Labels["clawmanager.io/sandbox-profile"] != "nfs-rootless-v1" || pod.Labels["clawmanager.io/runtime-security"] != RuntimeSecurityProfile || pod.Spec.HostUsers != nil && !*pod.Spec.HostUsers || pod.Spec.ShareProcessNamespace != nil && *pod.Spec.ShareProcessNamespace {
		t.Fatal("selected Pod is not the ordinary isolated NFS profile")
	}
	endpoint := "https://" + net.JoinHostPort(pod.Status.PodIP, "19090")
	trust, err := k8sservice.RuntimeSlotTLSConfig(ctx, kube, namespace)
	if err != nil {
		t.Fatal(err)
	}
	client := NewSlotRuntimeAgentClientWithHTTPClient(credentials.Control, kube, namespace, &http.Client{Transport: &http.Transport{TLSClientConfig: trust}, Timeout: 30 * time.Second}).(*slotRuntimeAgentClient)
	if runtimeType == RuntimeTypeOpenClaw || runtimeType == RuntimeTypeHermes {
		// These native entrypoints require the production public trust lease.
		// Authorize only this isolated acceptance controller's exact node IP.
		refreshPolicy := func(policyCtx context.Context) error {
			var policy struct {
				Revision string `json:"revision"`
			}
			if e := client.do(policyCtx, http.MethodGet, endpoint, "/v1/proxy-policy", nil, &policy); e != nil {
				return e
			}
			return client.do(policyCtx, http.MethodPut, endpoint, "/v1/proxy-policy", map[string]any{"namespace": namespace, "expected_revision": policy.Revision, "proxies": []string{"10.130.81.54"}}, &policy)
		}
		if err = refreshPolicy(ctx); err != nil {
			t.Fatal(err)
		}
		policyCtx, stopPolicy := context.WithCancel(ctx)
		policyDone := make(chan struct{})
		go func() {
			defer close(policyDone)
			ticker := time.NewTicker(15 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-policyCtx.Done():
					return
				case <-ticker.C:
					_ = refreshPolicy(policyCtx)
				}
			}
		}()
		t.Cleanup(func() { stopPolicy(); <-policyDone })
	}
	baseID := 71001
	if value := os.Getenv("CLAWMANAGER_NFS_ACCEPT_BASE_ID"); value != "" {
		baseID, err = strconv.Atoi(value)
		if err != nil || baseID < 71001 || baseID > 99998 {
			t.Fatal("invalid isolated acceptance base instance ID")
		}
	}
	ids := []int{baseID, baseID + 1}
	ports := []int{0, 0}
	for i := range ports {
		for _, container := range pod.Spec.Containers {
			if container.Name != fmt.Sprintf("slot-%d", i) {
				continue
			}
			for _, env := range container.Env {
				if env.Name == "RUNTIME_GATEWAY_PORT_START" {
					ports[i], _ = strconv.Atoi(env.Value)
				}
			}
		}
		if ports[i] < 1024 {
			t.Fatal("missing exact native slot port")
		}
	}
	source := &nfsAcceptanceInstances{instances: map[int]*models.Instance{}}
	generations := make([]int, len(ids))
	for i, id := range ids {
		generation := 1
		old, readErr := kube.CoreV1().ConfigMaps(namespace).Get(ctx, fmt.Sprintf("lite-instance-%d", id), metav1.GetOptions{})
		if readErr == nil {
			if old.Data["desired_state"] != "stopped" || old.Data["container_stop_released"] != "true" {
				t.Fatalf("prior diagnostic instance %d still owns an unconfirmed writer", id)
			}
			previous, e := strconv.Atoi(old.Data["generation"])
			if e != nil {
				t.Fatal(e)
			}
			generation = previous + 1
		} else if !apierrors.IsNotFound(readErr) {
			t.Fatal(readErr)
		}
		generations[i] = generation
		source.instances[id] = &models.Instance{ID: id, UserID: 9001, Type: runtimeType, InstanceMode: "lite", RuntimeType: "gateway", Status: "creating", RuntimeGeneration: generation, RuntimeSecurityProfile: RuntimeSecurityProfile}
	}
	key := make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		t.Fatal(err)
	}
	authority, err := NewRuntimeSecurityAuthority(kube, namespace, source, key)
	if err != nil {
		t.Fatal(err)
	}
	if err = ConfigureSlotRuntimeSecurity(client, authority); err != nil {
		t.Fatal(err)
	}
	// OpenCode has no separate Browser Worker. OpenClaw's real external browser
	// lifecycle is verified by its dedicated positive-termination acceptance.
	if err = ConfigureSlotExternalWriterCleanup(client, func(context.Context, runtimeauth.Identity) error { return nil }); err != nil {
		t.Fatal(err)
	}
	database, retainDatabase := nfsAcceptanceDatabase(t)
	fences := repository.NewWorkspaceWriteFenceRepository(database)
	if err = ConfigureSlotWorkspaceWriteFence(client, func(ctx context.Context, id runtimeauth.Identity) error {
		return fences.Fence(ctx, id.InstanceID, id.UserID, id.Generation, id.PodUID)
	}); err != nil {
		t.Fatal(err)
	}
	if err = ConfigureSlotWorkspaceWriteActivation(client, func(ctx context.Context, id runtimeauth.Identity) error {
		return fences.Activate(ctx, id.InstanceID, id.UserID, id.Generation, id.PodUID)
	}); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if _, err = database.SQL().Exec("INSERT INTO instances VALUES(?,?,?,'creating')", id, 9001, generations[i]); err != nil {
			t.Fatal(err)
		}
	}
	stop := func(stopCtx context.Context, id int) error {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		lastPending := ""
		for {
			cm, e := kube.CoreV1().ConfigMaps(namespace).Get(stopCtx, fmt.Sprintf("lite-instance-%d", id), metav1.GetOptions{})
			if apierrors.IsNotFound(e) {
				return nil
			}
			if e != nil {
				return e
			}
			if cm.Data["desired_state"] == "stopped" && cm.Data["container_stop_released"] == "true" {
				return nil
			}
			if e = client.DeleteGateway(stopCtx, endpoint, cm.Data["gateway_id"]); e == nil {
				return nil
			}
			if pending := e.Error(); pending != lastPending {
				t.Logf("instance %d awaiting confirmed stop: %s", id, pending)
				lastPending = pending
			}
			select {
			case <-stopCtx.Done():
				return fmt.Errorf("instance %d stop remains unconfirmed: %w", id, e)
			case <-ticker.C:
			}
		}
	}
	t.Cleanup(func() {
		for _, id := range ids {
			cleanup, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			if e := stop(cleanup, id); e != nil {
				*retainDatabase = true
				t.Errorf("diagnostic cleanup retained writer evidence: %v", e)
			}
			cancel()
		}
	})
	start := func(i, generation int) {
		id := ids[i]
		source.set(id, generation, "creating")
		if _, e := database.SQL().Exec("UPDATE instances SET runtime_generation=?,status='creating' WHERE id=?", generation, id); e != nil {
			t.Fatal(e)
		}
		request := RuntimeAgentCreateGatewayRequest{InstanceID: id, UserID: 9001, Generation: generation, AgentType: runtimeType, PodUID: string(pod.UID), UID: 200000 + id, GID: 200000 + id, GatewayPort: ports[i], WorkspacePath: fmt.Sprintf("/workspaces/%s/user-9001/instance-%d", runtimeType, id), SecurityProfile: RuntimeSecurityProfile, Environment: map[string]string{"CLAWMANAGER_LLM_MODEL": "auto", "CLAWMANAGER_TEAM_ENABLED": "false"}}
		if _, e := client.CreateGateway(ctx, endpoint, request); e != nil {
			t.Fatal(e)
		}
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			state, e := client.GatewayState(ctx, endpoint, fmt.Sprintf("gw-%d-%d", id, generation))
			if e == nil && state.State == "running" {
				break
			}
			if e == nil && state.State == "error" {
				var detail struct {
					Error   string `json:"error_message"`
					Failure string `json:"failure_class"`
				}
				_ = client.do(ctx, http.MethodGet, endpoint, "/v1/gateways/"+state.GatewayID, nil, &detail)
				t.Fatalf("native diagnostic instance %d failed startup: %s %s", id, detail.Error, detail.Failure)
			}
			select {
			case <-ctx.Done():
				t.Fatal("native diagnostic startup timed out")
			case <-ticker.C:
			}
		}
		source.set(id, generation, "running")
		if _, e := database.SQL().Exec("UPDATE instances SET status='running' WHERE id=?", id); e != nil {
			t.Fatal(e)
		}
		t.Logf("native instance %d generation%d running in slot-%d", id, generation, i)
	}
	start(0, generations[0])
	if runtimeType != RuntimeTypeOpenCode {
		if runtimeType == RuntimeTypeOpenClaw && os.Getenv("CLAWMANAGER_NFS_ACCEPT_MAINTENANCE") == "1" {
			*retainDatabase = true // Preserve SQL fencing if any native maintenance outcome is unknown.
			nfsAcceptanceNativeMaintenance(t, ctx, cfg, kube, pod, client, authority, endpoint, ids[0], generations[0], ports[0], stop)
			*retainDatabase = false
		}
		if err = stop(ctx, ids[0]); err != nil {
			t.Fatal(err)
		}
		_, state, _, saved, e := client.assignments.ReadContainerStop(ctx, endpoint, fmt.Sprintf("gw-%d-%d", ids[0], generations[0]))
		var proof runtimeSlotContainerProof
		if e != nil || state != "stopped" || decodeContainerProof(saved, &proof) != nil || proof.ContainerID == "" {
			t.Fatal("native stop lacks durable exact container proof")
		}
		t.Logf("native %s start/healthy/stop confirmed oldCID=%s", runtimeType, proof.ContainerID)
		return
	}
	start(1, generations[1])
	nativeTarget, _ := url.Parse("http://" + net.JoinHostPort(pod.Status.PodIP, strconv.Itoa(ports[1])) + "/global/health")
	probe := func(probeCtx context.Context) error {
		credential, e := authority.NativeGatewayCredentialForTarget(probeCtx, ids[1], nativeTarget)
		if e != nil {
			return e
		}
		request, e := http.NewRequestWithContext(probeCtx, http.MethodGet, nativeTarget.String(), nil)
		if e != nil {
			return e
		}
		request.Header.Set("X-ClawManager-Ingress-Token", credential)
		request.SetBasicAuth("opencode", credential)
		response, e := (&http.Client{Timeout: 2 * time.Second}).Do(request)
		if e != nil {
			return e
		}
		defer response.Body.Close()
		io.Copy(io.Discard, io.LimitReader(response.Body, 4096))
		if response.StatusCode != 200 {
			return fmt.Errorf("native B health HTTP %d", response.StatusCode)
		}
		return nil
	}
	if err = probe(ctx); err != nil {
		t.Fatal(err)
	}
	probeCtx, stopProbes := context.WithCancel(ctx)
	probeDone := make(chan struct{})
	var probes, probeFailures atomic.Int64
	var failureMu sync.Mutex
	firstFailure := ""
	go func() {
		defer close(probeDone)
		ticker := time.NewTicker(50 * time.Millisecond)
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
				probeFailures.Add(1)
				failureMu.Lock()
				if firstFailure == "" {
					firstFailure = e.Error()
				}
				failureMu.Unlock()
			}
		}
	}()
	t.Cleanup(func() { stopProbes(); <-probeDone })
	before, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		t.Fatal(err)
	}
	containerID := func(p *corev1.Pod, name string) string {
		for _, status := range p.Status.ContainerStatuses {
			if status.Name == name {
				return status.ContainerID
			}
		}
		return ""
	}
	oldA, oldB := containerID(before, "slot-0"), containerID(before, "slot-1")
	if oldA == "" || oldB == "" {
		t.Fatal("missing live native container identities")
	}
	workspace := fmt.Sprintf("/workspaces/opencode/user-9001/instance-%d", ids[0])
	counter := workspace + "/lifecycle-detached-counter"
	command := []string{"/usr/bin/env", "-i", "PATH=/usr/bin:/bin", "/usr/bin/setpriv", "--reuid=" + strconv.Itoa(200000+ids[0]), "--regid=" + strconv.Itoa(200000+ids[0]), "--clear-groups", "/bin/sh", "-c", `( ( setsid /bin/sh -c 'while :; do printf x >> "$1"; sleep 0.05; done' writer "$1" </dev/null >/dev/null 2>&1 & ) & )`, "writer", counter}
	if _, err = nfsAcceptanceExec(ctx, cfg, kube, podName, "slot-0", command); err != nil {
		t.Fatal(err)
	}
	size := func() int {
		out, e := nfsAcceptanceExec(ctx, cfg, kube, podName, "slot-0", []string{"/usr/bin/env", "-i", "PATH=/usr/bin:/bin", "/usr/bin/setpriv", "--reuid=" + strconv.Itoa(200000+ids[0]), "--regid=" + strconv.Itoa(200000+ids[0]), "--clear-groups", "/usr/bin/stat", "-c", "%s", counter})
		if e != nil {
			t.Fatal(e)
		}
		value, e := strconv.Atoi(strings.TrimSpace(out))
		if e != nil {
			t.Fatal(e)
		}
		return value
	}
	time.Sleep(200 * time.Millisecond)
	first := size()
	time.Sleep(200 * time.Millisecond)
	if size() <= first {
		t.Fatal("detached diagnostic writer did not run")
	}
	orphan := models.WorkspaceWriteIntent{InstanceID: ids[0], OwnerID: 9001, Generation: generations[0], Operation: strings.Repeat("f", 32), Action: "edit"}
	if _, err = database.SQL().Exec("INSERT INTO instance_workspace_write_intents VALUES(?,?,?,?,?,UTC_TIMESTAMP(6))", orphan.Operation, orphan.InstanceID, orphan.OwnerID, orphan.Generation, orphan.Action); err != nil {
		t.Fatal(err)
	}
	orphanPending := true
	t.Cleanup(func() {
		if orphanPending {
			settle, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = repository.NewWorkspaceWriteRepository(database).Complete(settle, orphan)
		}
	})
	if err = client.DeleteGateway(ctx, endpoint, fmt.Sprintf("gw-%d-%d", ids[0], generations[0])); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("unknown platform writer did not block actual reset: %v", err)
	}
	blocked, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil || containerID(blocked, "slot-0") != oldA {
		t.Fatal("slot reset despite unresolved external writer")
	}
	if err = repository.NewWorkspaceWriteRepository(database).Complete(ctx, orphan); err != nil {
		t.Fatal(err)
	}
	orphanPending = false
	t.Log("unresolved management write blocked reset before old container termination")
	if err = stop(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	_, state, _, saved, err := client.assignments.ReadContainerStop(ctx, endpoint, fmt.Sprintf("gw-%d-%d", ids[0], generations[0]))
	var proof runtimeSlotContainerProof
	if err != nil || state != "stopped" || decodeContainerProof(saved, &proof) != nil || proof.ContainerID != oldA {
		t.Fatal("stop lacked durable exact old-container proof")
	}
	t.Log("matching Kubernetes Terminated record and new-worker ledger reconciliation confirmed source stop")
	// Stop proof is authoritative for the old execution. Kubelet may publish
	// the replacement's Running/containerID status after worker reconciliation.
	// Wait only for visibility of the replacement; timeout is never stop proof.
	visibleCtx, cancelVisible := context.WithTimeout(ctx, 60*time.Second)
	defer cancelVisible()
	visibleTicker := time.NewTicker(250 * time.Millisecond)
	defer visibleTicker.Stop()
	var after *corev1.Pod
	var lastStatuses string
	for {
		after, err = kube.CoreV1().Pods(namespace).Get(visibleCtx, podName, metav1.GetOptions{})
		if err == nil {
			encodedStatuses, _ := json.Marshal(after.Status.ContainerStatuses)
			lastStatuses = string(encodedStatuses)
			var runningA, runningB bool
			for _, status := range after.Status.ContainerStatuses {
				if status.Name == "slot-0" {
					runningA = status.State.Running != nil
				}
				if status.Name == "slot-1" {
					runningB = status.State.Running != nil
				}
			}
			if after.UID != before.UID || containerID(after, "slot-1") != oldB || !runningB {
				t.Fatalf("peer changed while awaiting source replacement: oldA=%s oldB=%s podUID=%s oldPodUID=%s statuses=%s", oldA, oldB, after.UID, before.UID, lastStatuses)
			}
			if runningA && containerID(after, "slot-0") != "" && containerID(after, "slot-0") != oldA {
				break
			}
		}
		select {
		case <-visibleCtx.Done():
			t.Fatalf("replacement Running status not visible after confirmed stop: oldA=%s oldB=%s proofCID=%s statuses=%s lastError=%v: %v", oldA, oldB, proof.ContainerID, lastStatuses, err, visibleCtx.Err())
		case <-visibleTicker.C:
		}
	}
	t.Logf("replacement Running status visible after exact stop proof: oldA=%s newA=%s unchangedB=%s", oldA, containerID(after, "slot-0"), oldB)
	stoppedSize := size()
	time.Sleep(300 * time.Millisecond)
	if size() != stoppedSize {
		t.Fatal("detached writer survived confirmed old container termination")
	}
	start(0, generations[0]+1)
	stopProbes()
	<-probeDone
	if probes.Load() < 3 || probeFailures.Load() != 0 {
		failureMu.Lock()
		reason := firstFailure
		failureMu.Unlock()
		t.Fatalf("untouched B lost native/auth requests: probes=%d failures=%d first=%s", probes.Load(), probeFailures.Load(), reason)
	}
	latest, err := kube.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil || containerID(latest, "slot-1") != oldB {
		t.Fatal("B container changed during A reuse")
	}
	t.Logf("native OpenCode A reset+reuse confirmed oldCID=%s newCID=%s B unchanged=%s continuous authenticated native probes=%d failures=0", oldA, containerID(latest, "slot-0"), oldB, probes.Load())
}
