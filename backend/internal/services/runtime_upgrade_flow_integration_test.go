package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/repository"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

// Uses production MySQL tables, repositories, reservation and upgrade controller.
// Only the Runtime boundary is simulated; no model/user workspace is needed.
func upgradeFlowDB(t *testing.T) db.Session {
	t.Helper()
	port := os.Getenv("TEST_RUNTIME_MYSQL_PORT")
	if port == "" {
		t.Skip("isolated localhost MySQL required")
	}
	for _, c := range port {
		if c < '0' || c > '9' {
			t.Fatal("invalid port")
		}
	}
	u := mysql.ConnectionURL{Host: "127.0.0.1:" + port, User: "root", Database: "mysql", Options: map[string]string{"parseTime": "true"}}
	root, e := mysql.Open(u)
	if e != nil {
		t.Fatal(e)
	}
	name := fmt.Sprintf("upgrade_flow_%d", time.Now().UnixNano())
	if _, e = root.SQL().Exec("CREATE DATABASE " + name); e != nil {
		t.Fatal(e)
	}
	u.Database = name
	s, e := mysql.Open(u)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close(); root.SQL().Exec("DROP DATABASE " + name); root.Close() })
	exec := func(q string) {
		t.Helper()
		if _, e := s.SQL().Exec(q); e != nil {
			t.Fatalf("%v: %s", e, q)
		}
	}
	exec(`CREATE TABLE instances (id INT PRIMARY KEY,user_id INT,type VARCHAR(32),runtime_type VARCHAR(32),instance_mode VARCHAR(32),status VARCHAR(32),runtime_generation INT,workspace_path VARCHAR(1024),runtime_error_message TEXT NULL,runtime_error_code VARCHAR(64) NULL,updated_at DATETIME(6))`)
	for _, n := range []string{"users", "teams", "team_members"} {
		exec("CREATE TABLE " + n + " (id INT PRIMARY KEY)")
	}
	for _, file := range []string{"023_add_runtime_pool_v2.sql", "058_add_openclaw_runtime_upgrade_safety.sql", "061_persist_runtime_upgrade_source_identity.sql", "070_add_upgrade_restart_attempts.sql", "073_add_upgrade_operation_claims.sql", "074_add_upgrade_recovery_key.sql", "075_add_upgrade_retry_diagnostics.sql", "076_add_upgrade_item_isolation.sql"} {
		b, e := os.ReadFile("../db/migrations/" + file)
		if e != nil {
			t.Fatal(e)
		}
		text := string(b)
		if strings.HasPrefix(file, "023") {
			text = text[strings.Index(text, "CREATE TABLE IF NOT EXISTS runtime_pods"):strings.Index(text, "CREATE TABLE IF NOT EXISTS workspace_file_audits")]
		}
		for _, q := range strings.Split(text, ";") {
			if strings.TrimSpace(q) != "" {
				exec(q)
			}
		}
	}
	return s
}

type upgradeFlowAgent struct {
	*fakeRuntimeAgentClient
	states       map[string]*RuntimeAgentGatewayState
	lostResponse bool
	creating     bool
	creates      int
}

func (a *upgradeFlowAgent) GatewayState(ctx context.Context, endpoint, id string) (*RuntimeAgentGatewayState, error) {
	if s := a.states[id]; s != nil {
		return s, nil
	}
	return nil, ErrRuntimeAgentNotFound
}
func (a *upgradeFlowAgent) CreateGateway(ctx context.Context, endpoint string, req RuntimeAgentCreateGatewayRequest) (*RuntimeAgentCreateGatewayResponse, error) {
	a.creates++
	id := fmt.Sprintf("gw-%d-%d", req.InstanceID, req.Generation)
	state := "running"
	if a.creating {
		state = "creating"
	}
	a.states[id] = &RuntimeAgentGatewayState{GatewayID: id, InstanceID: req.InstanceID, Generation: req.Generation, State: state}
	if a.lostResponse {
		a.lostResponse = false
		return nil, context.DeadlineExceeded
	}
	return &RuntimeAgentCreateGatewayResponse{GatewayID: id, Port: req.GatewayPort, Status: state}, nil
}

