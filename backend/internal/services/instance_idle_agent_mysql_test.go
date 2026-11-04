package services

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"clawreef/internal/repository"
	"github.com/upper/db/v4"
)

func idleLifecycleDB(t *testing.T) db.Session {
	t.Helper()
	s := upgradeFlowDB(t)
	for _, q := range []string{`ALTER TABLE instances ADD access_token VARCHAR(255) NULL,ADD agent_bootstrap_token VARCHAR(255) NULL`, `CREATE TABLE gateway_runtime_tasks(instance_id INT,generation INT,finished BOOLEAN)`, `CREATE TABLE instance_workspace_write_intents(instance_id INT)`} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"007_add_instance_agent_control_plane.sql", "096_add_instance_auto_stop.sql", "097_add_instance_auto_stop_scan.sql"} {
		raw, err := os.ReadFile("../db/migrations/" + name)
		if err != nil {
			t.Fatal(err)
		}
		text := string(raw)
		if strings.HasPrefix(name, "007") {
			text = text[strings.Index(text, "CREATE TABLE IF NOT EXISTS instance_agents"):]
		}
		for _, q := range strings.Split(text, ";") {
			if strings.TrimSpace(q) != "" {
				if _, err = s.SQL().Exec(q); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	return s
}

func TestIdleLegacyAgentCannotRegisterAuthenticateOrRenewNativeLite(t *testing.T) {
	s := idleLifecycleDB(t)
	instances := repository.NewInstanceRepository(s)
	agents := repository.NewInstanceAgentRepository(s)
	svc := NewInstanceAgentService(instances, agents, repository.NewInstanceDesiredStateRepository(s), repository.NewInstanceRuntimeStatusRepository(s), repository.NewInstanceCommandRepository(s))
	for n, typ := range []string{"openclaw", "hermes", "opencode", "deepseek-harness"} {
		t.Run(typ, func(t *testing.T) {
			id := n + 1
			token := "bootstrap-" + typ
			sessionToken := "session-" + typ
			expiry := time.Now().Add(time.Hour)
			if _, err := s.SQL().Exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,agent_bootstrap_token,updated_at) VALUES(?,7,?,'gateway','lite','running',2,?,UTC_TIMESTAMP())`, id, typ, token); err != nil {
				t.Fatal(err)
			}
			if n%2 == 1 {
				if _, err := s.SQL().Exec("UPDATE instances SET runtime_type='' WHERE id=?", id); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := svc.Register(token, AgentRegisterRequest{InstanceID: id, AgentID: typ, AgentVersion: "1", ProtocolVersion: "v1"}, "127.0.0.1"); err == nil {
				t.Error("legacy bootstrap registered native Lite")
			}
			if _, err := s.SQL().Exec(`INSERT INTO instance_agents(instance_id,agent_id,agent_version,protocol_version,status,capabilities_json,session_token,session_expires_at) VALUES(?,?,'1','v1','online','[]',?,?)`, id, typ, sessionToken, expiry); err != nil {
				t.Fatal(err)
			}
			agent, err := agents.GetByInstanceID(id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = svc.AuthenticateSession(sessionToken); err == nil {
				t.Error("legacy session authenticated native Lite")
			}
			instance, err := instances.GetByID(id)
			if err != nil {
				t.Fatal(err)
			}
			if err = agents.Update(agent); err == nil {
				t.Error("durable repository accepted native Lite legacy agent write")
			}
			if _, err = svc.Heartbeat(&AgentSession{Instance: instance, Agent: agent}, AgentHeartbeatRequest{AgentID: typ, OpenClawStatus: "running"}, "127.0.0.1"); err == nil {
				t.Error("legacy stale session renewed native Lite")
			}
		})
	}
}

func TestIdleManualStopPersistsIntentBeforeCleanupAndRetriesSameSource(t *testing.T) {
	s := idleLifecycleDB(t)
	ctx := t.Context()
	for _, q := range []string{`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,updated_at) VALUES(1,7,'openclaw','gateway','lite','running',2,UTC_TIMESTAMP())`, `INSERT INTO runtime_pods(id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,agent_endpoint,state,capacity,used_slots) VALUES(11,'openclaw','runtime','pool','source-uid','pool','test','https://10.0.0.2:19090','ready',1,1)`, `INSERT INTO instance_runtime_bindings(instance_id,runtime_pod_id,runtime_type,gateway_id,gateway_port,workspace_path,state,generation) VALUES(1,11,'openclaw','gw-1-2',20000,'/workspace','running',2)`} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	for _, q := range []string{`UPDATE instances SET agent_bootstrap_token='old-bootstrap' WHERE id=1`, `INSERT INTO instance_agents(instance_id,agent_id,agent_version,protocol_version,status,capabilities_json,session_token,session_expires_at) VALUES(1,'old','1','v1','online','[]','old-session',UTC_TIMESTAMP()+INTERVAL 1 DAY)`, `INSERT INTO instance_desired_state(instance_id,desired_power_state,desired_config_revision_id) VALUES(1,'running',123)`} {
		if _, err := s.SQL().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	store := repository.NewInstanceAutoStopRepository(s)
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	agent := &manualStopFailureAgent{fakeRuntimeAgentClient: &fakeRuntimeAgentClient{}, store: store, fail: true, t: t}
	svc := &instanceService{instanceRepo: repository.NewInstanceRepository(s), bindingRepo: repository.NewInstanceRuntimeBindingRepository(s), runtimePodRepo: repository.NewRuntimePodRepository(s), agentClient: agent, autoStopRepo: store}
	if err := svc.Stop(1); err == nil {
		t.Fatal("failed cleanup returned success")
	}
	a, err := store.GetActivity(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if a.StopOperationID == "" || a.StoppedReason != "manual" || a.StopPhase != "error" || a.Blocker == "" {
		t.Fatalf("missing durable manual stop failure: %+v", a)
	}
	if err = svc.instanceRepo.UpdateRuntimeState(ctx, 1, "running", 2, nil); err == nil {
		t.Fatal("late heartbeat revived manual stop")
	}
	var state string
	var revision int
	row, e := s.SQL().QueryRow(`SELECT desired_power_state,desired_config_revision_id FROM instance_desired_state WHERE instance_id=1`)
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&state, &revision); e != nil || state != "stopped" || revision != 123 {
		t.Fatalf("desired state/config lost: %s %d %v", state, revision, e)
	}
	var authorities int
	row, e = s.SQL().QueryRow(`SELECT (SELECT COUNT(*) FROM instances WHERE id=1 AND agent_bootstrap_token IS NOT NULL)+(SELECT COUNT(*) FROM instance_agents WHERE instance_id=1 AND session_token IS NOT NULL)`)
	if e != nil {
		t.Fatal(e)
	}
	if e = row.Scan(&authorities); e != nil || authorities != 0 {
		t.Fatalf("legacy authority not revoked: %d %v", authorities, e)
	}
	operation := a.StopOperationID
	agent.fail = false
	if err = svc.Stop(1); err != nil {
		t.Fatal(err)
	}
	a, _ = store.GetActivity(ctx, 1)
	if a.StopOperationID != "" || a.StopPhase != "completed" || a.StoppedReason != "manual" {
		t.Fatalf("manual retry incomplete: %+v", a)
	}
	pending, err := store.PendingStops(ctx)
	if err != nil || len(pending) != 0 {
		t.Fatalf("pending=%+v err=%v", pending, err)
	}
	var phase string
	row, _ = s.SQL().QueryRow("SELECT phase FROM instance_idle_stop_operations WHERE id=?", operation)
	if err = row.Scan(&phase); err != nil || phase != "completed" {
		t.Fatalf("original intent lost: %s %v", phase, err)
	}
}

type manualStopFailureAgent struct {
	*fakeRuntimeAgentClient
	store *repository.InstanceAutoStopRepository
	fail  bool
	t     *testing.T
}

func (a *manualStopFailureAgent) DeleteGateway(ctx context.Context, endpoint, id string) error {
	activity, err := a.store.GetActivity(ctx, 1)
	if err != nil {
		return err
	}
	if activity == nil || activity.StopOperationID == "" || activity.StoppedReason != "manual" {
		a.t.Error("cleanup preceded durable manual intent")
	}
	if a.fail {
		return errors.New("cleanup receipt unavailable")
	}
	return nil
}

func TestIdleLegacyAgentProControlPlaneUnchanged(t *testing.T) {
	s := idleLifecycleDB(t)
	if _, err := s.SQL().Exec(`INSERT INTO instances(id,user_id,type,runtime_type,instance_mode,status,runtime_generation,agent_bootstrap_token,updated_at) VALUES(8,7,'openclaw','desktop','pro','running',0,'pro-bootstrap',UTC_TIMESTAMP())`); err != nil {
		t.Fatal(err)
	}
	svc := NewInstanceAgentService(repository.NewInstanceRepository(s), repository.NewInstanceAgentRepository(s), repository.NewInstanceDesiredStateRepository(s), repository.NewInstanceRuntimeStatusRepository(s), repository.NewInstanceCommandRepository(s))
	reg, err := svc.Register("pro-bootstrap", AgentRegisterRequest{InstanceID: 8, AgentID: "pro", AgentVersion: "1", ProtocolVersion: "v1"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	session, err := svc.AuthenticateSession(reg.SessionToken)
	if err != nil {
		t.Fatal(err)
	}
	heartbeat, err := svc.Heartbeat(session, AgentHeartbeatRequest{AgentID: "pro", OpenClawStatus: "running"}, "127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	if heartbeat.DesiredPowerState != "running" {
		t.Fatalf("Pro power state=%s", heartbeat.DesiredPowerState)
	}
}
