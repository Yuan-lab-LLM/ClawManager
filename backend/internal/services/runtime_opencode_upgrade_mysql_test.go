package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
	corev1 "k8s.io/api/core/v1"
)

type openCodeFlowDeployments struct {
	*fakeRuntimeDeploymentService
	sess                   db.Session
	source                 *k8s.OpenCodeUpgradeSource
	target                 *k8s.OpenCodeUpgradeSource
	retired                bool
	sourceGone             bool
	activated, retireCalls int
	targetRetired          bool
	loseRetireResponse     bool
	targetErr              error
}

func (d *openCodeFlowDeployments) CaptureOpenCodeUpgradeSource(_ context.Context, t k8s.RuntimeRolloutTarget) (*k8s.OpenCodeUpgradeSource, error) {
	if t.Name == d.source.Target.Name {
		return d.source, nil
	}
	if d.target != nil && t.Name == d.target.Target.Name {
		return d.target, nil
	}
	return nil, fmt.Errorf("unknown controller")
}
func (d *openCodeFlowDeployments) ValidateOpenCodeUpgradeSource(_ context.Context, p *k8s.OpenCodeUpgradeSource) error {
	if p.TemplateSHA256 != "frozen-template" || p.StorageIdentity != "frozen-storage" {
		return fmt.Errorf("configuration drift")
	}
	return nil
}
func (d *openCodeFlowDeployments) InspectOpenCodeUpgradeTarget(_ context.Context, ns, name, image, id string) (k8s.RuntimeRolloutTarget, error) {
	if d.target == nil {
		return k8s.RuntimeRolloutTarget{}, fmt.Errorf("target missing")
	}
	target := d.target.Target
	target.Ready = d.activated > 0
	return target, nil
}
func (d *openCodeFlowDeployments) ColdRuntimeStandbyReady(context.Context, k8s.RuntimeRolloutTarget, string) (bool, error) {
	return true, nil
}
func (d *openCodeFlowDeployments) EnsureUpgradePool(ctx context.Context, ns, source, target, image, id string) error {
	if d.target != nil {
		return nil
	}
	if err := d.fakeRuntimeDeploymentService.EnsureUpgradePool(ctx, ns, source, target, image, id); err != nil {
		return err
	}
	digest := imageDigestFromReference(image)
	uid := "target-pod-uid"
	ip := "10.0.0.2"
	endpoint := "http://10.0.0.2:19090"
	caps, _ := json.Marshal(openCodeUpgradeCapabilities)
	_, err := d.sess.SQL().ExecContext(ctx, `INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,image_digest,state,capacity,used_slots,last_seen_at,agent_endpoint,agent_protocol_version,session_store,capabilities_json) VALUES(2,'opencode',?,'target-pod',?,?,?, ?,?,'pending',8,0,?,?,?,'sqlite',?)`, ns, uid, ip, target, image, digest, time.Now().UTC(), endpoint, openCodeNativeProtocol, string(caps))
	if err != nil {
		return err
	}
	d.pods = append(d.pods, k8s.RuntimeDeploymentPod{RuntimeType: "opencode", Namespace: ns, PodName: "target-pod", PodUID: uid, PodIP: &ip, DeploymentName: target, ImageRef: image, ImageDigest: digest, State: "pending", Capacity: 8, PoolRole: "upgrade-target", UpgradeID: id, SourceDeployment: source})
	d.target = &k8s.OpenCodeUpgradeSource{Target: k8s.RuntimeRolloutTarget{Namespace: ns, Name: target, UID: "target-deployment-uid", Image: image}, TemplateSHA256: "frozen-template", StorageIdentity: "frozen-storage", Replicas: 1}
	return nil
}
func (d *openCodeFlowDeployments) SetUpgradePoolActive(context.Context, string, string, string, string, bool) error {
	d.activated++
	return nil
}
func (d *openCodeFlowDeployments) RetireOpenCodeUpgradeSource(_ context.Context, p *k8s.OpenCodeUpgradeSource, _ string) error {
	if p.Target.Name == d.source.Target.Name {
		d.retired = true
		d.retireCalls++
	} else {
		d.targetRetired = true
		if d.loseRetireResponse {
			d.loseRetireResponse = false
			return context.DeadlineExceeded
		}
	}
	return nil
}
func (d *openCodeFlowDeployments) OpenCodeUpgradeSourceRetired(_ context.Context, p *k8s.OpenCodeUpgradeSource, _ string) (bool, error) {
	if p.Target.Name != d.source.Target.Name {
		if d.targetRetired {
			return true, nil
		}
		return false, errors.New("target is serving")
	}
	if !d.retired {
		return false, fmt.Errorf("source is serving")
	}
	return d.sourceGone, nil
}

type openCodeFlowAgent struct {
	*fakeRuntimeAgentClient
	RuntimeUpgradeAgentClient
	d                                *openCodeFlowDeployments
	idleCalls                        int
	idleErr                          error
	sourceStopped                    bool
	stopErr                          error
	badCapsule                       bool
	loseMigrationResponse            bool
	nativeRequests                   map[string]bool
	migrations, creates              int
	states                           map[string]*RuntimeAgentGatewayState
	currentAssignment                k8s.RuntimeAssignment
	assignmentState                  string
	targetFails                      bool
	restoreRequest                   RuntimeAgentWorkspaceRequest
	badRestore                       bool
	releaseCalls                     int
	compatibilityCalls, acquireCalls int
}