func TestUpgradeRecoveryMySQLFlow(t *testing.T) {
	for _, scenario := range []string{"legacy-three-item-resume", "response-lost", "timeout-keeps-fence"} {
		t.Run(scenario, func(t *testing.T) {
			s := upgradeFlowDB(t)
			ctx := context.Background()
			root := t.TempDir()
			exec := func(q string, args ...any) {
				t.Helper()
				if _, e := s.SQL().Exec(q, args...); e != nil {
					t.Fatal(e)
				}
			}
			digest := "sha256:" + strings.Repeat("b", 64)
			caps, _ := json.Marshal(openClawUpgradeRequiredCapabilities)
			exec(`INSERT INTO runtime_rollouts (id,runtime_type,target_image_ref,target_image_digest,status,phase,preflight_id,source_images_json,preflight_json,batch_size,updated_at) VALUES (123,'openclaw',?,?,'running','gateway_restart','p123','{"tenant/source":"old"}',?,2,NOW()-INTERVAL 2 HOUR)`, "repo@"+digest, digest, `{"target_runtime_version":"2026.8.1","target_upgrade_protocol":"openclaw-upgrade-v3"}`)
			exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,openclaw_version,session_store,agent_protocol_version,capabilities_json) VALUES (307,'openclaw','tenant','target','uid123','source-u123',?,?,'http://agent','ready',100,'2026.8.1','sqlite','openclaw-upgrade-v3',?)`, "repo@"+digest, digest, string(caps))
			ids := []int{481}
			if scenario == "legacy-three-item-resume" {
				ids = []int{481, 482, 484}
			}
			for _, id := range ids {
				state, status, gen := "restart_ready", "creating", 2
				if id == 484 {
					state, status, gen = "migrated", "stopped", 1
				}
				exec("INSERT INTO instances VALUES (?,1,'openclaw','gateway','lite',?,?,?,NULL,NULL,NOW())", id, status, gen, RuntimeWorkspacePathWithRoot(root, "openclaw", 1, id))
				exec(`INSERT INTO runtime_upgrade_items (rollout_id,instance_id,state,source_generation,snapshot_ref,postflight_json,created_at,updated_at) VALUES (123,?,?,1,'capsule','{}',NOW(),NOW()-INTERVAL 2 HOUR)`, id, state)
			}
			pods := repository.NewRuntimePodRepository(s)
			bindings := repository.NewInstanceRuntimeBindingRepository(s)
			rollouts := repository.NewRuntimeRolloutRepository(s)
			agent := &upgradeFlowAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, states: map[string]*RuntimeAgentGatewayState{}, lostResponse: scenario == "response-lost", creating: scenario == "timeout-keeps-fence"}
			scheduler := NewRuntimeScheduler(repository.NewInstanceRepository(s), pods, bindings, rollouts, agent, nil, nil, nil, time.Second, WithRuntimeSchedulerWorkspaceRoot(root))
			upgrade := NewRuntimeUpgradeService(s, rollouts, pods, bindings, agent, root, "")
			upgrade.gatewayRestarter = scheduler
			step := func() bool {
				t.Helper()
				r, e := rollouts.GetByID(ctx, 123)
				if e != nil {
					t.Fatal(e)
				}
				p, e := pods.List(ctx, "openclaw")
				if e != nil {
					t.Fatal(e)
				}
				done, e := upgrade.ValidateTargetRuntime(ctx, r, p)
				if e != nil {
					t.Fatal(e)
				}
				return done
			}
			step()
			if scenario == "timeout-keeps-fence" {
				exec("UPDATE runtime_upgrade_items SET restart_deadline_at=NOW()-INTERVAL 1 SECOND")
				for n := 0; n < 4; n++ {
					step()
				}
				var item models.RuntimeUpgradeItem
				if e := s.Collection("runtime_upgrade_items").Find("instance_id", 481).One(&item); e != nil {
					t.Fatal(e)
				}
				if item.RecoveryStatus == nil || *item.RecoveryStatus != "needs_attention" || agent.creates != 1 {
					t.Fatalf("unbounded retry/hidden error: %+v creates=%d", item, agent.creates)
				}
				if ok, e := pods.TryClaimSlot(ctx, 307); e != nil || ok {
					t.Fatalf("timeout removed fence %v %v", ok, e)
				}
				b, e := bindings.GetByInstanceID(ctx, 481)
				if e != nil || b == nil {
					t.Fatal("timeout destroyed reservation")
				}
				// Late authoritative success is observed even in the held state.
				agent.states["gw-481-2"].State = "running"
			}
			done := false
			for n := 0; n < 8; n++ {
				if step() {
					done = true
					break
				}
			}
			if !done {
				t.Fatal("rollout could not converge")
			}
			if agent.creates != len(ids) {
				t.Fatalf("duplicate/lost starts: creates=%d instances=%d", agent.creates, len(ids))
			}
			for _, id := range ids {
				b, e := bindings.GetRunningByInstanceID(ctx, id)
				if e != nil || b == nil || b.Generation != 2 {
					t.Fatalf("instance %d not recovered: %+v %v", id, b, e)
				}
				var item models.RuntimeUpgradeItem
				if e := s.Collection("runtime_upgrade_items").Find("instance_id", id).One(&item); e != nil {
					t.Fatal(e)
				}
				if item.State != "verified" || item.RestartAttempts != 1 {
					t.Fatalf("bad final state: %+v", item)
				}
			}
		})
	}
}
