package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
)

type gatewayStateSequence struct {
	states []string
	calls  int
}

type concurrentStopRuntimeAgent struct {
	*fakeRuntimeAgentClient
	bindings *fakeRuntimeBindingRepo
}

func (a *concurrentStopRuntimeAgent) DeleteGateway(ctx context.Context, endpoint, gatewayID string) error {
	if binding := a.bindings.bindings[209]; binding != nil {
		binding.State = RuntimeGatewayBindingCreating
	}
	return nil
}

func (a *concurrentStopRuntimeAgent) GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error) {
	return nil, ErrRuntimeAgentNotFound
}

func (s *gatewayStateSequence) GatewayState(context.Context, string, string) (*RuntimeAgentGatewayState, error) {
	if s.calls >= len(s.states) {
		return nil, ErrRuntimeAgentNotFound
	}
	state := s.states[s.calls]
	s.calls++
	return &RuntimeAgentGatewayState{State: state}, nil
}

func TestWaitForGatewayStoppedAcceptsLegacyStoppedRecord(t *testing.T) {
	reader := &gatewayStateSequence{states: []string{"stopping", "stopped"}}
	confirmed, err := waitForGatewayStopped(context.Background(), reader, "http://agent", "gw-1", time.Second)
	if err != nil || !confirmed {
		t.Fatalf("confirmed=%v err=%v", confirmed, err)
	}
}

func TestWaitForGatewayStoppedAcceptsRemovedRecord(t *testing.T) {
	confirmed, err := waitForGatewayStopped(context.Background(), &gatewayStateSequence{}, "http://agent", "gw-1", time.Second)
	if err != nil || !confirmed {
		t.Fatalf("confirmed=%v err=%v", confirmed, err)
	}
}

func TestStopCandidateGatewayAcceptsConfirmedConcurrentBindingTransition(t *testing.T) {
	bindingRepo := &fakeRuntimeBindingRepo{
		bindings: map[int]*models.InstanceRuntimeBinding{
			209: {InstanceID: 209, RuntimePodID: 9, GatewayID: "gw-209-2", Generation: 2, State: RuntimeGatewayBindingRunning},
		},
		deleteAndReleaseCalls: map[int]int{},
	}
	agent := &concurrentStopRuntimeAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, bindings: bindingRepo}
	service := &RuntimeUpgradeService{bindings: bindingRepo, agent: agent}
	stopped, err := service.stopCandidateGateway(context.Background(), runtimeUpgradeCandidate{
		InstanceID: 209, RuntimePodID: int64Pointer(9), GatewayID: "gw-209-2", Generation: 2,
		BindingState: RuntimeGatewayBindingRunning, AgentEndpoint: "http://runtime", Capabilities: []string{"openclaw.gateway.stop-confirm"},
	})
	if err != nil || !stopped {
		t.Fatalf("stopped=%v err=%v, want confirmed success", stopped, err)
	}
	if bindingRepo.bindings[209] != nil || bindingRepo.deleteAndReleaseCalls[209] != 1 {
		t.Fatalf("concurrent binding was not released: binding=%#v calls=%d", bindingRepo.bindings[209], bindingRepo.deleteAndReleaseCalls[209])
	}
}

func TestUnboundRollbackRequiresPersistedConfirmedSourceIdentity(t *testing.T) {
	podID := int64(9)
	gatewayID := "gw-209-2"
	generation := 2
	item := models.RuntimeUpgradeItem{RuntimePodID: &podID, SourceGatewayID: &gatewayID, SourceGeneration: &generation, State: "migration_started"}
	if !upgradeItemOwnsConfirmedStoppedGateway(item) {
		t.Fatal("confirmed post-quiesce item was rejected")
	}
	for _, state := range []string{"pending", "prepared", "compatibility_checked", "quiescing"} {
		item.State = state
		if upgradeItemOwnsConfirmedStoppedGateway(item) {
			t.Fatalf("pre-quiesce state %s was allowed to recover without a binding", state)
		}
	}
	item.State = "migrated"
	item.SourceGatewayID = nil
	if upgradeItemOwnsConfirmedStoppedGateway(item) {
		t.Fatal("missing source gateway identity was accepted")
	}
}