func (a *openCodeFlowAgent) RequiresConfirmedGatewayStop() bool { return true }
func (a *openCodeFlowAgent) openCodeSourceAssignment(context.Context, int) (k8s.RuntimeAssignment, string, error) {
	if a.currentAssignment.InstanceID == 0 {
		return k8s.RuntimeAssignment{}, "", errors.New("not an empty pool")
	}
	return a.currentAssignment, a.assignmentState, nil
}
func (a *openCodeFlowAgent) openCodeSourceStopProof(_ context.Context, want k8s.RuntimeAssignment) (json.RawMessage, error) {
	if a.stopErr != nil || a.assignmentState != "stopped" {
		return nil, errors.New("no C1")
	}
	proof, _ := json.Marshal(map[string]any{"pod_uid": want.PodUID, "instance_id": want.InstanceID, "user_id": want.UserID, "generation": want.Generation, "gateway_id": want.GatewayID, "container_id": "cid-" + want.GatewayID, "finished_at": time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)})
	return proof, nil
}
func (a *openCodeFlowAgent) openCodePoolIdle(_ context.Context, pod models.RuntimePod) error {
	a.idleCalls++
	if a.d.targetRetired && pod.ID == 2 {
		return errors.New("retired target endpoint no longer exists")
	}
	if a.currentAssignment.PodUID == stringValue(pod.PodUID) && a.assignmentState != "stopped" {
		return errors.New("source writer remains")
	}
	return a.idleErr
}
func (a *openCodeFlowAgent) ActivateUpgrade(ctx context.Context, endpoint, id string) error {
	for i := range a.d.pods {
		if a.d.pods[i].PodName == "target-pod" {
			a.d.pods[i].State = "ready"
		}
	}
	_, err := a.d.sess.SQL().ExecContext(ctx, `UPDATE runtime_pods SET state='ready' WHERE id=2`)
	return err
}

