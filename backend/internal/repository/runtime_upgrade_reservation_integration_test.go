package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

func upgradeMySQL(t *testing.T) db.Session {
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
	root, err := mysql.Open(u)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("upgrade_reservation_%d", time.Now().UnixNano())
	if _, err := root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	u.Database = name
	sess, err := mysql.Open(u)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sess.Close(); root.SQL().Exec("DROP DATABASE " + name); root.Close() })
	exec := func(q string) {
		t.Helper()
		if _, err := sess.SQL().Exec(q); err != nil {
			t.Fatalf("schema: %v\n%s", err, q)
		}
	}
	exec("CREATE TABLE instances (id INT PRIMARY KEY, type VARCHAR(32), instance_mode VARCHAR(32), status VARCHAR(32), runtime_generation INT, runtime_error_message TEXT NULL, runtime_error_code VARCHAR(64) NULL, updated_at DATETIME(6))")
	for _, table := range []string{"users", "teams", "team_members"} {
		exec("CREATE TABLE " + table + " (id INT PRIMARY KEY)")
	}
	read := func(file string) string {
		t.Helper()
		b, e := os.ReadFile("../db/migrations/" + file)
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	base := read("023_add_runtime_pool_v2.sql")
	base = base[strings.Index(base, "CREATE TABLE IF NOT EXISTS runtime_pods"):strings.Index(base, "CREATE TABLE IF NOT EXISTS workspace_file_audits")]
	for _, schema := range []string{base, read("058_add_openclaw_runtime_upgrade_safety.sql"), read("061_persist_runtime_upgrade_source_identity.sql"), read("070_add_upgrade_restart_attempts.sql"), read("074_add_upgrade_recovery_key.sql")} {
		for _, query := range strings.Split(schema, ";") {
			if strings.TrimSpace(query) != "" {
				exec(query)
			}
		}
	}
	return sess
}