func TestMissingRollbackSourceIdentityIsDeterministicNotInfrastructureRetryable(t *testing.T) {
	err := fmt.Errorf("OPENCLAW_ROLLBACK_SOURCE_IDENTITY_MISSING: instance 350 has no confirmed persisted source gateway identity; automatic rollback is safely held")
	if runtimeUpgradeInfrastructureRetryable(err) {
		t.Fatal("missing persisted source identity must terminate in a safe hold instead of retrying forever")
	}
}

func TestPersistedSourcePodIdentityRejectsReplacement(t *testing.T) {
	uid := "pod-old"
	deployment := "openclaw-runtime"
	digest := "sha256:" + strings.Repeat("a", 64)
	item := models.RuntimeUpgradeItem{SourcePodUID: &uid, SourceDeploymentName: &deployment, SourceImageDigest: &digest}
	pod := models.RuntimePod{PodUID: &uid, DeploymentName: deployment, ImageDigest: &digest}
	if !persistedSourcePodMatches(pod, item) {
		t.Fatal("matching source Pod identity was rejected")
	}
	replacement := "pod-new"
	pod.PodUID = &replacement
	if persistedSourcePodMatches(pod, item) {
		t.Fatal("replacement Pod was mistaken for the stopped source writer")
	}
}

func int64Pointer(value int64) *int64 { return &value }

func TestRuntimeUpgradeScopePersistsLabSelectionWithoutNewRolloutColumns(t *testing.T) {
	runID := int64(17)
	raw := `{"upgrade_lab_run_id":17,"candidate_instance_ids":[9,3,9,-1]}`
	rollout := &models.RuntimeRollout{PreflightJSON: &raw}
	scope := runtimeUpgradeScopeFromRollout(rollout)
	if scope.UpgradeLabRunID == nil || *scope.UpgradeLabRunID != runID {
		t.Fatalf("lab run id = %#v", scope.UpgradeLabRunID)
	}
	if got := scope.CandidateInstanceIDs; len(got) != 2 || got[0] != 3 || got[1] != 9 {
		t.Fatalf("candidate ids = %#v", got)
	}
}

func TestUpgradeGatewayRestartTimeoutOnlyAppliesToActiveRestart(t *testing.T) {
	now := time.Now().UTC()
	expired, fresh := now.Add(-time.Second), now.Add(time.Minute)
	if !upgradeGatewayRestartExpired(models.RuntimeUpgradeItem{State: "restart_ready", RestartDeadlineAt: &expired}, time.Time{}, now) {
		t.Fatal("expired restart_ready item was allowed to wait forever")
	}
	if upgradeGatewayRestartExpired(models.RuntimeUpgradeItem{State: "restart_ready", RestartDeadlineAt: &fresh}, time.Time{}, now) {
		t.Fatal("fresh gateway restart was timed out")
	}
	if upgradeGatewayRestartExpired(models.RuntimeUpgradeItem{State: "gateway_verified", RestartDeadlineAt: &expired}, time.Time{}, now) {
		t.Fatal("verified gateway was timed out")
	}
	if !upgradeGatewayRestartExpired(models.RuntimeUpgradeItem{State: "restart_ready", RestartDeadlineAt: &expired}, now.Add(-time.Minute), now) {
		t.Fatal("replacement target pod must not silently reset the deadline")
	}
}

func TestGatewayRestartNotBeforeUsesNewestTargetPodCreation(t *testing.T) {
	now := time.Now().UTC()
	rollout := models.RuntimeRollout{UpdatedAt: now.Add(-10 * time.Minute)}
	pods := []models.RuntimePod{{CreatedAt: now.Add(-8 * time.Minute)}, {CreatedAt: now.Add(-2 * time.Minute)}}
	if got := gatewayRestartNotBefore(rollout, pods); !got.Equal(now.Add(-2 * time.Minute)) {
		t.Fatalf("restart not-before = %s", got)
	}
}