func openCodeControllerFixture(t *testing.T) (db.Session, *RuntimeScheduler, *models.RuntimeRollout, *openCodeFlowAgent) {
	t.Helper()
	sess := upgradeFlowDB(t)
	migration, err := os.ReadFile("../db/migrations/068_add_runtime_rollout_checkpoints.sql")
	if err != nil {
		t.Fatal(err)
	}
	cleanLines := []string{}
	for _, line := range strings.Split(string(migration), "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), "--") {
			cleanLines = append(cleanLines, line)
		}
	}
	for _, statement := range strings.Split(strings.Join(cleanLines, "\n"), ";") {
		if strings.TrimSpace(statement) != "" {
			if _, err = sess.SQL().Exec(statement); err != nil {
				t.Fatal(err)
			}
		}
	}

	ctx := context.Background()
	repo := repository.NewRuntimeRolloutRepository(sess)
	digest := "sha256:" + strings.Repeat("a", 64)
	source := k8s.RuntimeRolloutTarget{Namespace: "runtime", Name: "opencode-runtime", UID: "source-deployment-uid", Image: openCodeLegacyImage}
	sourcePod := models.RuntimePod{ID: 1, RuntimeType: "opencode", Namespace: "runtime", PodName: "source-pod", PodUID: stringPtrOrNil("source-pod-uid"), PodIP: stringPtrOrNil("10.0.0.1"), DeploymentName: source.Name, ImageRef: source.Image, ImageDigest: stringPtrOrNil(imageDigestFromReference(source.Image)), AgentEndpoint: stringPtrOrNil("http://10.0.0.1:19090"), State: "ready", Capacity: 8}
	d := &openCodeFlowDeployments{sess: sess, fakeRuntimeDeploymentService: &fakeRuntimeDeploymentService{pods: []k8s.RuntimeDeploymentPod{{RuntimeType: "opencode", Namespace: "runtime", PodName: sourcePod.PodName, PodUID: *sourcePod.PodUID, PodIP: sourcePod.PodIP, DeploymentName: source.Name, ImageRef: source.Image, ImageDigest: *sourcePod.ImageDigest, State: "ready", Capacity: 8}}}, source: &k8s.OpenCodeUpgradeSource{Target: source, Template: corev1.PodTemplateSpec{}, TemplateSHA256: "frozen-template", StorageIdentity: "frozen-storage", Replicas: 1}}
	a := &openCodeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, d: d}
	s := NewRuntimeScheduler(repository.NewInstanceRepository(sess), repository.NewRuntimePodRepository(sess), repository.NewInstanceRuntimeBindingRepository(sess), repo, a, nil, nil, d, time.Second, WithRuntimeSchedulerNamespace("runtime"), WithRuntimeSchedulerWorkspaceRoot("/workspaces"))
	s.upgrade = NewRuntimeUpgradeService(sess, repo, s.podRepo, s.bindingRepo, a, "/workspaces", "")
	p := openCodeUpgradePlan{TargetImage: "clawmanager.local/opencode-lite@" + digest, Contract: openCodeNativeContract{Version: openCodeNativeVersion, Revision: openCodeNativeRevision, BinarySHA256: openCodeNativeBinarySHA256, JournalSHA256: openCodeNativeJournalSHA256, BaseImage: openCodeLegacyImage}, Kind: "opencode-native-preserve", Version: 1, Strategy: RuntimeUpgradeStrategyOpenCodePreserve, Stage: "standby", Sources: []*k8s.OpenCodeUpgradeSource{d.source}, SourceDeploymentProofs: []k8s.RuntimeRolloutTarget{source}, SourcePods: []models.RuntimePod{sourcePod}, Operation: strings.Repeat("a", 32), Instances: []openCodeUpgradeInstance{}}
	raw, _ := json.Marshal(p)
	rawString := string(raw)
	fingerprint := openCodePreflightFingerprint(raw)
	sourceRaw := `{"runtime/opencode-runtime":"` + source.Image + `"}`
	proof := "proof"
	r := &models.RuntimeRollout{RuntimeType: "opencode", Status: "pending", Phase: "opencode_native_preserve", TargetImageRef: "clawmanager.local/opencode-lite@" + digest, TargetImageDigest: &digest, PreflightJSON: &rawString, PlanFingerprint: &fingerprint, PreflightID: &proof, SourceImagesJSON: &sourceRaw, BatchSize: 1, MaxUnavailable: 1}
	if err := repo.Create(ctx, r); err != nil {
		t.Fatal(err)
	}
	store := repo.(interface {
		CompareAndSavePoolCheckpoint(context.Context, int64, string, string) error
	})
	if err := store.CompareAndSavePoolCheckpoint(ctx, r.ID, "", rawString); err != nil {
		t.Fatal(err)
	}
	return sess, s, r, a
}
func TestOpenCodeNativeEmptyPoolMySQLResumesAndRequiresPhysicalRetirement(t *testing.T) {
	ctx := context.Background()
	sess, s, r, a := openCodeControllerFixture(t)
	for i := 0; i < 20; i++ {
		current, err := s.rolloutRepo.GetByID(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == "finished" {
			t.Fatal("finished before actual source Pod exit")
		}
		if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if !a.d.retired || a.d.retireCalls != 1 {
		t.Fatalf("retired=%v calls=%d", a.d.retired, a.d.retireCalls)
	}
	a.d.sourceGone = true
	for i := 0; i < 5; i++ {
		current, err := s.rolloutRepo.GetByID(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == "finished" {
			break
		}
		if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	current, err := s.rolloutRepo.GetByID(ctx, r.ID)
	if err != nil || current.Status != "finished" {
		t.Fatalf("rollout=%+v err=%v", current, err)
	}
	if len(a.d.rolloutImageCalls) != 0 || a.idleCalls < 2 {
		t.Fatalf("unsafe shared replacement or missing physical idle: %+v", a.d.fakeRuntimeDeploymentService)
	}
	var count int
	row, _ := sess.SQL().QueryRow(`SELECT COUNT(*) FROM runtime_rollout_checkpoints WHERE rollout_id=?`, r.ID)
	if err = row.Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
}
func TestOpenCodeNativeEmptyPoolMySQLUnknownWriterNeverActivates(t *testing.T) {
	ctx := context.Background()
	_, s, r, a := openCodeControllerFixture(t)
	a.idleErr = errors.New("physical writer exit unknown")
	for i := 0; i < 4; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		_ = s.advanceOpenCodeUpgrade(ctx, current)
	}
	if a.d.activated != 0 || a.d.retired {
		t.Fatal("unknown source writer allowed pool activation")
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	if current.Status != "running" || current.ErrorMessage == nil {
		t.Fatalf("protection lost %+v", current)
	}
}

func (a *openCodeFlowAgent) DeleteGateway(_ context.Context, endpoint, id string) error {
	if a.stopErr != nil {
		return a.stopErr
	}
	a.sourceStopped = true
	a.assignmentState = "stopped"
	delete(a.states, id)
	return nil
}
func (a *openCodeFlowAgent) PreflightUpgradeCompatibility(_ context.Context, _ string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentUpgradeCompatibility, error) {
	a.compatibilityCalls++
	if !a.sourceStopped {
		return nil, errors.New("native read before source C1")
	}
	digest := strings.Repeat("a", 64)
	return &RuntimeAgentUpgradeCompatibility{InstanceID: req.InstanceID, RolloutID: req.RolloutID, Generation: req.Generation, ConfigBytes: 100, SessionBytes: 1024, StateBytes: 2048, AvailableBytes: 1 << 20, CheckedAt: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC), Status: "compatible", ConfigOriginalSHA256: digest, ConfigTargetSHA256: digest, ProbeOutputSHA256: digest, ConfigValidated: true, DoctorValidated: true, SessionDryRunValid: true}, nil
}
func (a *openCodeFlowAgent) AcquireWriterLease(context.Context, string, RuntimeAgentWriterLeaseRequest) error {
	a.acquireCalls++
	return nil
}
func (a *openCodeFlowAgent) ReleaseWriterLease(context.Context, string, RuntimeAgentWriterLeaseRequest) error {
	a.releaseCalls++
	return nil
}
func (a *openCodeFlowAgent) MigrateSessionSQLite(_ context.Context, _ string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	if !a.sourceStopped {
		return nil, errors.New("migration before source C1")
	}
	if a.nativeRequests == nil {
		a.nativeRequests = map[string]bool{}
	}
	key := fmt.Sprintf("%s/%d/%d", req.RolloutID, req.InstanceID, req.Generation)
	if !a.nativeRequests[key] {
		a.nativeRequests[key] = true
		a.migrations++
	}
	if a.loseMigrationResponse {
		a.loseMigrationResponse = false
		return nil, context.DeadlineExceeded
	}
	return a.SessionSQLiteMigrationStatus(context.Background(), "", req)
}
func (a *openCodeFlowAgent) SessionSQLiteMigrationStatus(_ context.Context, _ string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteMigration, error) {
	digest := strings.Repeat("c", 64)
	return &RuntimeAgentSessionSQLiteMigration{InstanceID: req.InstanceID, RolloutID: req.RolloutID, Generation: req.Generation, ArchiveBytes: 1024, ArchiveFiles: 4, StateCapsuleBytes: 1024, ConfigOriginalSHA256: strings.Repeat("a", 64), ConfigTargetSHA256: strings.Repeat("a", 64), CompletedAt: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC), Status: "validated", OutputSHA256: digest, PreservationSHA256: digest, SessionCatalogSHA256: digest, DataPreserved: !a.badCapsule, RollbackAvailable: true}, nil
}
func (a *openCodeFlowAgent) CreateGateway(_ context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	a.creates++
	gateway := fmt.Sprintf("gw-%d-%d", req.InstanceID, req.Generation)
	status := "running"
	podUID := "source-pod-uid"
	if strings.Contains(endpoint, "10.0.0.2") {
		podUID = "target-pod-uid"
		if a.targetFails {
			status = "error"
		}
	}
	a.currentAssignment = k8s.RuntimeAssignment{InstanceID: req.InstanceID, UserID: req.UserID, Generation: req.Generation, RuntimeType: "opencode", Endpoint: endpoint, GatewayID: gateway, PodUID: podUID}
	a.assignmentState = "running"
	a.states[gateway] = &RuntimeAgentGatewayState{GatewayID: gateway, InstanceID: req.InstanceID, Generation: req.Generation, State: status}
	return &RuntimeAgentCreateGatewayResponse{GatewayID: gateway, Port: req.GatewayPort, Status: status}, nil
}
func (a *openCodeFlowAgent) GatewayState(_ context.Context, _ string, id string) (*RuntimeAgentGatewayState, error) {
	if state := a.states[id]; state != nil {
		return state, nil
	}
	return nil, ErrRuntimeAgentNotFound
}

func openCodeActiveControllerFixture(t *testing.T) (db.Session, *RuntimeScheduler, *models.RuntimeRollout, *openCodeFlowAgent) {
	t.Helper()
	sess, s, r, a := openCodeControllerFixture(t)
	ctx := context.Background()
	exec := func(query string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().ExecContext(ctx, query, args...); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"099_add_workspace_write_fences.sql", "068_add_workspace_write_intents.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		for _, q := range strings.Split(string(raw), ";") {
			if strings.TrimSpace(q) != "" {
				exec(q)
			}
		}
	}
	exec(`ALTER TABLE team_members ADD team_id INT, ADD instance_id INT`)
	exec(`CREATE TABLE instance_commands(id INT PRIMARY KEY,instance_id INT,status VARCHAR(32))`)
	exec(`INSERT INTO users(id) VALUES(1)`)
	exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,workspace_path,updated_at) VALUES(7,1,'opencode','gateway','lite','running',3,'/workspaces/opencode/user-1/instance-7',NOW())`)
	exec(`INSERT INTO instance_desired_state(instance_id,desired_power_state,created_at,updated_at) VALUES(7,'running',NOW(),NOW())`)
	exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,used_slots,last_seen_at) VALUES(1,'opencode','runtime','source-pod','source-pod-uid','10.0.0.1','opencode-runtime',?,?,'http://10.0.0.1:19090','ready',8,1,NOW())`, openCodeLegacyImage, imageDigestFromReference(openCodeLegacyImage))
	exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation,created_at,updated_at) VALUES(7,1,'opencode','gw-7-3',32000,'/workspaces/opencode/user-1/instance-7','running',3,NOW(),NOW())`)
	p, err := s.loadOpenCodePlan(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := s.bindingRepo.GetByInstanceID(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	p.Instances = []openCodeUpgradeInstance{{ID: 7, Owner: 1, Generation: 3, Status: "running", Workspace: binding.WorkspacePath, SourcePod: p.SourcePods[0], Assignment: k8s.RuntimeAssignment{InstanceID: 7, UserID: 1, Generation: 3, RuntimeType: "opencode", Endpoint: "http://10.0.0.1:19090", GatewayID: "gw-7-3", PodUID: "source-pod-uid"}, Binding: binding, State: "pending"}}
	raw, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	rawString := string(raw)
	r.PreflightJSON = &rawString
	fingerprint := openCodePreflightFingerprint(raw)
	r.PlanFingerprint = &fingerprint
	exec(`UPDATE runtime_rollouts SET preflight_json=?,plan_fingerprint=? WHERE id=?`, rawString, fingerprint, r.ID)
	exec(`UPDATE runtime_rollout_checkpoints SET state_json=? WHERE rollout_id=?`, rawString, r.ID)
	exec(`INSERT INTO gateway_runtime_upgrades(instance_id,owner_user_id,generation,operation_id,state,created_at,updated_at) VALUES(7,1,3,?,'reserved',NOW(),NOW())`, p.Operation)
	exec(`INSERT INTO instance_lifecycle_manual_guards(instance_id,token,created_at) VALUES(7,?,NOW())`, "opencode_upgrade_"+p.Operation)
	candidate := runtimeUpgradeCandidate{InstanceID: 7, UserID: 1, Generation: 3, RuntimePodID: &p.SourcePods[0].ID, GatewayID: "gw-7-3", SourceVersion: openCodeNativeVersion, SourcePodUID: "source-pod-uid", SourceDeployment: "opencode-runtime", SourceImageDigest: imageDigestFromReference(openCodeLegacyImage)}
	if err = s.upgrade.insertUpgradeItems(ctx, sess, r.ID, []runtimeUpgradeCandidate{candidate}, openCodeNativeVersion); err != nil {
		t.Fatal(err)
	}
	a.currentAssignment = p.Instances[0].Assignment
	a.assignmentState = "running"
	a.states = map[string]*RuntimeAgentGatewayState{"gw-7-3": {GatewayID: "gw-7-3", InstanceID: 7, Generation: 3, State: "running"}}
	a.d.sourceGone = true
	return sess, s, r, a
}
func TestOpenCodeNativeActiveMySQLKeepsFenceAndResumesLostNativeResponse(t *testing.T) {
	sess, s, r, a := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	a.loseMigrationResponse = true
	for i := 0; i < 40; i++ {
		current, err := s.rolloutRepo.GetByID(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == "finished" {
			break
		}
		err = s.advanceOpenCodeUpgrade(ctx, current)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("step %d: %v", i, err)
		}
		if errors.Is(err, context.DeadlineExceeded) && a.releaseCalls != 0 {
			t.Fatal("released lease after unknown migration outcome")
		}
	}
	current, err := s.rolloutRepo.GetByID(ctx, r.ID)
	if err != nil || current.Status != "finished" {
		t.Fatalf("rollout=%+v err=%v", current, err)
	}
	if a.migrations != 1 || a.creates != 1 {
		t.Fatalf("migration operations=%d creates=%d", a.migrations, a.creates)
	}
	var generation int
	var status string
	row, _ := sess.SQL().QueryRow(`SELECT runtime_generation,status FROM instances WHERE id=7`)
	if err = row.Scan(&generation, &status); err != nil || generation != 4 || status != "running" {
		t.Fatal(generation, status, err)
	}
	var state string
	row, _ = sess.SQL().QueryRow(`SELECT state FROM gateway_runtime_upgrades WHERE instance_id=7`)
	if err = row.Scan(&state); err != nil || state != "completed" {
		t.Fatal(state, err)
	}
}
func TestOpenCodeNativeActiveMySQLRefusesMissingC1AndIncompleteCapsule(t *testing.T) {
	for _, bad := range []string{"c1", "capsule"} {
		t.Run(bad, func(t *testing.T) {
			_, s, r, a := openCodeActiveControllerFixture(t)
			if bad == "c1" {
				a.stopErr = errors.New("source C1 pending")
			} else {
				a.badCapsule = true
			}
			for i := 0; i < 10; i++ {
				current, _ := s.rolloutRepo.GetByID(context.Background(), r.ID)
				_ = s.advanceOpenCodeUpgrade(context.Background(), current)
			}
			if a.creates != 0 || a.d.activated != 0 || a.d.retired {
				t.Fatal("incomplete source/capsule proof allowed activation")
			}
		})
	}
}

func (a *openCodeFlowAgent) RestoreSessionSQLite(_ context.Context, _ string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error) {
	if !req.PreserveSessionSQLite || req.ResumeGeneration != 3 || req.Generation != 3 && req.Generation != 4 || a.assignmentState != "stopped" {
		return nil, errors.New("unsafe restore identity or live writer")
	}
	a.restoreRequest = req
	return a.SessionSQLiteRestoreStatus(context.Background(), "", req)
}
func (a *openCodeFlowAgent) SessionSQLiteRestoreStatus(_ context.Context, _ string, req RuntimeAgentWorkspaceRequest) (*RuntimeAgentSessionSQLiteRestore, error) {
	return &RuntimeAgentSessionSQLiteRestore{InstanceID: req.InstanceID, RolloutID: req.RolloutID, Generation: req.Generation, CompletedAt: time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC), Status: "restored", OutputSHA256: strings.Repeat("d", 64), ConfigRestored: true, StateRestored: true, PreservedSessionSQLite: !a.badRestore}, nil
}
func TestOpenCodeNativeMySQLRollbackStopsTargetAndPreservesCurrentHistory(t *testing.T) {
	sess, s, r, a := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	a.targetFails = true
	for i := 0; i < 60; i++ {
		current, err := s.rolloutRepo.GetByID(ctx, r.ID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == "error" {
			break
		}
		if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	current, err := s.rolloutRepo.GetByID(ctx, r.ID)
	if err != nil || current.Status != "error" || stringValue(current.RollbackStatus) != "restored" {
		t.Fatalf("rollout=%+v err=%v", current, err)
	}
	if a.restoreRequest.Generation != 4 || a.restoreRequest.ResumeGeneration != 3 || !a.restoreRequest.PreserveSessionSQLite || a.creates != 2 || !a.d.targetRetired || a.d.retired {
		t.Fatalf("unsafe rollback request=%+v creates=%d targetRetired=%v sourceRetired=%v", a.restoreRequest, a.creates, a.d.targetRetired, a.d.retired)
	}
	var generation int
	var status string
	row, _ := sess.SQL().QueryRow(`SELECT runtime_generation,status FROM instances WHERE id=7`)
	if err = row.Scan(&generation, &status); err != nil || generation != 5 || status != "running" {
		t.Fatal(generation, status, err)
	}
	images, err := LoadCommittedRuntimeImages(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if images.Images["opencode"] != "" {
		t.Fatal("rolled back target became default")
	}
}
func TestOpenCodeNativeMySQLRollbackCannotDiscardNewHistory(t *testing.T) {
	_, s, r, a := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	a.targetFails = true
	a.badRestore = true
	for i := 0; i < 30; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		_ = s.advanceOpenCodeUpgrade(ctx, current)
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	if a.creates != 1 || current.Status != "running" || a.d.targetRetired || a.d.retired {
		t.Fatalf("unverified restore released protection: %+v creates=%d", current, a.creates)
	}
}

func (a *openCodeFlowAgent) openCodeHistoricalSourceStopProof(ctx context.Context, want k8s.RuntimeAssignment) (json.RawMessage, error) {
	return a.openCodeSourceStopProof(ctx, want)
}

func TestOpenCodeNativeMySQLStoppedSourceRetainsExactProvenanceAfterRetirement(t *testing.T) {
	sess, s, r, a := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	p, err := s.loadOpenCodePlan(ctx, r)
	if err != nil {
		t.Fatal(err)
	}
	p.Instances[0].Status = "stopped"
	p.Instances[0].Binding = nil
	a.sourceStopped = true
	a.assignmentState = "stopped"
	raw, _ := json.Marshal(p)
	fingerprint := openCodePreflightFingerprint(raw)
	for _, statement := range []string{`DELETE FROM instance_runtime_bindings WHERE instance_id=7`, `UPDATE instances SET status='stopped' WHERE id=7`, `UPDATE instance_desired_state SET desired_power_state='stopped' WHERE instance_id=7`} {
		if _, err = sess.SQL().ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET preflight_json=?,plan_fingerprint=? WHERE id=?`, string(raw), fingerprint, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().ExecContext(ctx, `UPDATE runtime_rollout_checkpoints SET state_json=? WHERE rollout_id=?`, string(raw), r.ID); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if current.Status == "finished" {
			break
		}
		if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	if current.Status != "finished" || a.creates != 0 {
		t.Fatalf("stopped source was started: %+v creates=%d", current, a.creates)
	}
	next := openCodeUpgradeInstance{ID: 7, Owner: 1, Generation: 3, Status: "stopped", Workspace: "/workspaces/opencode/user-1/instance-7"}
	source, prior, err := s.completedOpenCodeStoppedSource(ctx, next, a.currentAssignment, a.d.target)
	if err != nil || prior != r.ID || source.ID != 1 || stringValue(source.PodUID) != "source-pod-uid" {
		t.Fatalf("historical bridge=%+v prior=%d error=%v", source, prior, err)
	}
	changed := *a.d.target
	changed.StorageIdentity = "replaced-storage"
	if _, _, err = s.completedOpenCodeStoppedSource(ctx, next, a.currentAssignment, &changed); err == nil {
		t.Fatal("historical source accepted different storage")
	}
	changed = *a.d.target
	changed.Target.UID = "replacement-controller"
	if _, _, err = s.completedOpenCodeStoppedSource(ctx, next, a.currentAssignment, &changed); err == nil {
		t.Fatal("historical source accepted replacement controller")
	}
}
func TestOpenCodeNativeMySQLCheckpointRejectsFrozenIdentityDriftBeforeMutation(t *testing.T) {
	for _, field := range []string{"target", "source", "native"} {
		t.Run(field, func(t *testing.T) {
			sess, s, r, a := openCodeControllerFixture(t)
			ctx := context.Background()
			if field == "target" {
				r.TargetImageRef = "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("f", 64)
				if _, err := sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET target_image_ref=? WHERE id=?`, r.TargetImageRef, r.ID); err != nil {
					t.Fatal(err)
				}
			} else {
				p, err := s.loadOpenCodePlan(ctx, r)
				if err != nil {
					t.Fatal(err)
				}
				if field == "source" {
					p.Sources[0].Target.UID = "changed"
				} else {
					p.Contract.JournalSHA256 = strings.Repeat("f", 64)
				}
				raw, _ := json.Marshal(p)
				if _, err = sess.SQL().ExecContext(ctx, `UPDATE runtime_rollout_checkpoints SET state_json=? WHERE rollout_id=?`, string(raw), r.ID); err != nil {
					t.Fatal(err)
				}
			}
			if err := s.advanceOpenCodeUpgrade(ctx, r); err == nil {
				t.Fatal("changed frozen identity was accepted")
			}
			if len(a.d.upgradePoolCalls) != 0 || a.d.activated != 0 {
				t.Fatal("mutated pool before rejecting identity")
			}
		})
	}
}

func TestOpenCodeNativeMySQLControllerClaimRejectsConcurrentDispatch(t *testing.T) {
	sess, s, r, a := openCodeControllerFixture(t)
	ctx := context.Background()
	unlock, err := repository.LockOpenCodeUpgradeController(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.advanceOpenCodeUpgrade(ctx, r); !errors.Is(err, repository.ErrUpgradeCapacityPending) {
		unlock()
		t.Fatalf("concurrent controller entered: %v", err)
	}
	if len(a.d.upgradePoolCalls) != 0 {
		unlock()
		t.Fatal("concurrent controller mutated native pools")
	}
	unlock()
	if err = s.advanceOpenCodeUpgrade(ctx, r); err != nil {
		t.Fatal(err)
	}
}

func TestOpenCodeNativeMySQLRollbackRetirementResumesAfterCommittedScaleResponseLoss(t *testing.T) {
	_, s, r, a := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	a.targetFails = true
	a.d.loseRetireResponse = true
	lost := false
	for i := 0; i < 65; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if current.Status == "error" {
			break
		}
		err := s.advanceOpenCodeUpgrade(ctx, current)
		if errors.Is(err, context.DeadlineExceeded) {
			lost = true
			continue
		}
		if err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	if !lost || current.Status != "error" || stringValue(current.RollbackStatus) != "restored" {
		t.Fatalf("rollback failed takeover after scale CAS: lost=%v rollout=%+v", lost, current)
	}
}

func (d *openCodeFlowDeployments) EnsureOpenCodeUpgradePool(ctx context.Context, source *k8s.OpenCodeUpgradeSource, name, image, operation string) (k8s.RuntimeRolloutTarget, error) {
	if err := d.EnsureUpgradePool(ctx, source.Target.Namespace, source.Target.Name, name, image, operation); err != nil {
		return k8s.RuntimeRolloutTarget{}, err
	}
	return d.target.Target, nil
}
func (d *openCodeFlowDeployments) ValidateOpenCodeUpgradeTarget(context.Context, *k8s.OpenCodeUpgradeSource, k8s.RuntimeRolloutTarget, string) error {
	return d.targetErr
}

func TestOpenCodeNativeMySQLRejectsTargetTransformDriftBeforeSourceStop(t *testing.T) {
	_, s, r, a := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	if err := s.advanceOpenCodeUpgrade(ctx, r); err != nil {
		t.Fatal(err)
	}
	a.d.targetErr = errors.New("worker mount/standby environment differs from frozen source transform")
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	if err := s.advanceOpenCodeUpgrade(ctx, current); err == nil {
		t.Fatal("target drift was accepted")
	}
	if a.sourceStopped || a.migrations != 0 || a.creates != 0 || a.d.activated != 0 {
		t.Fatal("target drift reached source stop or activation")
	}
}

func TestOpenCodeNativeMySQLDefaultQueryKeepsTwoGenerationLineageAndIgnoresLegacyAudit(t *testing.T) {
	sess, _, legacy, _ := openCodeControllerFixture(t)
	ctx := context.Background()
	repo := repository.NewRuntimeRolloutRepository(sess)
	if _, err := sess.SQL().ExecContext(ctx, `UPDATE runtime_rollouts SET status='finished',preflight_id=NULL,preflight_json=NULL WHERE id=?`, legacy.ID); err != nil {
		t.Fatal(err)
	}
	source := k8s.RuntimeRolloutTarget{Namespace: "runtime", Name: "opencode-runtime", UID: "source-deployment-uid", Image: openCodeLegacyImage}
	insert := func(source k8s.RuntimeRolloutTarget, image string) *models.RuntimeRollout {
		t.Helper()
		preflight, _ := json.Marshal(map[string]any{"strategy": RuntimeUpgradeStrategyOpenCodePreserve, "source_deployment_proofs": []k8s.RuntimeRolloutTarget{source}})
		preflightString := string(preflight)
		sources, _ := json.Marshal(map[string]string{source.Namespace + "/" + source.Name: source.Image})
		sourcesString := string(sources)
		proof, fingerprint := strings.TrimPrefix(imageDigestFromReference(image), "sha256:"), "fingerprint"
		r := &models.RuntimeRollout{RuntimeType: "opencode", Status: "finished", Phase: "gateway_restart", TargetImageRef: image, PreflightJSON: &preflightString, SourceImagesJSON: &sourcesString, PreflightID: &proof, PlanFingerprint: &fingerprint, BatchSize: 1, MaxUnavailable: 1}
		if err := repo.Create(ctx, r); err != nil {
			t.Fatal(err)
		}
		return r
	}
	firstImage := "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("b", 64)
	first := insert(source, firstImage)
	secondSource := k8s.RuntimeRolloutTarget{Namespace: "runtime", Name: runtimeUpgradeTargetDeploymentName(source.Name, first.ID), UID: "first-successor-uid", Image: firstImage}
	secondImage := "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("c", 64)
	second := insert(secondSource, secondImage)
	hermesImage := "registry.local/hermes@sha256:" + strings.Repeat("d", 64)
	if err := repo.Create(ctx, &models.RuntimeRollout{RuntimeType: "hermes", Status: "finished", TargetImageRef: hermesImage, BatchSize: 1, MaxUnavailable: 1}); err != nil {
		t.Fatal(err)
	}
	history, checkpoints, err := runtimeScaleDefaultRollouts(ctx, sess, "opencode")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 3 {
		t.Fatalf("default SQL loader discarded lineage: %d records", len(history))
	}
	targets, err := runtimeDefaultScaleTargets("runtime", "opencode", history, checkpoints)
	if err != nil || len(targets) != 1 || targets[0].DeploymentName != runtimeUpgradeTargetDeploymentName(secondSource.Name, second.ID) {
		t.Fatalf("default targets=%+v err=%v", targets, err)
	}
	images, err := LoadCommittedRuntimeImages(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	if images.Images["opencode"] != secondImage || images.Images["hermes"] != hermesImage {
		t.Fatalf("legacy audit contaminated committed defaults: %+v", images.Images)
	}
}

func (a *openCodeFlowAgent) openCodeActivateUpgrade(ctx context.Context, pod models.RuntimePod, id string) error {
	if stringValue(pod.PodUID) != "target-pod-uid" {
		return errors.New("wrong target UID")
	}
	return a.ActivateUpgrade(ctx, stringValue(pod.AgentEndpoint), id)
}

func TestOpenCodeNativeMySQLOldCapabilityTargetCannotStopSource(t *testing.T) {
	for _, active := range []bool{false, true} {
		for _, count := range []int{4, 5} {
			t.Run(fmt.Sprintf("active=%v/caps=%d", active, count), func(t *testing.T) {
				fixture := openCodeControllerFixture
				if active {
					fixture = openCodeActiveControllerFixture
				}
				sess, s, r, a := fixture(t)
				ctx := context.Background()
				if err := s.advanceOpenCodeUpgrade(ctx, r); err != nil {
					t.Fatal(err)
				}
				caps := []string{"opencode.native-preserve-v1", "opencode.workspace.writer-lease", "opencode.session-continuity-v1", "opencode.runtime-standby-v1", "opencode.native-payload-poduid-v1"}
				raw, _ := json.Marshal(caps[:count])
				if _, err := sess.SQL().ExecContext(ctx, `UPDATE runtime_pods SET capabilities_json=? WHERE id=2`, string(raw)); err != nil {
					t.Fatal(err)
				}
				current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
				err := s.advanceOpenCodeUpgrade(ctx, current)
				missing := "opencode.native-payload-poduid-v1"
				if count == 5 {
					missing = "opencode.scoped-payload-grants-v1"
				}
				if err == nil || !strings.Contains(err.Error(), missing) {
					t.Fatalf("old target accepted: err=%v", err)
				}
				if a.sourceStopped || a.creates != 0 || a.migrations != 0 || a.compatibilityCalls != 0 || a.acquireCalls != 0 || a.d.activated != 0 {
					t.Fatal("old target reached source stop/native lease/execution")
				}
				if active {
					var fences int
					row, _ := sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_workspace_write_fences`)
					if err := row.Scan(&fences); err != nil || fences != 0 {
						t.Fatalf("source fenced before capability admission: %d %v", fences, err)
					}
				}
				latest, _ := s.rolloutRepo.GetByID(ctx, r.ID)
				if latest.Status != "running" || !s.upgrade.InstanceBlocked(ctx, RuntimeTypeOpenCode, 7) {
					t.Fatal("capability refusal lost active type protection")
				}
			})
		}
	}
}

