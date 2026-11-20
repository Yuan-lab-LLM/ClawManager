package services

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"clawreef/internal/models"
	"clawreef/internal/services/k8s"
)

type emptyRecoveryBackend struct {
	*fakeRuntimeDeploymentService
	proof       *k8s.EmptyRuntimePoolProof
	inspections int
}

func (f *emptyRecoveryBackend) InspectEmptyRuntimePool(_ context.Context, target k8s.RuntimePodScaleTarget) (*k8s.EmptyRuntimePoolProof, error) {
	f.inspections++
	p := *f.proof
	p.Source.Namespace = target.Namespace
	p.Source.Name = target.DeploymentName
	p.Source.UID = target.DeploymentUID
	return &p, nil
}
func (*emptyRecoveryBackend) RecoverEmptyRuntimePool(context.Context, *k8s.EmptyRuntimePoolProof, string, string, string, func(context.Context) error) error {
	return nil
}

func TestEmptyRuntimeRecoveryAdmissionMySQL(t *testing.T) {
	sess := runtimeManagementMySQLDB(t)
	management := runtimeManagementMySQLService(sess)
	s := management.scheduler
	s.management = management
	s.upgrade = &RuntimeUpgradeService{sess: sess}
	s.runtimeNamespace = "tenant"
	backend := &emptyRecoveryBackend{fakeRuntimeDeploymentService: &fakeRuntimeDeploymentService{}, proof: &k8s.EmptyRuntimePoolProof{Source: k8s.RuntimeRolloutTarget{Image: "repo/runtime@sha256:" + strings.Repeat("a", 64)}, Configuration: strings.Repeat("c", 64), SlotCount: 8}}
	s.deployments = backend
	actor := 1
	image := "repo/runtime@sha256:" + strings.Repeat("b", 64)
	// A Pro desktop is outside this Lite-pool operation.
	if _, err := sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(101,1,'openclaw','desktop','pro','running',1)"); err != nil {
		t.Fatal(err)
	}
	r, err := s.SubmitEmptyRuntimeRecovery(t.Context(), "openclaw", image, "source-uid", &actor)
	if err != nil {
		t.Fatal(err)
	}
	if r.ID == 0 || r.Status != "pending" || r.Phase != RuntimeEmptyPoolRecoveryPhase {
		t.Fatalf("no durable recovery: %+v", r)
	}
	if !s.upgrade.InstanceBlocked(t.Context(), "openclaw", 102) {
		t.Fatal("unverified empty recovery admitted a new writer")
	}
	if s.upgrade.InstanceBlocked(t.Context(), "hermes", 102) {
		t.Fatal("OpenClaw recovery blocked another Runtime")
	}
	again, err := s.SubmitEmptyRuntimeRecovery(t.Context(), "openclaw", image, "source-uid", &actor)
	if err != nil || again.ID != r.ID || backend.inspections != 1 {
		t.Fatalf("lost request created a new intent: %+v %v inspections=%d", again, err, backend.inspections)
	}
	if _, err = s.SubmitEmptyRuntimeRecovery(t.Context(), "openclaw", "repo/runtime@sha256:"+strings.Repeat("d", 64), "source-uid", &actor); err == nil {
		t.Fatal("overlapping same-type recovery was admitted")
	}
	if _, err = sess.SQL().Exec("UPDATE runtime_rollouts SET status='finished' WHERE id=?", r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = sess.SQL().Exec("INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation) VALUES(102,1,'openclaw','gateway','lite','deleting',1)"); err != nil {
		t.Fatal(err)
	}
	if _, err = s.SubmitEmptyRuntimeRecovery(t.Context(), "openclaw", "repo/runtime@sha256:"+strings.Repeat("e", 64), "source-uid", &actor); err == nil {
		t.Fatal("old Lite instance was ignored before recovery")
	}
}

func TestEmptyRuntimeRecoveryDefaultAndFutureUpgradeLineage(t *testing.T) {
	for _, rt := range []string{"openclaw", "opencode", "hermes", "deepseek-harness"} {
		t.Run(rt, func(t *testing.T) {
			image := "repo/runtime@sha256:" + strings.Repeat("b", 64)
			plan := emptyPoolRecoveryPlan{Kind: RuntimeEmptyPoolRecoveryStrategy, Version: 1, SourceUID: "uid", Proof: &k8s.EmptyRuntimePoolProof{Source: k8s.RuntimeRolloutTarget{Namespace: "tenant", Name: "site-" + rt + "-runtime", UID: "uid", Image: "repo/runtime@sha256:" + strings.Repeat("a", 64)}, Configuration: strings.Repeat("c", 64), SlotCount: 8}}
			raw, _ := json.Marshal(plan)
			sources, _ := json.Marshal(map[string]string{replacementKey(plan.Proof.Source): plan.Proof.Source.Image})
			fingerprint := emptyPoolFingerprint(plan, image)
			r := models.RuntimeRollout{ID: 20, RuntimeType: rt, Status: "finished", Phase: RuntimeEmptyPoolRecoveryPhase, TargetImageRef: image, PreflightJSON: stringPtrOrNil(string(raw)), SourceImagesJSON: stringPtrOrNil(string(sources)), PlanFingerprint: &fingerprint}
			history := []models.RuntimeRollout{{ID: 10, RuntimeType: rt, Status: "finished", TargetImageRef: "old-history-without-current-proof"}, r}
			targets, err := runtimeDefaultScaleTargets("tenant", rt, "site-"+rt+"-runtime", history, nil)
			if err != nil || len(targets) != 1 || targets[0].DeploymentUID != "uid" || targets[0].ImageRef != image {
				t.Fatalf("wrong verified default: %+v %v", targets, err)
			}
			if rt == "openclaw" || rt == "opencode" {
				sources2, _ := json.Marshal(map[string]string{replacementKey(plan.Proof.Source): image})
				preflight := "native-proof"
				nextFingerprint := "next-proof"
				phase := "image_rollout"
				var preflightJSON *string
				if rt == "opencode" {
					phase = "opencode_native_preserve"
					p := `{"strategy":"opencode_native_preserve_v1"}`
					preflightJSON = &p
				}
				next := models.RuntimeRollout{ID: 21, RuntimeType: rt, Status: "finished", Phase: phase, TargetImageRef: image, SourceImagesJSON: stringPtrOrNil(string(sources2)), PreflightID: &preflight, PlanFingerprint: &nextFingerprint, PreflightJSON: preflightJSON}
				targets, err = runtimeDefaultScaleTargets("tenant", rt, "site-"+rt+"-runtime", append(history, next), nil)
				if err != nil || len(targets) != 1 || targets[0].DeploymentName != "site-"+rt+"-runtime-u21" {
					t.Fatalf("future upgrade lost empty-recovery lineage: %+v %v", targets, err)
				}
			}
			r.PlanFingerprint = stringPtrOrNil("tampered")
			if _, err = emptyRecoveryScaleTarget("tenant", rt, r); err == nil {
				t.Fatal("tampered proof became the default")
			}
		})
	}
}