func TestVerifiedUpgradeBindingMustStillBelongToReadyTarget(t *testing.T) {
	target := "registry/openclaw@sha256:" + strings.Repeat("a", 64)
	binding := &models.InstanceRuntimeBinding{RuntimePodID: 17, Generation: 4}
	pods := []models.RuntimePod{{ID: 17, ImageDigest: stringPtrOrNil("sha256:" + strings.Repeat("a", 64)), State: "ready"}}
	if !verifiedUpgradeBindingOnTarget(binding, 4, pods, target) {
		t.Fatal("current target binding was rejected")
	}
	if verifiedUpgradeBindingOnTarget(binding, 5, pods, target) {
		t.Fatal("stale generation was accepted")
	}
	pods[0].Draining = true
	if verifiedUpgradeBindingOnTarget(binding, 4, pods, target) {
		t.Fatal("draining target binding was accepted")
	}
	if verifiedUpgradeBindingOnTarget(nil, 4, pods, target) {
		t.Fatal("missing binding was accepted")
	}
}

func TestRuntimeUpgradeGuardAppliesOnlyToOpenClaw(t *testing.T) {
	service := &RuntimeUpgradeService{}
	for _, runtimeType := range []string{RuntimeTypeHermes, RuntimeTypeOpenCode, RuntimeTypeDeepSeekHarness} {
		if service.InstanceBlocked(context.Background(), runtimeType, 17) {
			t.Fatalf("OpenClaw upgrade guard blocked %s instance", runtimeType)
		}
	}
	if !service.InstanceBlocked(context.Background(), RuntimeTypeOpenClaw, 17) {
		t.Fatal("OpenClaw guard must fail closed when its database is unavailable")
	}
}

