package services

import (
	"clawreef/internal/models"
	"context"
	"encoding/json"
	"os"
	"testing"
)

func TestOpenCodeNativeCheckpointIsNeverGenericProgress(t *testing.T) {
	data, err := os.ReadFile("testdata/opencode_rollout14_preserve.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, emptyTargets := range []bool{true, false} {
		var plan map[string]any
		if err := json.Unmarshal(data, &plan); err != nil {
			t.Fatal(err)
		}
		if emptyTargets {
			plan["stage"] = "standby"
			plan["targets"] = []any{}
			plan["target_controllers"] = []any{}
		}
		raw, _ := json.Marshal(plan)
		repo := &checkpointRepo{raw: string(raw)}
		if p, err := LoadRuntimePoolProgress(context.Background(), repo, 14); err != nil || p != nil {
			t.Fatalf("native checkpoint parsed as ordinary progress: p=%+v err=%v", p, err)
		}
		if p, err := LoadRuntimePoolOperation(context.Background(), repo, 14); err != nil || p != nil {
			t.Fatalf("native checkpoint parsed as OpenClaw operation: p=%+v err=%v", p, err)
		}
		if repo.raw != string(raw) || len(repo.statuses) != 0 {
			t.Fatal("read model changed durable protection")
		}
	}
}

func TestOpenCodeNativeViewKeepsHistoricalErrorScopedToObservation(t *testing.T) {
	data, err := os.ReadFile("testdata/opencode_rollout14_preserve.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, scenario := range []string{"current-payload-refusal", "other-stage", "generic-error", "finished", "capsule-present"} {
		t.Run(scenario, func(t *testing.T) {
			var p openCodeUpgradePlan
			if err := json.Unmarshal(data, &p); err != nil {
				t.Fatal(err)
			}
			message := "OpenCode native payload must be an immutable root-owned regular file"
			preflight := string(data)
			r := &models.RuntimeRollout{ID: 14, RuntimeType: RuntimeTypeOpenCode, Status: "running", PreflightJSON: &preflight, ErrorMessage: &message}
			switch scenario {
			case "other-stage":
				p.Stage = "restart"
			case "generic-error":
				message = "temporary control-plane error"
			case "finished":
				r.Status = "finished"
			case "capsule-present":
				p.Instances[0].Capsule = &RuntimeAgentSessionSQLiteMigration{}
			}
			raw, _ := json.Marshal(p)
			repo := &checkpointRepo{raw: string(raw)}
			v, err := LoadOpenCodeUpgradeOperation(context.Background(), repo, r)
			if err != nil {
				t.Fatal(err)
			}
			if v.Blocked != (scenario == "current-payload-refusal") || v.ProtectionHeld != (scenario != "finished") || v.ErrorScope != "last_recorded_controller_error_not_revalidated" || v.ProtectionScope != "active_runtime_type_guard" {
				t.Fatalf("misclassified historical/native protection: %+v", v)
			}
			if v.Instances[0].State != "quiesced" || !v.Instances[0].HasStopProof || v.TargetControllers[0].DeploymentUID != "b0e149af-494f-47d8-8686-0810e9fde1e9" || v.Targets[0].PodUID != "91fcf4b2-ffc9-42a7-af95-00ae38aba9cc" {
				t.Fatalf("recorded state/identity lost: %+v", v)
			}
			if repo.raw != string(raw) || len(repo.statuses) != 0 {
				t.Fatal("view mutated native protection")
			}
		})
	}
}
