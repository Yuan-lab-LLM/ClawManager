package services

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/services/k8s"
	"github.com/upper/db/v4"
)

type forceFlowBackend struct {
	*openCodeFlowDeployments
	zeroCalls, replaceCalls, probeCalls int
	oldGone                             bool
	probeErr                            error
	loseScale                           bool
}

func (d *forceFlowBackend) ObserveOpenCodeForceTargetZero(context.Context, *k8s.OpenCodeUpgradeSource, *k8s.OpenCodeUpgradeSource, string, string) (bool, error) {
	return d.zeroCalls > 0, nil
}
func (d *forceFlowBackend) ScaleOpenCodeForceTargetZero(context.Context, *k8s.OpenCodeUpgradeSource, *k8s.OpenCodeUpgradeSource, string, string) error {
	d.zeroCalls++
	if d.loseScale {
		d.loseScale = false
		return context.DeadlineExceeded
	}
	return nil
}
func (d *forceFlowBackend) ObserveOpenCodeForceTarget(_ context.Context, _, old *k8s.OpenCodeUpgradeSource, image, op, request string) (bool, error) {
	if d.target.Target.Image != image {
		return false, errors.New("new image not committed")
	}
	return d.oldGone, nil
}
func (d *forceFlowBackend) OpenCodeForceOldPodsGone(context.Context, *k8s.OpenCodeUpgradeSource, string) (bool, error) {
	return d.oldGone, nil
}
func (d *forceFlowBackend) ReplaceOpenCodeForceTarget(ctx context.Context, source, old *k8s.OpenCodeUpgradeSource, image, op, request string) error {
	if !d.oldGone {
		return k8s.ErrRuntimeFaultEvictionPending
	}
	d.replaceCalls++
	if d.target.Target.Image == image {
		return nil
	}
	d.target.Target.Image = image
	d.target.PodUIDs = []string{"force-new-uid"}
	caps, _ := json.Marshal(openCodeUpgradeCapabilities)
	_, err := d.sess.SQL().Exec(`INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,pod_ip,deployment_name,image_ref,image_digest,state,capacity,used_slots,last_seen_at,agent_endpoint,agent_protocol_version,session_store,capabilities_json) VALUES(3,'opencode','runtime','force-target-pod','force-new-uid','10.0.0.3',?,?,?,'pending',8,0,NOW(),'http://10.0.0.3:19090',?,'sqlite',?)`, old.Target.Name, image, imageDigestFromReference(image), openCodeNativeProtocol, string(caps))
	if err != nil {
		return err
	}
	for i := range d.pods {
		if d.pods[i].PodName == "target-pod" {
			d.pods[i].PodName = "force-target-pod"
			d.pods[i].PodUID = "force-new-uid"
			d.pods[i].PodIP = stringPtrOrNil("10.0.0.3")
			d.pods[i].ImageRef = image
			d.pods[i].ImageDigest = imageDigestFromReference(image)
		}
	}
	return nil
}
func (d *forceFlowBackend) InspectOpenCodeForceLeaseAbsence(context.Context, *k8s.OpenCodeUpgradeSource, models.RuntimePod, []int) (string, error) {
	d.probeCalls++
	if d.probeErr != nil {
		return "", d.probeErr
	}
	return strings.Repeat("e", 64), nil
}

type forceFlowAgent struct {
	*openCodeFlowAgent
	proofErr    error
	inactiveErr error
}

func (a *forceFlowAgent) openCodeForceInactiveTarget(ctx context.Context, pod models.RuntimePod, _ json.RawMessage) error {
	if a.inactiveErr != nil {
		return a.inactiveErr
	}
	return a.openCodePoolIdle(ctx, pod)
}
func (a *forceFlowAgent) openCodeForceReadOnlySourceProof(context.Context, k8s.RuntimeAssignment, json.RawMessage) error {
	return a.proofErr
}
func (a *forceFlowAgent) openCodeForceNativeEvidence(context.Context, int64, []openCodeUpgradeInstance, models.RuntimePod) (json.RawMessage, error) {
	if a.proofErr != nil {
		return nil, a.proofErr
	}
	return json.RawMessage(`[{"record_uid":"frozen-failed-receipt","result_sha256":"` + strings.Repeat("f", 64) + `"}]`), nil
}