func TestUpgradeReceiptReconciliationOnlyHandlesAmbiguousTransport(t *testing.T) {
	ctx := context.Background()
	if !shouldReconcileUpgradeReceipt(ctx, context.DeadlineExceeded) {
		t.Fatal("transport timeout must be reconciled against the durable receipt")
	}
	for _, err := range []error{ErrRuntimeAgentConflict, ErrRuntimeAgentNotFound, ErrRuntimeAgentUnsupported} {
		if shouldReconcileUpgradeReceipt(ctx, err) {
			t.Fatalf("definitive Runtime Agent error %v was treated as ambiguous", err)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if shouldReconcileUpgradeReceipt(cancelled, context.DeadlineExceeded) {
		t.Fatal("cancelled rollout context started a receipt reconciliation request")
	}
	if !validSessionSQLiteMigration(&RuntimeAgentSessionSQLiteMigration{Status: "validated", OutputSHA256: "output", SessionCatalogSHA256: "catalog"}) {
		t.Fatal("validated migration evidence was rejected")
	}
	if !validSessionSQLiteRestore(&RuntimeAgentSessionSQLiteRestore{Status: "restored", ConfigRestored: true, StateRestored: true}, false) {
		t.Fatal("validated restore evidence was rejected")
	}
	if validSessionSQLiteRestore(&RuntimeAgentSessionSQLiteRestore{Status: "restored", ConfigRestored: true, StateRestored: true}, true) {
		t.Fatal("legacy archive restore receipt was accepted for an 8.1 SQLite-preserving rollback")
	}
	if !validSessionSQLiteRestore(&RuntimeAgentSessionSQLiteRestore{Status: "restored", ConfigRestored: true, StateRestored: true, PreservedSessionSQLite: true}, true) {
		t.Fatal("SQLite-preserving rollback receipt was rejected")
	}
}

func TestActiveOpenClawRolloutBlockIsScopedToLabCandidates(t *testing.T) {
	lab := `{"upgrade_lab_run_id":7,"candidate_instance_ids":[203]}`
	tests := []struct {
		name           string
		phase          string
		rollbackStatus string
		preflight      string
		itemExists     bool
		itemState      string
		want           bool
	}{
		{name: "production maintenance blocks new OpenClaw", phase: "maintenance", want: true},
		{name: "empty pool reset blocks new OpenClaw", phase: RuntimeUpgradePhaseEmptyPoolReset, want: true},
		{name: "production gateway restart blocks missing item", phase: "gateway_restart", want: true},
		{name: "production candidate restart ready remains upgrade-owned", phase: "gateway_restart", itemExists: true, itemState: "restart_ready", want: true},
		{name: "lab maintenance ignores ordinary OpenClaw", phase: "maintenance", preflight: lab, want: false},
		{name: "lab rollback ignores ordinary OpenClaw", rollbackStatus: "waiting", preflight: lab, want: false},
		{name: "lab maintenance blocks candidate", phase: "maintenance", preflight: lab, itemExists: true, want: true},
		{name: "lab candidate restart ready remains upgrade-owned", phase: "gateway_restart", preflight: lab, itemExists: true, itemState: "restart_ready", want: true},
		{name: "lab candidate restart pending remains blocked", phase: "gateway_restart", preflight: lab, itemExists: true, itemState: "migrated", want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := activeOpenClawRolloutBlocksInstance(test.phase, test.rollbackStatus, test.preflight, test.itemExists, test.itemState); got != test.want {
				t.Fatalf("blocked = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRuntimeDeploymentInventoryIsPartitionedBetweenProductionAndLab(t *testing.T) {
	runID := int64(7)
	production := runtimeUpgradeScope{}
	lab := runtimeUpgradeScope{UpgradeLabRunID: &runID}
	cases := []struct {
		name           string
		productionWant bool
		labWant        bool
	}{
		{name: "openclaw-runtime", productionWant: true, labWant: false},
		{name: "openclaw-runtime-u44", productionWant: true, labWant: false},
		{name: "openclaw-upgrade-lab-r7-source", productionWant: false, labWant: true},
		{name: "openclaw-upgrade-lab-r7-source-u55", productionWant: false, labWant: true},
		{name: "openclaw-upgrade-lab-r8-source", productionWant: false, labWant: false},
	}
	for _, test := range cases {
		if got := runtimeDeploymentInUpgradeScope(test.name, production); got != test.productionWant {
			t.Fatalf("production scope for %s = %v, want %v", test.name, got, test.productionWant)
		}
		if got := runtimeDeploymentInUpgradeScope(test.name, lab); got != test.labWant {
			t.Fatalf("lab scope for %s = %v, want %v", test.name, got, test.labWant)
		}
	}
}

func TestRolloutExpectedTargetDeploymentsUsesOnlyPersistedSources(t *testing.T) {
	sources := `{"runtime-system/openclaw-runtime":"registry/openclaw@sha256:abc","runtime-system/openclaw-upgrade-lab-r7-source":"registry/openclaw@sha256:def"}`
	rollout := &models.RuntimeRollout{ID: 23, SourceImagesJSON: &sources}
	targets, err := rolloutExpectedTargetDeployments(rollout)
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"runtime-system/openclaw-runtime-u23", "runtime-system/openclaw-upgrade-lab-r7-source-u23"} {
		if _, ok := targets[expected]; !ok {
			t.Fatalf("missing target %s in %#v", expected, targets)
		}
	}
	if len(targets) != 2 {
		t.Fatalf("unexpected target set: %#v", targets)
	}
}

func TestImageDigestFromReferenceRequiresImmutableSHA256(t *testing.T) {
	digest := strings.Repeat("a", 64)
	if got := imageDigestFromReference("registry.example/openclaw@sha256:" + digest); got != "sha256:"+digest {
		t.Fatalf("digest = %q", got)
	}
	for _, invalid := range []string{"registry/openclaw:2026.8.1", "registry/openclaw@sha256:abc", "registry/openclaw@sha256:" + strings.Repeat("z", 64)} {
		if got := imageDigestFromReference(invalid); got != "" {
			t.Fatalf("invalid reference %q produced %q", invalid, got)
		}
	}
}

func TestOpenClawUpgradeContractExcludesFullWorkspaceSnapshots(t *testing.T) {
	joined := strings.Join(openClawUpgradeRequiredCapabilities, ",")
	if strings.Contains(joined, "workspace.snapshot") || strings.Contains(joined, "workspace.atomic-restore") {
		t.Fatalf("full-workspace capability remained in upgrade contract: %s", joined)
	}
	for _, required := range []string{"openclaw.session-sqlite-migrate-v1", "openclaw.session-sqlite-restore-v1", "openclaw.session-sqlite-preserve-v1", "openclaw.runtime-standby-v1", "openclaw.upgrade-capsule-v3", "openclaw.upgrade-preflight-v4"} {
		if !containsString(openClawUpgradeRequiredCapabilities, required) {
			t.Fatalf("missing capability %s", required)
		}
	}
}

func TestResolvedSourceOpenClawVersionOnlyTrustsPinnedLegacyDigest(t *testing.T) {
	reported := "2026.8.1"
	if got := resolvedSourceOpenClawVersion(models.RuntimePod{OpenClawVersion: &reported}); got != reported {
		t.Fatalf("reported version = %q", got)
	}
	baseline := openClawLegacyBaselineDigest
	if got := resolvedSourceOpenClawVersion(models.RuntimePod{ImageDigest: &baseline}); got != openClawLegacyBaselineRuntimeVersion {
		t.Fatalf("baseline version = %q", got)
	}
	if got := resolvedSourceOpenClawVersion(models.RuntimePod{ImageRef: "registry/openclaw:looks-like-7.1"}); got != "" {
		t.Fatalf("arbitrary tag inferred version %q", got)
	}
}

func TestRuntimePodMatchesImmutableImageByDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("d", 64)
	pod := models.RuntimePod{ImageRef: "registry/openclaw:legacy", ImageDigest: &digest}
	if !runtimePodMatchesImage(pod, "registry/openclaw@"+digest) {
		t.Fatal("tagged source pod did not match its immutable image digest")
	}
	other := "sha256:" + strings.Repeat("e", 64)
	pod.ImageDigest = &other
	if runtimePodMatchesImage(pod, "registry/openclaw@"+digest) {
		t.Fatal("different image digest was accepted")
	}
}

func TestNextRuntimeUpgradeBatchKeepsTeamAtomicAndWorkerBeforeLeader(t *testing.T) {
	team := 7
	items := []models.RuntimeUpgradeItem{
		{InstanceID: 1, State: "prepared"},
		{InstanceID: 2, TeamID: &team, State: "prepared"},
		{InstanceID: 3, TeamID: &team, State: "prepared", IsTeamLeader: true},
		{InstanceID: 4, State: "prepared"},
	}
	batch := nextRuntimeUpgradeBatch(items, 2, "prepared")
	if len(batch) != 1 || batch[0].InstanceID != 1 {
		t.Fatalf("first batch = %+v, want only standalone instance 1", batch)
	}
	batch = nextRuntimeUpgradeBatch(items[1:], 1, "prepared")
	if len(batch) != 2 || batch[0].InstanceID != 2 || batch[1].InstanceID != 3 {
		t.Fatalf("Team batch = %+v, want worker and leader together", batch)
	}
}

func TestNextRuntimeUpgradeMigrationBatchResumesEveryDurableIntermediateState(t *testing.T) {
	items := []models.RuntimeUpgradeItem{
		{InstanceID: 1, State: "compatibility_checked"},
		{InstanceID: 2, State: "quiescing"},
		{InstanceID: 3, State: "quiesced"},
		{InstanceID: 4, State: "migration_started"},
		{InstanceID: 5, State: "migrated"},
	}
	batch := nextRuntimeUpgradeMigrationBatch(items, 8)
	if len(batch) != 4 {
		t.Fatalf("migration resume batch = %+v, want four incomplete states", batch)
	}
	for index, want := range []int{1, 2, 3, 4} {
		if batch[index].InstanceID != want {
			t.Fatalf("migration resume batch[%d] = %d, want %d", index, batch[index].InstanceID, want)
		}
	}
}

func TestImmutableRuntimeImagePinsTagToObservedDigest(t *testing.T) {
	digest := "sha256:" + strings.Repeat("b", 64)
	got, err := immutableRuntimeImage("10.130.14.23:5000/agentsruntime/openclaw-lite:legacy", digest)
	if err != nil {
		t.Fatal(err)
	}
	want := "10.130.14.23:5000/agentsruntime/openclaw-lite@" + digest
	if got != want {
		t.Fatalf("immutable image = %q, want %q", got, want)
	}
	if _, err := immutableRuntimeImage("registry/openclaw:legacy", ""); err == nil {
		t.Fatal("missing source digest was accepted")
	}
}

func TestResolveRuntimeImageReferencePinsTagInLiveRegistry(t *testing.T) {
	digest := "sha256:" + strings.Repeat("c", 64)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/agentsruntime/openclaw-lite/manifests/release" {
			t.Fatalf("registry path = %q", r.URL.Path)
		}
		w.Header().Set("Docker-Content-Digest", digest)
		_, _ = w.Write([]byte(`{"schemaVersion":2}`))
	}))
	defer server.Close()
	host := strings.TrimPrefix(server.URL, "http://")
	sources := map[string]string{"runtime-system/openclaw-runtime": host + "/agentsruntime/openclaw-lite@sha256:" + strings.Repeat("a", 64)}
	got, err := resolveRuntimeImageReference(context.Background(), host+"/agentsruntime/openclaw-lite:release", sources)
	if err != nil {
		t.Fatal(err)
	}
	want := host + "/agentsruntime/openclaw-lite@" + digest
	if got != want {
		t.Fatalf("resolved image = %q, want %q", got, want)
	}
	if _, err := resolveRuntimeImageReference(context.Background(), "other.invalid/agentsruntime/openclaw-lite:release", sources); err == nil {
		t.Fatal("cross-registry target was accepted")
	}
}

