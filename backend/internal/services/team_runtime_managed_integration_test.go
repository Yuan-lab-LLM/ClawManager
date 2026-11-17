package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"clawreef/internal/models"
	"clawreef/internal/runtimeauth"
)

// Exercise the real Runtime journal through CM's serving relay, including the
// actual SQL-backed source proof. Neither Redis nor the journal is mocked.
func TestRuntimeJournalThroughManagedBridgeAndSourceProof(t *testing.T) {
	source := os.Getenv("RUNTIME_DURABLE_SOURCE")
	if source == "" {
		t.Skip("requires isolated Runtime source and disposable SQL/Redis")
	}
	_, repo, team, task, baseline := projectionMySQL(t)
	bus, _ := capacityRedisFixture(t)
	cleanupMaintenanceTeam(t, bus, team.ID)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	service := &teamService{repo: repo}
	if !service.projectConsumedTeamEvent(ctx, team, nil, baseline) {
		t.Fatal("SQL projection baseline failed")
	}
	instanceID := 7
	identity := runtimeauth.Identity{UserID: team.UserID, InstanceID: instanceID, Generation: 1, PodUID: "fixture-only"}
	environment := fmt.Sprintf(`{"CLAWMANAGER_TEAM_ID":"%d"}`, team.ID)
	manager := &ManagedTeamRedis{
		instances: managedInstanceFixture{&models.Instance{ID: instanceID, UserID: team.UserID, EnvironmentOverridesJSON: &environment}},
		teams:     &managedTeamFixture{team, &models.TeamMember{TeamID: team.ID, UserID: team.UserID, InstanceID: &instanceID, Status: models.TeamMemberStatusIdle}},
		current:   func(context.Context, int) (runtimeauth.Identity, error) { return identity, nil },
		resolve:   func(context.Context, *models.Team) (*redisBus, error) { return bus, nil },
		networkProtection: func(_ context.Context, ip netip.Addr) error {
			if ip != netip.MustParseAddr("127.0.0.1") {
				return errManagedRedis
			}
			return nil
		},
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				session, err := manager.Open(ctx, identity)
				if err != nil {
					return
				}
				defer session.Close()
				_ = session.Relay(ctx, connection, nil)
			}()
		}
	}()
	rootID := fmt.Sprintf("team-%d-task-%d", team.ID, task.ID)
	directory := t.TempDir()
	script := `import assert from 'node:assert/strict';
import fs from 'node:fs/promises';
import path from 'node:path';
import {pathToFileURL} from 'node:url';
process.env.XDG_STATE_HOME=process.env.JOURNAL_ROOT;
const source=(await fs.readFile(process.env.RUNTIME_DURABLE_SOURCE,'utf8')).replace('import { definePluginEntry } from "openclaw/plugin-sdk/plugin-entry";','const definePluginEntry=x=>x;');
const moduleFile=path.join(process.env.JOURNAL_ROOT,'actual-runtime.mjs');await fs.writeFile(moduleFile,source+'\nexport {terminalJournal,RedisClient};');
const p=await import(pathToFileURL(moduleFile).href);
const cfg={teamId:process.env.TEAM_ID,memberId:'leader',sharedDir:path.join(process.env.JOURNAL_ROOT,'shared')};
const redis=new p.RedisClient(process.env.BRIDGE_URL);await redis.connect();
const envelope={messageId:'managed-proof',taskId:process.env.ROOT_ID,rootTaskId:process.env.ROOT_ID,assignmentId:'managed-assignment',revision:1,redisId:'1-0'};
const journal=p.terminalJournal(cfg);
try {
 if(process.argv[2]==='prepare'){
  await journal.admit(envelope);assert.equal(await journal.started(envelope),true);
  await journal.stage(envelope,{event:'completion_proposed',eventId:'managed-event',completionId:'managed-completion',attemptId:'managed-attempt',assignmentId:envelope.assignmentId,revision:1,status:'succeeded',resultMarkdown:'complete durable result'});
  await journal.accepted(envelope);await journal.flush(redis,envelope,true);await journal.flush(redis,envelope,true);
  const retained=await journal.read(envelope);assert.ok(!retained.compact,'missing CM ACK retains full result');assert.equal(retained.publications[0].event.resultMarkdown,'complete durable result');
  await assert.rejects(redis.command('SET','claw:team:'+cfg.teamId+':__cm_capacity:replay-source-closed','forged'));
 }else{
  const proof=JSON.parse(await redis.command('GET','claw:team:'+cfg.teamId+':__cm_capacity:replay-source-closed'));
  assert.equal(proof.schema,1);assert.match(proof.proof_sha256,/^[a-f0-9]{64}$/);
  await journal.flush(redis,envelope,true);assert.equal(await journal.read(envelope),null,'CM source proof plus exact accepted ACK permits reclamation');
 }
 console.log(JSON.stringify({phase:process.argv[2],realManagedRelay:true,realRuntimeJournal:true,exactSQLSourceProof:true}));
}finally{redis.close();}
`
	scriptFile := filepath.Join(directory, "managed-journal.mjs")
	if err = os.WriteFile(scriptFile, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	run := func(phase string) {
		t.Helper()
		command := exec.CommandContext(ctx, "node", scriptFile, phase)
		command.Env = append(os.Environ(), "JOURNAL_ROOT="+directory, "BRIDGE_URL=redis://"+listener.Addr().String()+"/"+strconv.Itoa(bus.db), "TEAM_ID="+strconv.Itoa(team.ID), "ROOT_ID="+rootID)
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("Runtime phase %s: %v\n%s", phase, err, output)
		}
		t.Logf("Runtime %s: %s", phase, output)
	}
	run("prepare")
	if length, err := bus.doRaw(ctx, "XLEN", teamEventsKey(team.ID)); err != nil || length != int64(1) {
		t.Fatal("actual allowlisted Lua duplicated a retried publication", length, err)
	}
	payload := map[string]interface{}{"completionId": "managed-completion", "attemptId": "managed-attempt", "assignmentId": "managed-assignment", "revision": 1}
	member := &models.TeamMember{MemberKey: "leader"}
	if err = service.emitCompletionAcknowledgement(team, bus, task, member, payload, teamCompletionDecisionAccepted, "integration_contract"); err != nil {
		t.Fatal(err)
	}
	if err = service.reclaimTeamDeliveryReceipts(ctx, team, bus); err != nil {
		t.Fatal(err)
	}
	raw, ok, err := bus.Get(ctx, teamCapacityPrefix(team.ID)+"replay-source-closed")
	var proof map[string]interface{}
	if err != nil || !ok || json.Unmarshal([]byte(raw), &proof) != nil {
		t.Fatal("actual proof missing", err)
	}
	run("recover")
}