func openCodeForceFixture(t *testing.T) (db.Session, *RuntimeScheduler, *models.RuntimeRollout, *forceFlowAgent, *forceFlowBackend) {
	t.Helper()
	sess, s, r, agent := openCodeActiveControllerFixture(t)
	ctx := context.Background()
	for i := 0; i < 4; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if err := s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	p, err := s.loadOpenCodePlan(ctx, current)
	if err != nil || p.Stage != "preserve" {
		t.Fatal(p, err)
	}
	agent.d.target.PodUIDs = []string{"target-pod-uid"}
	p.TargetProofs[0].PodUIDs = []string{"target-pod-uid"}
	if err = s.saveOpenCodePlan(ctx, current, p); err != nil {
		t.Fatal(err)
	}
	b := &forceFlowBackend{openCodeFlowDeployments: agent.d}
	a := &forceFlowAgent{openCodeFlowAgent: agent}
	s.agentClient = a
	s.deployments = b
	s.ocForceInspect = func(context.Context, string, map[string]string) (openCodeNativeContract, error) {
		return p.Contract, nil
	}
	return sess, s, current, a, b
}
func TestOpenCodeForceRetryMySQLAuditsThenReplacesInactiveTargetAndResumes(t *testing.T) {
	sess, s, r, a, b := openCodeForceFixture(t)
	ctx := context.Background()
	image := "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("b", 64)
	pf, err := s.PreflightOpenCodeForceRetry(ctx, r.ID, image)
	if err != nil || !pf.Allowed {
		t.Fatalf("safe preserved source refused: %+v %v", pf, err)
	}
	req := OpenCodeForceRetryRequest{TargetImageRef: image, Force: true, RequestID: "81e247de-ef46-41ad-853e-0bb20524303e", Fingerprint: pf.Fingerprint}
	confirmed, err := s.ConfirmOpenCodeForceRetry(ctx, r.ID, req, 1)
	if err != nil || confirmed.ID != r.ID || confirmed.TargetImageRef != image {
		t.Fatalf("confirm: %+v %v", confirmed, err)
	}
	again, err := s.ConfirmOpenCodeForceRetry(ctx, r.ID, req, 1)
	if err != nil || again.ID != r.ID {
		t.Fatal("same UUID created another attempt", err)
	}
	if b.zeroCalls != 0 || b.replaceCalls != 0 || a.migrations != 0 {
		t.Fatal("confirmation dispatched target mutation before durable audit")
	}
	var audits int
	row, _ := sess.SQL().QueryRow(`SELECT COUNT(*) FROM runtime_upgrade_audits WHERE rollout_id=? AND action='opencode_force_retry'`, r.ID)
	if err = row.Scan(&audits); err != nil || audits != 1 {
		t.Fatal(audits, err)
	}
	for i := 0; i < 3; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		_ = s.advanceOpenCodeUpgrade(ctx, current)
	}
	if b.replaceCalls != 0 || a.migrations != 0 || a.acquireCalls != 0 {
		t.Fatal("old surviving Pod allowed new image/native writes")
	}
	b.oldGone = true
	for i := 0; i < 7; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatal(err)
		}
		p, _ := s.loadOpenCodePlan(ctx, current)
		if len(p.Instances) > 0 && p.Instances[0].State == "migrated" {
			break
		}
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	p, err := s.loadOpenCodePlan(ctx, current)
	if err != nil || p.Targets[0].PodUID == nil || *p.Targets[0].PodUID != "force-new-uid" || p.Instances[0].Capsule == nil {
		t.Fatalf("fresh preserve did not resume: %+v %v", p, err)
	}
	if current.Status != "running" || a.creates != 0 || !s.upgrade.InstanceBlocked(ctx, RuntimeTypeOpenCode, 7) {
		t.Fatal("force prematurely published default/released guard")
	}
	var sourceGen int
	row, _ = sess.SQL().QueryRow(`SELECT runtime_generation FROM instances WHERE id=7`)
	if err = row.Scan(&sourceGen); err != nil || sourceGen != 3 {
		t.Fatal(sourceGen, err)
	}
}
func TestOpenCodeForceRetryMySQLRefusesUnprovedInputsBeforeMutation(t *testing.T) {
	for _, scenario := range []string{"capsule", "stage", "source-proof", "lease", "binding", "fingerprint", "force", "foreign-request", "target-pod-proof", "target-foreign-binding"} {
		t.Run(scenario, func(t *testing.T) {
			sess, s, r, a, b := openCodeForceFixture(t)
			ctx := context.Background()
			image := "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("b", 64)
			if scenario == "source-proof" {
				a.proofErr = errors.New("unknown source C1")
			}
			if scenario == "lease" {
				b.probeErr = errors.New("existing or unknown lease")
			}
			if scenario == "capsule" || scenario == "stage" {
				p, _ := s.loadOpenCodePlan(ctx, r)
				if scenario == "capsule" {
					p.Instances[0].Capsule = &RuntimeAgentSessionSQLiteMigration{Status: "validated"}
				} else {
					p.Stage = "activate"
				}
				if err := s.saveOpenCodePlan(ctx, r, p); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "target-pod-proof" {
				p, _ := s.loadOpenCodePlan(ctx, r)
				p.TargetProofs[0].PodUIDs = []string{"foreign-pod"}
				if err := s.saveOpenCodePlan(ctx, r, p); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "target-foreign-binding" {
				if _, err := sess.SQL().Exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(8,1,'opencode','gateway','lite','creating',1,NOW())`); err != nil {
					t.Fatal(err)
				}
				if _, err := sess.SQL().Exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation,created_at,updated_at) VALUES(8,2,'opencode','foreign',32001,'/workspaces/opencode/user-1/instance-8','reserved',1,NOW(),NOW())`); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "binding" {
				if _, err := sess.SQL().Exec(`INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation,created_at,updated_at) VALUES(7,2,'opencode','foreign',32000,'/workspaces/opencode/user-1/instance-7','running',3,NOW(),NOW())`); err != nil {
					t.Fatal(err)
				}
			}
			pf, err := s.PreflightOpenCodeForceRetry(ctx, r.ID, image)
			if scenario == "fingerprint" || scenario == "force" || scenario == "foreign-request" {
				if err != nil {
					t.Fatal(err)
				}
				req := OpenCodeForceRetryRequest{TargetImageRef: image, Force: true, RequestID: "81e247de-ef46-41ad-853e-0bb20524303e", Fingerprint: pf.Fingerprint}
				if scenario == "fingerprint" {
					req.Fingerprint = strings.Repeat("0", 64)
				}
				if scenario == "force" {
					req.Force = false
				}
				if scenario == "foreign-request" {
					if _, e := s.ConfirmOpenCodeForceRetry(ctx, r.ID, req, 1); e != nil {
						t.Fatal(e)
					}
					req.RequestID = "b187361c-5e4c-438b-a2fb-15b70b09ea2a"
				}
				_, err = s.ConfirmOpenCodeForceRetry(ctx, r.ID, req, 1)
			}
			if err == nil {
				t.Fatal("unsafe force request admitted", scenario)
			}
			if b.zeroCalls != 0 || b.replaceCalls != 0 || a.acquireCalls != 0 || a.migrations != 0 {
				t.Fatal("rejected force mutated target/native state")
			}
		})
	}
}

func TestOpenCodeForceRetryMySQLRechecksLeaseBeforeAcquireAndKeepsGuards(t *testing.T) {
	sess, s, r, a, b := openCodeForceFixture(t)
	ctx := context.Background()
	image := "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("b", 64)
	_, _ = sess.SQL().Exec(`UPDATE runtime_rollouts SET error_message=? WHERE id=?`, openCodeScopedPayloadMissing, r.ID)
	pf, err := s.PreflightOpenCodeForceRetry(ctx, r.ID, image)
	if err != nil {
		t.Fatal(err)
	}
	req := OpenCodeForceRetryRequest{TargetImageRef: image, Force: true, RequestID: "81e247de-ef46-41ad-853e-0bb20524303e", Fingerprint: pf.Fingerprint}
	if _, err = s.ConfirmOpenCodeForceRetry(ctx, r.ID, req, 1); err != nil {
		t.Fatal(err)
	}
	b.oldGone = true
	for i := 0; i < 3; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	p, err := s.loadOpenCodePlan(ctx, current)
	if err != nil || p.Stage != "preserve" {
		t.Fatal(p, err)
	}
	b.probeErr = errors.New("unknown existing lease even if expired")
	if err = s.advanceOpenCodeUpgrade(ctx, current); err == nil {
		t.Fatal("unknown lease admitted Acquire")
	}
	if a.acquireCalls != 0 || a.migrations != 0 || a.releaseCalls != 0 {
		t.Fatal("unknown lease triggered a writer or clearing")
	}
	if !s.upgrade.InstanceBlocked(ctx, RuntimeTypeOpenCode, 7) {
		t.Fatal("type protection released")
	}
	var count int
	row, _ := sess.SQL().QueryRow(`SELECT COUNT(*) FROM instance_workspace_write_fences WHERE instance_id=7 AND state='closed'`)
	if err = row.Scan(&count); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	var audit string
	row, _ = sess.SQL().QueryRow(`SELECT detail_json FROM runtime_upgrade_audits WHERE rollout_id=? AND action='opencode_force_retry'`, r.ID)
	if err = row.Scan(&audit); err != nil || !strings.Contains(audit, openCodeScopedPayloadMissing) || !strings.Contains(audit, "old_checkpoint") {
		t.Fatal("original evidence not retained", err)
	}
	// The original failed-attempt error is audited but cannot re-block the fresh
	// force attempt after confirmation. Only the new error remains observable.
	fresh, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	view, err := LoadOpenCodeUpgradeOperation(ctx, s.rolloutRepo, fresh)
	if err != nil || view.ForceRetry == nil || view.ForceRetry.RequestID != req.RequestID {
		t.Fatal(view, err)
	}
	public, _ := json.Marshal(view)
	if strings.Contains(string(public), "lease_evidence") || strings.Contains(string(public), "native_evidence") || strings.Contains(string(public), "old_checkpoint") {
		t.Fatal("public force summary leaked private audit")
	}
}

func TestOpenCodeForceRetryMySQLWaitsForNewPodAndRegistrationWithoutFalseBlock(t *testing.T) {
	sess, s, r, a, b := openCodeForceFixture(t)
	ctx := context.Background()
	image := "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("b", 64)
	pf, err := s.PreflightOpenCodeForceRetry(ctx, r.ID, image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConfirmOpenCodeForceRetry(ctx, r.ID, OpenCodeForceRetryRequest{TargetImageRef: image, Force: true, RequestID: "81e247de-ef46-41ad-853e-0bb20524303e", Fingerprint: pf.Fingerprint}, 1); err != nil {
		t.Fatal(err)
	}
	b.oldGone = true
	for i := 0; i < 2; i++ {
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatal(err)
		}
	}
	saved := b.pods
	b.pods = nil
	for _, window := range []string{"no-physical-pod", "no-registration"} {
		if window == "no-registration" {
			b.pods = saved
			if _, err = sess.SQL().Exec(`UPDATE runtime_pods SET deployment_name='registration-pending' WHERE id=3`); err != nil {
				t.Fatal(err)
			}
		}
		current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
		if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
			t.Fatalf("normal %s window treated as blocked: %v", window, err)
		}
		current, _ = s.rolloutRepo.GetByID(ctx, r.ID)
		view, e := LoadOpenCodeUpgradeOperation(ctx, s.rolloutRepo, current)
		if e != nil || view.Blocked || view.Stage != "force_ready" || !view.ProtectionHeld {
			t.Fatal(view, e)
		}
	}
	if a.migrations != 0 || a.acquireCalls != 0 {
		t.Fatal("missing new readiness permitted native writes")
	}
}

func TestOpenCodeForceRetryMySQLControllerContentionDoesNotOverwriteRecordedError(t *testing.T) {
	sess, s, r, a, b := openCodeForceFixture(t)
	ctx := context.Background()
	original := openCodeScopedPayloadMissing
	if _, err := sess.SQL().Exec(`UPDATE runtime_rollouts SET error_message=? WHERE id=?`, original, r.ID); err != nil {
		t.Fatal(err)
	}
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	release, err := repository.LockOpenCodeUpgradeController(ctx, sess)
	if err != nil {
		t.Fatal(err)
	}
	err = s.advanceOpenCodeUpgrade(ctx, current)
	release()
	if !errors.Is(err, repository.ErrUpgradeCapacityPending) {
		t.Fatal("expected lock contention", err)
	}
	latest, err := s.rolloutRepo.GetByID(ctx, r.ID)
	if err != nil || stringValue(latest.ErrorMessage) != original || latest.Status != "running" {
		t.Fatal("contention changed known operation", latest, err)
	}
	if b.zeroCalls != 0 || a.acquireCalls != 0 || a.migrations != 0 {
		t.Fatal("contending tick performed work")
	}
}

func TestOpenCodeForceRetryMySQLLostZeroReplyDoesNotReinspectGoneTargetOrResubmitZero(t *testing.T) {
	_, s, r, a, b := openCodeForceFixture(t)
	ctx := context.Background()
	image := "clawmanager.local/opencode-lite@sha256:" + strings.Repeat("b", 64)
	pf, err := s.PreflightOpenCodeForceRetry(ctx, r.ID, image)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.ConfirmOpenCodeForceRetry(ctx, r.ID, OpenCodeForceRetryRequest{TargetImageRef: image, Force: true, RequestID: "81e247de-ef46-41ad-853e-0bb20524303e", Fingerprint: pf.Fingerprint}, 1); err != nil {
		t.Fatal(err)
	}
	b.loseScale = true
	current, _ := s.rolloutRepo.GetByID(ctx, r.ID)
	if err = s.advanceOpenCodeUpgrade(ctx, current); err != context.DeadlineExceeded {
		t.Fatal("unknown zero not held", err)
	}
	a.inactiveErr = errors.New("old target endpoint gone")
	current, _ = s.rolloutRepo.GetByID(ctx, r.ID)
	if err = s.advanceOpenCodeUpgrade(ctx, current); err != nil {
		t.Fatal("committed zero required old endpoint", err)
	}
	current, _ = s.rolloutRepo.GetByID(ctx, r.ID)
	p, err := s.loadOpenCodePlan(ctx, current)
	if err != nil || p.Stage != "force_replace" || b.zeroCalls != 1 || a.acquireCalls != 0 {
		t.Fatal(p, err, b.zeroCalls, a.acquireCalls)
	}
}