func TestInspectOpenClawRegistryImageSelectsUpgradeStrategyFromImageMetadata(t *testing.T) {
	for _, test := range []struct {
		name           string
		version        string
		strategy       string
		protocol       string
		want           string
		imageOnlyReset bool
		wantErr        bool
	}{
		{name: "older labelled OpenClaw keeps generic rolling update", version: "2026.6.5", want: RuntimeUpgradeStrategyLegacyRolling},
		{name: "legacy 7.1 keeps generic rolling update", version: "2026.7.1-2", want: RuntimeUpgradeStrategyLegacyRolling},
		{name: "unlabelled legacy image keeps generic rolling update", want: RuntimeUpgradeStrategyLegacyRolling},
		{name: "malformed legacy version keeps generic rolling update", version: "legacy", want: RuntimeUpgradeStrategyLegacyRolling},
		{name: "partial data safe contract without version is rejected", strategy: openClawDataSafeImageStrategy, wantErr: true},
		{name: "empty pool accepts missing version metadata", imageOnlyReset: true, want: RuntimeUpgradeStrategyLegacyRolling},
		{name: "8.1 uses data safe update", version: "2026.8.1", strategy: openClawDataSafeImageStrategy, protocol: openClawDataSafeProtocol, want: RuntimeUpgradeStrategyOpenClawDataSafe},
		{name: "later compatible version uses data safe update", version: "2026.9.0", strategy: openClawDataSafeImageStrategy, protocol: openClawDataSafeProtocol, want: RuntimeUpgradeStrategyOpenClawDataSafe},
		{name: "8.1 without contract is rejected", version: "2026.8.1", wantErr: true},
		{name: "empty pool does not require 8.1 migration contract", version: "2026.8.1", imageOnlyReset: true, want: RuntimeUpgradeStrategyLegacyRolling},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifestDigest := "sha256:" + strings.Repeat("c", 64)
			configDigest := "sha256:" + strings.Repeat("d", 64)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case strings.Contains(r.URL.Path, "/manifests/"):
					w.Header().Set("Docker-Content-Digest", manifestDigest)
					_, _ = w.Write([]byte(`{"schemaVersion":2,"config":{"digest":"` + configDigest + `"}}`))
				case strings.Contains(r.URL.Path, "/blobs/"):
					labels := map[string]string{
						"io.clawmanager.runtime.type":     "openclaw",
						"io.clawmanager.openclaw.version": test.version,
					}
					if test.strategy != "" {
						labels["io.clawmanager.upgrade.strategy"] = test.strategy
					}
					if test.protocol != "" {
						labels["io.clawmanager.upgrade.protocol"] = test.protocol
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"config": map[string]any{"Labels": labels}})
				default:
					t.Fatalf("unexpected registry path %q", r.URL.Path)
				}
			}))
			defer server.Close()
			host := strings.TrimPrefix(server.URL, "http://")
			sources := map[string]string{"runtime/openclaw-runtime": host + "/agentsruntime/openclaw-lite:old"}
			got, err := inspectOpenClawRegistryImage(context.Background(), host+"/agentsruntime/openclaw-lite:release", sources, test.imageOnlyReset)
			if test.wantErr {
				if err == nil {
					t.Fatalf("unsupported image was accepted: %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.Strategy != test.want || got.RuntimeVersion != test.version || got.ImageRef != host+"/agentsruntime/openclaw-lite@"+manifestDigest {
				t.Fatalf("classification = %+v", got)
			}
		})
	}
}