func TestOpenCodeNativeMySQLRechecksScopedCapabilityBeforeQuiesce(t *testing.T) {
	sess, s, r, a := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if err := s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	old := []string{"opencode.native-preserve-v1", "opencode.workspace.writer-lease", "opencode.session-continuity-v1", "opencode.runtime-standby-v1", "opencode.native-payload-poduid-v1"}
	raw, _ := json.Marshal(old)
	if _, err := sess.SQL().Exec(`UPDATE runtime_pods SET capabilities_json=? WHERE id=2`, string(raw)); err != nil {
		t.Fatal(err)
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	if err := s.advanceOpenCodeUpgrade(ctx, current); err == nil || !strings.Contains(err.Error(), "opencode.scoped-payload-grants-v1") {
		t.Fatalf("post-freeze old capability accepted: %v", err)
	}
	var fences int
	row, _ := sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_workspace_write_fences`)
	if err := row.Scan(&fences); err != nil || fences != 0 {
		t.Fatalf("source fenced before capability recheck: %d %v", fences, err)
	}
	if a.sourceStopped || a.compatibilityCalls != 0 || a.acquireCalls != 0 || a.migrations != 0 {
		t.Fatal("capability regression reached stop/native maintenance")
	}
}

func (d *openCodeFlowDeployments) ValidateOpenCodeUpgradeScheduling(context.Context, *k8s.OpenCodeUpgradeSource, *k8s.OpenCodeUpgradeSource, string, string) error {
	return d.targetErr
}
func (d *openCodeFlowDeployments) SetOpenCodeUpgradePoolActive(ctx context.Context, source, target *k8s.OpenCodeUpgradeSource, operation string, active bool) error {
	if d.targetErr != nil {
		return d.targetErr
	}
	return d.SetUpgradePoolActive(ctx, source.Target.Namespace, source.Target.Name, target.Target.Name, operation, active)
}

func TestOpenCodeNativeMySQLRechecksTargetSchedulingAndLineageAfterStandby(t *testing.T) {
	_, s, r, a := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if err := s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	a.d.targetErr = errors.New("target scheduling or source lineage changed after frozen standby")
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	if err := s.advanceOpenCodeUpgrade(ctx, current); err == nil {
		t.Fatal("post-standby target metadata drift was accepted")
	}
	if a.sourceStopped || a.creates != 0 || a.migrations != 0 {
		t.Fatal("metadata drift reached source stop or native writes")
	}
}

func TestOpenCodeNativeMySQLCapabilityRecheckRetainsExactPhysicalAndRegistrationIdentity(t *testing.T) {
	for _, drift := range []string{"missing-registration-uid", "wrong-registration-id", "physical-image"} {
		t.Run(drift, func(t *testing.T) {
			sess, s, r, a := openCodeActiveControllerFixture(t)
			ctx := context.Background()
			for i := 0; i < 2; i++ {
				current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
				if err := s.advanceOpenCodeUpgrade(ctx, current); err != nil {
					t.Fatal(err)
				}
			}
			switch drift {
			case "missing-registration-uid":
				if _, err := sess.SQL().Exec(`UPDATE runtime_pods SET pod_uid=NULL WHERE id=2`); err != nil {
					t.Fatal(err)
				}
			case "wrong-registration-id":
				if _, err := sess.SQL().Exec(`UPDATE runtime_pods SET id=3 WHERE id=2`); err != nil {
					t.Fatal(err)
				}
			case "physical-image":
				for i := range a.d.pods {
					if a.d.pods[i].PodName == "target-pod" {
						a.d.pods[i].ImageRef = "registry/changed@sha256:" + strings.Repeat("b", 64)
						a.d.pods[i].ImageDigest = imageDigestFromReference(a.d.pods[i].ImageRef)
					}
				}
			}
			current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
			if err := s.advanceOpenCodeUpgrade(ctx, current); err == nil {
				t.Fatal("unproven current target registration/physical image admitted")
			}
			var fences int
			row, _ := sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_workspace_write_fences`)
			if err := row.Scan(&fences); err != nil || fences != 0 {
				t.Fatalf("target drift fenced source: %d %v", fences, err)
			}
			if a.sourceStopped || a.compatibilityCalls != 0 || a.acquireCalls != 0 || a.migrations != 0 {
				t.Fatal("target drift caused source/native mutations")
			}
		})
	}
}

