package services

import (
	"clawreef/internal/models"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestOpenCodeNativeContractRejectsUnknownIdentity(t *testing.T) {
	good := map[string]string{
		"io.clawmanager.runtime.type": "opencode", "io.clawmanager.upgrade.strategy": "opencode-native-preserve-v1", "io.clawmanager.upgrade.protocol": "opencode-preserve-v1",
		"io.clawmanager.opencode.version": "1.18.13", "io.clawmanager.opencode.revision": "a105350812f05f914c768e468559dbd6bd508d8e",
		"io.clawmanager.opencode.binary-sha256": openCodeNativeBinarySHA256, "io.clawmanager.opencode.journal-sha256": openCodeNativeJournalSHA256, "io.clawmanager.opencode.base-image": "registry.local/opencode@sha256:" + strings.Repeat("c", 64),
	}
	if _, err := validateOpenCodeNativeContract(good, nil); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"io.clawmanager.runtime.type", "io.clawmanager.upgrade.strategy", "io.clawmanager.upgrade.protocol", "io.clawmanager.opencode.version", "io.clawmanager.opencode.revision", "io.clawmanager.opencode.binary-sha256", "io.clawmanager.opencode.journal-sha256", "io.clawmanager.opencode.base-image"} {
		t.Run(key, func(t *testing.T) {
			labels := map[string]string{}
			for k, v := range good {
				labels[k] = v
			}
			labels[key] = "unknown"
			if _, err := validateOpenCodeNativeContract(labels, nil); err == nil {
				t.Fatal("unknown contract accepted")
			}
		})
	}
	for _, key := range []string{"OPENCODE_DB", "OPENCODE_CHANNEL", "OPENCODE_DATA_DIR", "XDG_DATA_HOME", "XDG_CONFIG_HOME", "XDG_STATE_HOME"} {
		if _, err := validateOpenCodeNativeContract(good, map[string]string{key: "/other"}); err == nil {
			t.Fatalf("path override %s accepted", key)
		}
	}
}

func TestOpenCodeCapsuleGateRequiresEveryProof(t *testing.T) {
	good := RuntimeAgentSessionSQLiteMigration{InstanceID: 7, Status: "validated", OutputSHA256: strings.Repeat("a", 64), PreservationSHA256: strings.Repeat("b", 64), SessionCatalogSHA256: strings.Repeat("c", 64), DataPreserved: true, RollbackAvailable: true}
	if err := validateOpenCodeCapsule(7, &good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RuntimeAgentSessionSQLiteMigration){func(r *RuntimeAgentSessionSQLiteMigration) { r.InstanceID++ }, func(r *RuntimeAgentSessionSQLiteMigration) { r.Status = "completed" }, func(r *RuntimeAgentSessionSQLiteMigration) { r.OutputSHA256 = "" }, func(r *RuntimeAgentSessionSQLiteMigration) { r.PreservationSHA256 = "" }, func(r *RuntimeAgentSessionSQLiteMigration) { r.SessionCatalogSHA256 = "" }, func(r *RuntimeAgentSessionSQLiteMigration) { r.DataPreserved = false }, func(r *RuntimeAgentSessionSQLiteMigration) { r.RollbackAvailable = false }} {
		r := good
		change(&r)
		if err := validateOpenCodeCapsule(7, &r); err == nil {
			t.Fatal("incomplete capsule accepted")
		}
	}
}

func TestOpenCodeOCIRequiresActualBoundedBodyDigest(t *testing.T) {
	body := []byte(`{"proof":"actual"}`)
	sum := sha256.Sum256(body)
	digest := "sha256:" + hex.EncodeToString(sum[:])
	for _, tc := range []struct {
		name, header string
		body         []byte
		limit        int64
		good         bool
	}{
		{"exact", "", body, 128, true}, {"matching header", digest, body, 128, true}, {"spoofed header", digest, []byte(`{"proof":"changed"}`), 128, false}, {"wrong header", "sha256:" + strings.Repeat("f", 64), body, 128, false}, {"truncated", digest, body[:8], 128, false}, {"oversized", digest, body, 8, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if tc.header != "" {
					w.Header().Set("Docker-Content-Digest", tc.header)
				}
				w.Write(tc.body)
			}))
			defer server.Close()
			var result map[string]string
			err := fetchOpenCodeRegistryJSON(context.Background(), server.Client(), server.URL, digest, tc.limit, &result)
			if (err == nil) != tc.good {
				t.Fatalf("accepted=%v error=%v", err == nil, err)
			}
		})
	}
}