func TestOpenClawNumericVersionComparison(t *testing.T) {
	for _, version := range []string{"2026.8.1", "v2026.8.1", "2026.8.1-2", "2026.9.0", "2027.1.0"} {
		if !openClawVersionAtLeast(version, targetOpenClawUpgradeVersion) {
			t.Fatalf("version %q was not classified as 8.1+", version)
		}
	}
	for _, version := range []string{"2026.7.1-2", "2025.12.9", "latest", ""} {
		if openClawVersionAtLeast(version, targetOpenClawUpgradeVersion) {
			t.Fatalf("version %q was classified as 8.1+", version)
		}
	}
}

func TestRuntimeUpgradeAPICollectionsEncodeAsArrays(t *testing.T) {
	preflightJSON, err := json.Marshal(newRuntimeUpgradePreflightResult())
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"blockers":[]`, `"warnings":[]`, `"required_capabilities":[]`} {
		if !strings.Contains(string(preflightJSON), field) {
			t.Fatalf("preflight JSON %s does not contain %s", preflightJSON, field)
		}
	}

	detailsJSON, err := json.Marshal(newRuntimeUpgradeDetails(&models.RuntimeRollout{}, nil, nil))
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"items":[]`, `"audits":[]`} {
		if !strings.Contains(string(detailsJSON), field) {
			t.Fatalf("details JSON %s does not contain %s", detailsJSON, field)
		}
	}
}