func TestUpgradeReservationMySQLContract(t *testing.T) {
	sess := upgradeMySQL(t)
	ctx := context.Background()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	digest := "sha256:" + strings.Repeat("a", 64)
	exec(`INSERT INTO runtime_rollouts (id,runtime_type,target_image_ref,target_image_digest,status,phase,preflight_id,source_images_json) VALUES (123,'openclaw',?,?,'running','gateway_restart','p123','{"tenant/source":"old"}')`, "repo@"+digest, digest)
	exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,pod_uid,deployment_name,image_ref,image_digest,agent_endpoint,state,capacity,used_slots) VALUES (307,'openclaw','tenant','target-pod','uid123','source-u123',?,?,'http://agent','ready',2,0)`, "repo@"+digest, digest)
	for _, id := range []int{481, 482, 484} {
		exec("INSERT INTO instances VALUES (?,'openclaw','lite','creating',2,NULL,NULL,NOW())", id)
		exec(`INSERT INTO runtime_upgrade_items (rollout_id,instance_id,state,source_generation,snapshot_ref,postflight_json,created_at,updated_at) VALUES (123,?,'restart_ready',1,'capsule','{}',NOW(),NOW()-INTERVAL 1 HOUR)`, id)
	}
	pods := NewRuntimePodRepository(sess)
	bindings := &instanceRuntimeBindingRepository{sess: sess}
	pod, err := pods.GetByID(ctx, 307)
	if err != nil {
		t.Fatal(err)
	}
	req := UpgradeGatewayReservation{RolloutID: 123, InstanceID: 481, Generation: 2, Pod: *pod, WorkspacePath: "/workspaces/481", PortStart: 19000, PortEnd: 19100, PortBlockSize: 3}
	if ok, err := pods.TryClaimSlot(ctx, 307); err != nil || ok {
		t.Fatalf("ordinary scheduling bypassed fence: %v %v", ok, err)
	}
	exec(`INSERT INTO runtime_pods (id,runtime_type,namespace,pod_name,deployment_name,image_ref,state,capacity) VALUES (308,'hermes','tenant','hermes','hermes','legacy','ready',100)`)
	if ok, err := pods.TryClaimSlot(ctx, 308); err != nil || !ok {
		t.Fatalf("OpenClaw upgrade blocked Hermes: %v %v", ok, err)
	}
	// Reject valid-capacity requests with the wrong authorization, before writes.
	for _, mutate := range []func(*UpgradeGatewayReservation){
		func(r *UpgradeGatewayReservation) { r.Generation = 3 },
		func(r *UpgradeGatewayReservation) { s := "different-uid"; r.Pod.PodUID = &s },
		func(r *UpgradeGatewayReservation) { r.Pod.Namespace = "another-tenant" },
	} {
		bad := req
		mutate(&bad)
		if _, err := bindings.ReserveUpgradeGateway(ctx, bad); !errors.Is(err, ErrUpgradeReservationDenied) {
			t.Fatalf("bad authorization accepted: %v", err)
		}
	}
	for _, tc := range []struct{ change, restore string }{
		{"UPDATE runtime_rollouts SET phase='session_migration' WHERE id=123", "UPDATE runtime_rollouts SET phase='gateway_restart' WHERE id=123"},
		{"UPDATE runtime_pods SET image_digest='wrong' WHERE id=307", "UPDATE runtime_pods SET image_digest='" + digest + "' WHERE id=307"},
		{"UPDATE instances SET instance_mode='pro' WHERE id=481", "UPDATE instances SET instance_mode='lite' WHERE id=481"},
		{"UPDATE runtime_upgrade_items SET state='migrated' WHERE instance_id=481", "UPDATE runtime_upgrade_items SET state='restart_ready' WHERE instance_id=481"},
		{"UPDATE runtime_upgrade_items SET source_generation=0 WHERE instance_id=481", "UPDATE runtime_upgrade_items SET source_generation=1 WHERE instance_id=481"},
		{"UPDATE runtime_upgrade_items SET recovery_status='needs_attention' WHERE instance_id=481", "UPDATE runtime_upgrade_items SET recovery_status=NULL WHERE instance_id=481"},
	} {
		exec(tc.change)
		if _, err := bindings.ReserveUpgradeGateway(ctx, req); err == nil {
			t.Fatalf("unsafe reservation accepted after %s", tc.change)
		}
		exec(tc.restore)
	}
	exec(`INSERT INTO runtime_rollouts (id,runtime_type,target_image_ref,status) VALUES (124,'openclaw','other','running')`)
	if _, err := bindings.ReserveUpgradeGateway(ctx, req); !errors.Is(err, ErrUpgradeReservationDenied) {
		t.Fatalf("competing rollout accepted: %v", err)
	}
	exec("UPDATE runtime_rollouts SET status='completed' WHERE id=124")
	// Real deployed old Agent omits UID. Only exact Kubernetes-observed
	// identity may fill this gap; never treat NULL as a wildcard.
	exec("UPDATE runtime_pods SET pod_uid=NULL WHERE id=307")
	if _, err := bindings.ReserveUpgradeGateway(ctx, req); !errors.Is(err, ErrUpgradeReservationDenied) {
		t.Fatalf("NULL UID accepted without Kubernetes: %v", err)
	}
	uid := "uid123"
	req.Pod.KubernetesPodUID = &uid
	var wg sync.WaitGroup
	errs := make(chan error, 12)
	for n := 0; n < 12; n++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := bindings.ReserveUpgradeGateway(ctx, req); errs <- e }()
	}
	wg.Wait()
	close(errs)
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	var item models.RuntimeUpgradeItem
	if err := sess.Collection("runtime_upgrade_items").Find(db.Cond{"rollout_id": 123, "instance_id": 481}).One(&item); err != nil {
		t.Fatal(err)
	}
	if item.RestartAttempts != 1 || item.RestartDeadlineAt == nil {
		t.Fatalf("attempt not atomic/idempotent: %+v", item)
	}
	if item.RestartTargetPodUID == nil || *item.RestartTargetPodUID != "uid123" {
		t.Fatal("observed UID not pinned to attempt")
	}
	wrong := "replacement"
	replacement := req
	replacement.Pod.KubernetesPodUID = &wrong
	if _, err := bindings.ReserveUpgradeGateway(ctx, replacement); !errors.Is(err, ErrUpgradeReservationDenied) {
		t.Fatalf("pending binding moved to replacement UID: %v", err)
	}
	b, err := bindings.GetByInstanceID(ctx, 481)
	if err != nil || b == nil {
		t.Fatalf("binding: %v", err)
	}
	// A zero-process heartbeat cannot erase existing durable reservations.
	if err := pods.UpdateHeartbeat(ctx, 307, "ready", 0, 2, false, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	req.InstanceID = 482
	req.WorkspacePath = "/workspaces/482"
	b2, err := bindings.ReserveUpgradeGateway(ctx, req)
	if err != nil {
		t.Fatal(err)
	}
	if b2.GatewayPort == b.GatewayPort {
		t.Fatal("duplicate reserved port")
	}
	req.InstanceID = 484
	req.WorkspacePath = "/workspaces/484"
	if _, err := bindings.ReserveUpgradeGateway(ctx, req); !errors.Is(err, ErrUpgradeCapacityPending) {
		t.Fatalf("capacity overbooked: %v", err)
	}
	// Simulate audit failure: capacity and binding must roll back together.
	exec("UPDATE runtime_pods SET capacity=3 WHERE id=307")
	exec("RENAME TABLE runtime_upgrade_audits TO hidden_audits")
	if _, err := bindings.ReserveUpgradeGateway(ctx, req); err == nil {
		t.Fatal("expected audit failure")
	}
	exec("RENAME TABLE hidden_audits TO runtime_upgrade_audits")
	if b, err := bindings.GetByInstanceID(ctx, 484); err != nil || b != nil {
		t.Fatalf("partial reservation committed: %+v %v", b, err)
	}
	p, err := pods.GetByID(ctx, 307)
	if err != nil || p.UsedSlots != 2 {
		t.Fatalf("capacity leaked: %+v %v", p, err)
	}
	if _, err := bindings.ReserveUpgradeGateway(ctx, req); err != nil {
		t.Fatal(err)
	}
	if ok, err := pods.TryClaimSlot(ctx, 307); err != nil || ok {
		t.Fatalf("ordinary fence removed: %v %v", ok, err)
	}
	// A terminal/foreign rollout cannot replay an existing reservation either.
	exec("UPDATE runtime_rollouts SET status='completed' WHERE id=123")
	if _, err := bindings.ReserveUpgradeGateway(ctx, req); !errors.Is(err, ErrUpgradeReservationDenied) {
		t.Fatalf("terminal upgrade authorized: %v", err)
	}
}