func TestOpenCodeNativeMySQLSuccessfulCheckpointClearsOnlyResolvedCapabilityRefusal(t *testing.T) {
	for _, message := range []string{"OpenCode target lacks opencode.scoped-payload-grants-v1", "unrelated native error remains recorded"} {
		t.Run(message, func(t *testing.T) {
			sess, s, r, _ := openCodeActiveControllerFixture(t)
			ctx := context.Background()
			for i := 0; i < 2; i++ {
				current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
				if err := s.advanceOpenCodeUpgrade(ctx, current); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := sess.SQL().Exec(`UPDATE runtime_rollouts SET error_message=? WHERE id=?`, message, r.ID); err != nil {
				t.Fatal(err)
			}
			current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
			if err := s.advanceOpenCodeUpgrade(ctx, current); err != nil {
				t.Fatal(err)
			}
			latest, _ := s.rolloutRepo.GetByID(ctx, r.ID)
			expected := message
			if strings.Contains(message, "opencode.scoped-payload-grants-v1") {
				expected = ""
			}
			if stringValue(latest.ErrorMessage) != expected || latest.Status != "running" || !s.upgrade.InstanceBlocked(ctx, RuntimeTypeOpenCode, 7) {
				t.Fatalf("checkpoint lost accurate refusal/protection: %+v", latest)
			}
		})
	}
}