func TestLiteClassificationHonorsExplicitInstanceMode(t *testing.T) {
	for _, test := range []struct {
		name    string
		mode    string
		backend string
		want    bool
	}{
		{name: "lite gateway", mode: InstanceModeLite, backend: RuntimeBackendGateway, want: true},
		{name: "pro desktop", mode: InstanceModePro, backend: RuntimeBackendDesktop, want: false},
		{name: "explicit pro wins over conflicting gateway", mode: InstanceModePro, backend: RuntimeBackendGateway, want: false},
		{name: "legacy gateway fallback", mode: "", backend: RuntimeBackendGateway, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			instance := &models.Instance{InstanceMode: test.mode, RuntimeType: test.backend}
			if got := isLiteRuntimeInstance(instance); got != test.want {
				t.Fatalf("isLiteRuntimeInstance() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestLocalUpgradeSnapshotPreservesOpenClawWorkspaceBytes(t *testing.T) {
	root := t.TempDir()
	workspace := RuntimeWorkspacePathWithRoot(root, RuntimeTypeOpenClaw, 45, 123)
	if err := os.MkdirAll(filepath.Join(workspace, ".openclaw", "sessions"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".openclaw", "settings.json"), []byte(`{"version":"2026.7.1-2"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	validSQLite := append([]byte("SQLite format 3\x00"), make([]byte, 64)...)
	if err := os.WriteFile(filepath.Join(workspace, ".openclaw", "sessions", "sessions.sqlite"), validSQLite, 0o600); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(workspace, ".openclaw", "tmp", "openclaw-200075", "device-identity.98ce393a.lock.sqlite")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	service := &RuntimeUpgradeService{workspaceRoot: root}
	_, _, err := service.createLocalSnapshot(77, runtimeUpgradeCandidate{InstanceID: 123, UserID: 45, WorkspacePath: workspace})
	if err == nil {
		t.Fatal("full workspace snapshot unexpectedly remained enabled")
	}
}

func TestSQLiteMainDatabaseClassification(t *testing.T) {
	for _, test := range []struct {
		name string
		want bool
	}{
		{name: "sessions.sqlite", want: true},
		{name: "state.SQLITE3", want: true},
		{name: "cache.db", want: true},
		{name: "device-identity.98ce393a.lock.sqlite", want: false},
		{name: "sessions.sqlite-wal", want: false},
		{name: "sessions.sqlite-shm", want: false},
		{name: "sessions.sqlite-journal", want: false},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := isSQLiteMainDatabase(test.name); got != test.want {
				t.Fatalf("isSQLiteMainDatabase(%q) = %v, want %v", test.name, got, test.want)
			}
		})
	}
}

func TestRuntimeWorkspaceSQLitePreflightIgnoresLockAndCompanionFiles(t *testing.T) {
	root := t.TempDir()
	validSQLite := append([]byte("SQLite format 3\x00"), make([]byte, 64)...)
	for name, data := range map[string][]byte{
		"sessions.sqlite":                  validSQLite,
		"device-identity.test.lock.sqlite": nil,
		"sessions.sqlite-wal":              []byte("wal bytes"),
		"sessions.sqlite-shm":              []byte("shm bytes"),
		"sessions.sqlite-journal":          []byte("journal bytes"),
	} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := validateRuntimeWorkspaceSQLite(root); err != nil {
		t.Fatalf("preflight rejected SQLite companion file: %v", err)
	}
	inventory, err := inspectRuntimeWorkspace(root)
	if err != nil {
		t.Fatal(err)
	}
	if inventory.FileCount != 5 || inventory.SQLiteFileCount != 1 {
		t.Fatalf("inventory = %#v", inventory)
	}
}

func TestInspectRuntimeWorkspaceRejectsInvalidSQLiteWithoutChangingIt(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "broken.sqlite")
	original := []byte("not a sqlite database")
	if err := os.WriteFile(dbPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := inspectRuntimeWorkspace(root); err == nil {
		t.Fatal("invalid SQLite header was accepted")
	}
	got, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatal("preflight changed invalid database bytes")
	}
}

func TestRuntimeUpgradeCandidateOrderKeepsLeaderLastWithinEachTeam(t *testing.T) {
	teamOne, teamTwo := 11, 12
	candidates := []runtimeUpgradeCandidate{
		{InstanceID: 1, TeamID: &teamOne, Role: "leader"},
		{InstanceID: 4, TeamID: &teamTwo, Role: "leader"},
		{InstanceID: 3, TeamID: &teamOne, Role: "worker"},
		{InstanceID: 2, TeamID: &teamOne, Role: "worker"},
		{InstanceID: 5, TeamID: &teamTwo, Role: "worker"},
	}
	sortRuntimeUpgradeCandidates(candidates)
	want := []int{2, 3, 1, 5, 4}
	for index, instanceID := range want {
		if candidates[index].InstanceID != instanceID {
			t.Fatalf("candidate order = %#v, want %v", candidates, want)
		}
	}
}

func TestValidateOpenClawUpgradeAggregateCapacityUsesSessionAndStateCapsule(t *testing.T) {
	compatibility := func(instanceID int, sessionBytes, stateBytes, configBytes int64, availableBytes uint64) models.RuntimeUpgradeItem {
		raw, err := json.Marshal(map[string]any{"compatibility": RuntimeAgentUpgradeCompatibility{
			InstanceID: instanceID, Status: "compatible", SessionBytes: sessionBytes,
			StateBytes: stateBytes, ConfigBytes: configBytes, AvailableBytes: availableBytes,
			ConfigValidated: true, DoctorValidated: true, SessionDryRunValid: true,
		}})
		if err != nil {
			t.Fatal(err)
		}
		value := string(raw)
		return models.RuntimeUpgradeItem{InstanceID: instanceID, State: "compatibility_checked", PreflightJSON: &value}
	}
	items := []models.RuntimeUpgradeItem{
		compatibility(1, 10<<20, 3<<20, 4<<10, 2<<30),
		compatibility(2, 20<<20, 5<<20, 8<<10, 3<<30),
	}
	required, available, err := validateOpenClawUpgradeAggregateCapacity(items)
	if err != nil {
		t.Fatal(err)
	}
	wantRequired := uint64(1<<30) + uint64(30<<20)*3 + uint64(12<<10)*2 + uint64(8<<20)*2
	if required != wantRequired || available != uint64(2<<30) {
		t.Fatalf("capacity = required %d available %d, want %d and %d", required, available, wantRequired, uint64(2<<30))
	}
	tooSmall := compatibility(3, 64<<20, 8<<20, 1024, 1<<30)
	if _, _, err := validateOpenClawUpgradeAggregateCapacity([]models.RuntimeUpgradeItem{tooSmall}); err == nil {
		t.Fatal("aggregate capacity accepted a volume without migration reserve")
	}
}