func TestOpenCodeReceiptsBindRolloutGenerationAndIndependentStatus(t *testing.T) {
	req := RuntimeAgentWorkspaceRequest{RolloutID: "71", InstanceID: 7, Generation: 3}
	digest := strings.Repeat("a", 64)
	stamp := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	compatibility := RuntimeAgentUpgradeCompatibility{RolloutID: req.RolloutID, Generation: req.Generation, InstanceID: req.InstanceID, Status: "compatible", ConfigOriginalSHA256: digest, ConfigTargetSHA256: digest, ConfigBytes: 100, SessionBytes: 1024, StateBytes: 2048, AvailableBytes: 1 << 20, CheckedAt: stamp, ProbeOutputSHA256: digest, ConfigValidated: true, DoctorValidated: true, SessionDryRunValid: true}
	if err := validateOpenCodeCompatibilityReceipt(req, &compatibility); err != nil {
		t.Fatal(err)
	}
	migration := RuntimeAgentSessionSQLiteMigration{RolloutID: req.RolloutID, Generation: req.Generation, InstanceID: req.InstanceID, Status: "validated", ArchiveBytes: 1024, ArchiveFiles: 4, ConfigOriginalSHA256: digest, ConfigTargetSHA256: digest, CompletedAt: stamp, OutputSHA256: digest, PreservationSHA256: digest, SessionCatalogSHA256: digest, DataPreserved: true, RollbackAvailable: true}
	if err := validateOpenCodeCapsuleReceipt(req, &migration, &compatibility); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RuntimeAgentSessionSQLiteMigration){func(r *RuntimeAgentSessionSQLiteMigration) { r.RolloutID = "other" }, func(r *RuntimeAgentSessionSQLiteMigration) { r.Generation++ }, func(r *RuntimeAgentSessionSQLiteMigration) { r.ArchiveBytes = 0 }, func(r *RuntimeAgentSessionSQLiteMigration) { r.ArchiveFiles = 0 }, func(r *RuntimeAgentSessionSQLiteMigration) { r.ConfigOriginalSHA256 = strings.Repeat("b", 64) }} {
		r := migration
		change(&r)
		if err := validateOpenCodeCapsuleReceipt(req, &r, &compatibility); err == nil {
			t.Fatal("foreign or incomplete native capsule receipt accepted")
		}
	}
	status := migration
	status.SessionCatalogSHA256 = strings.Repeat("b", 64)
	if sameOpenCodeCapsuleProof(&migration, &status) {
		t.Fatal("different status catalog was accepted")
	}
	restore := RuntimeAgentSessionSQLiteRestore{RolloutID: req.RolloutID, Generation: req.Generation, InstanceID: req.InstanceID, Status: "restored", OutputSHA256: digest, ConfigRestored: true, StateRestored: true, PreservedSessionSQLite: true, CompletedAt: stamp}
	if err := validateOpenCodeRestoreReceipt(req, &restore); err != nil {
		t.Fatal(err)
	}
	restore.Generation++
	if err := validateOpenCodeRestoreReceipt(req, &restore); err == nil {
		t.Fatal("foreign restore generation accepted")
	}
}

func TestOpenCodeNativeMaintenanceRejectsReusedEndpointBeforeDispatch(t *testing.T) {
	endpoint := "https://10.0.0.2:19090"
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "replacement", Namespace: "runtime", UID: "replacement-uid", Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1", "clawmanager.io/sandbox-profile": "nfs-rootless-v1"}}, Status: corev1.PodStatus{PodIP: "10.0.0.2"}}
	kube := fake.NewSimpleClientset(pod)
	client := &slotRuntimeAgentClient{kube: kube, namespace: "runtime"}
	if _, err := client.nativeMaintenanceProfile(context.Background(), endpoint, "frozen-old-uid"); err == nil {
		t.Fatal("replacement UID at reused endpoint was accepted")
	}
	for _, action := range kube.Actions() {
		if action.GetVerb() != "get" && action.GetVerb() != "list" {
			t.Fatalf("mutated %s before matching native Pod UID", action.GetResource().Resource)
		}
	}
	if current, err := client.nativeMaintenanceProfile(context.Background(), endpoint, "replacement-uid"); err != nil || current == nil || current.UID != pod.UID {
		t.Fatal("exact expected UID failed", err)
	}
}

func TestOpenCodeActivateHTTPBindsFrozenUIDAndPreservesLegacyBody(t *testing.T) {
	calls := []map[string]string{}
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/openclaw/upgrade/activate" {
			t.Error("unexpected activation route")
		}
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		calls = append(calls, body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	pod := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Name: "target", Namespace: "runtime", UID: "frozen-target-uid", Labels: map[string]string{"clawmanager.io/runtime-protocol": "container-slots-v1", "clawmanager.io/sandbox-profile": "nfs-rootless-v1"}}, Status: corev1.PodStatus{PodIP: parsed.Hostname()}}
	kube := fake.NewSimpleClientset(pod)
	base := NewRuntimeAgentClientWithHTTPClient("fixture-token", server.Client()).(*runtimeAgentHTTPClient)
	client := &slotRuntimeAgentClient{runtimeAgentHTTPClient: base, kube: kube, namespace: "runtime"}
	target := models.RuntimePod{PodUID: stringPtrOrNil("frozen-target-uid"), AgentEndpoint: &server.URL}
	if err = client.openCodeActivateUpgrade(context.Background(), target, "71"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 1 || calls[0]["expected_pod_uid"] != "frozen-target-uid" || calls[0]["rollout_id"] != "71" {
		t.Fatalf("activation lost UID identity: %+v", calls)
	}
	if err = base.ActivateUpgrade(context.Background(), server.URL, "legacy-72"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 || len(calls[1]) != 1 || calls[1]["rollout_id"] != "legacy-72" {
		t.Fatalf("legacy activation body changed: %+v", calls)
	}
	pod.UID = "replacement-uid"
	if _, err = kube.CoreV1().Pods("runtime").Update(context.Background(), pod, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	if err = client.openCodeActivateUpgrade(context.Background(), target, "71"); err == nil || len(calls) != 2 {
		t.Fatal("replacement UID reached activation HTTP")
	}
}

func TestOpenCodePlannedMaintenanceCountsAllFrozenRunningInstances(t *testing.T) {
	if actual := openCodePlannedUnavailability([]openCodeUpgradeInstance{{Status: "running"}, {Status: "stopped"}, {Status: "running"}}); actual != 2 {
		t.Fatalf("planned unavailable=%d", actual)
	}
	if actual := openCodePlannedUnavailability(nil); actual != 0 {
		t.Fatal(actual)
	}
}
