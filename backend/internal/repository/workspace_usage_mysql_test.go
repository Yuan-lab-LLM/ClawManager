package repository

import (
	"clawreef/internal/models"
	"context"
	driver "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4/adapter/mysql"
	"os"
	"strings"
	"testing"
	"time"
)

func TestWorkspaceUsageMySQLPublication(t *testing.T) {
	dsn := os.Getenv("WORKSPACE_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable empty MySQL")
	}
	cfg, err := driver.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_") {
		t.Fatal("refusing non-test database")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName, Options: map[string]string{"parseTime": "true"}})
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var count int
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	if err = row.Scan(&count); err != nil || count != 0 {
		t.Fatal("requires empty database", count, err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`CREATE TABLE instances(id INT PRIMARY KEY,user_id INT,owner VARCHAR(128),type VARCHAR(32),runtime_type VARCHAR(32),instance_mode VARCHAR(16),status VARCHAR(32),workspace_path TEXT,runtime_generation INT,workspace_usage_bytes BIGINT DEFAULT 0,workspace_usage_observed_at DATETIME(6) NULL,workspace_usage_epoch BIGINT DEFAULT 0,workspace_usage_blocked BOOLEAN DEFAULT FALSE,updated_at TIMESTAMP(6) DEFAULT CURRENT_TIMESTAMP(6) ON UPDATE CURRENT_TIMESTAMP(6))`)
	defer sess.SQL().Exec("DROP TABLE instances")
	exec(`INSERT INTO instances(id,user_id,owner,type,runtime_type,instance_mode,status,workspace_path,runtime_generation,updated_at) VALUES(7,3,NULL,'openclaw','gateway','lite','stopped','/workspaces/openclaw/user-3/instance-7',2,'2026-01-01')`)
	r := NewWorkspaceUsageRepository(sess)
	candidates, err := r.Candidates(t.Context(), 0, 100)
	if err != nil || len(candidates) != 1 {
		t.Fatal(candidates, err)
	}
	candidate := candidates[0]
	if candidate.WorkspaceUsageObservedAt != nil {
		t.Fatal("unobserved zero is measured")
	}
	observed := time.Now().UTC().Truncate(time.Microsecond)
	ok, err := r.Publish(t.Context(), candidate, 7, observed)
	if err != nil || !ok {
		t.Fatal(ok, err)
	}
	assert := func(want int64) {
		t.Helper()
		var n int64
		var at, updated time.Time
		row, err := sess.SQL().QueryRow("SELECT workspace_usage_bytes,workspace_usage_observed_at,updated_at FROM instances WHERE id=7")
		if err != nil {
			t.Fatal(err)
		}
		if err = row.Scan(&n, &at, &updated); err != nil {
			t.Fatal(err)
		}
		if n != want || !at.Equal(observed) || updated.Year() != 2026 || updated.Month() != 1 {
			t.Fatal("observation changed business timestamp or lost value", n, at, updated)
		}
	}
	assert(7)
	for _, change := range []string{"workspace_path='/different'", "workspace_path='/WORKSPACES/openclaw/user-3/instance-7'", "runtime_generation=3", "user_id=4", "owner='another'", "workspace_usage_epoch=1", "workspace_usage_blocked=TRUE"} {
		t.Run(change, func(t *testing.T) {
			exec("UPDATE instances SET " + change + ",updated_at=updated_at WHERE id=7")
			ok, err := r.Publish(context.Background(), candidate, 0, observed.Add(time.Second))
			if err != nil || ok {
				t.Fatal("stale scan accepted", ok, err)
			}
			assert(7)
			exec("UPDATE instances SET workspace_path=?,runtime_generation=2,user_id=3,owner=NULL,workspace_usage_epoch=0,workspace_usage_blocked=FALSE,updated_at=updated_at WHERE id=7", *candidate.WorkspacePath)
		})
	}
	ok, err = r.Publish(t.Context(), candidate, 0, observed)
	if err != nil || !ok {
		t.Fatal("real empty observation rejected", ok, err)
	}
	assert(0)
	if ok, err := r.Publish(t.Context(), candidate, 999, observed.Add(-time.Second)); err != nil || ok {
		t.Fatal("older replica overwrote later observation", ok, err)
	}
	assert(0)
	_, err = r.Publish(t.Context(), models.Instance{}, -1, observed)
	if err == nil {
		t.Fatal("invalid publication accepted")
	}
}

func TestWorkspaceUsageResetAndStaleUpdateMySQL(t *testing.T) {
	r := runtimeSecurityMySQL(t)
	if _, err := r.sess.SQL().Exec("CREATE TABLE instance_runtime_recovery(instance_id INT)"); err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := r.sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("UPDATE instances SET type='openclaw',runtime_type='gateway',instance_mode='lite',workspace_path='/workspaces/openclaw/user-3/instance-7',workspace_usage_bytes=17,workspace_usage_observed_at=UTC_TIMESTAMP(6),status='creating' WHERE id=7")
	stale, err := r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	exec("UPDATE instances SET workspace_usage_bytes=99,workspace_usage_observed_at=UTC_TIMESTAMP(6) WHERE id=7")
	stale.Name = "ordinary rename"
	if err = r.Update(stale); err != nil {
		t.Fatal(err)
	}
	current, err := r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	if current.WorkspaceUsageBytes != 99 || current.WorkspaceUsageObservedAt == nil {
		t.Fatal("stale model erased independent observation", current)
	}
	if err = r.UpdateRuntimeState(t.Context(), 7, "stopped", 2, nil); err != nil {
		t.Fatal(err)
	}
	current, err = r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	if current.WorkspaceUsageBlocked || current.WorkspaceUsageObservedAt == nil {
		t.Fatal("ordinary stop/sleep blocked measurement")
	}
	if err = r.SetWorkspacePath(t.Context(), 7, *current.WorkspacePath); err != nil {
		t.Fatal(err)
	}
	current, err = r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	if current.WorkspaceUsageObservedAt == nil {
		t.Fatal("unchanged path erased measurement proof")
	}
	usage := NewWorkspaceUsageRepository(r.sess)
	beforeReset := *current
	claimed, err := r.ClaimLifecycleStatus(t.Context(), 7, []string{"stopped"}, "stopped")
	if err != nil || !claimed {
		t.Fatal(claimed, err)
	}
	candidates, err := usage.Candidates(t.Context(), 0, 100)
	if err != nil || len(candidates) != 0 {
		t.Fatal("reset erasure window is sampleable", candidates, err)
	}
	if ok, err := usage.Publish(t.Context(), beforeReset, 0, time.Now()); err != nil || ok {
		t.Fatal("pre-reset scan published", ok, err)
	}
	for _, table := range []string{"skill_package_materialize_jobs", "instance_skills", "instance_commands", "instance_runtime_status", "instance_agents", "instance_config_revisions", "instance_external_access", "instance_gateway_token_aliases"} {
		exec("CREATE TABLE " + table + "(instance_id INT)")
	}
	// instance_desired_state is created by applyIdleStopFixtureSchema only in
	// some native fixtures; this reset check needs only its instance_id column.
	exec("CREATE TABLE IF NOT EXISTS instance_desired_state(instance_id INT)")
	if err = r.ResetInstanceRuntimeData(t.Context(), 7); err != nil {
		t.Fatal(err)
	}
	candidates, err = usage.Candidates(t.Context(), 0, 100)
	if err != nil || len(candidates) != 1 {
		t.Fatal("successful reset never unblocked", candidates, err)
	}
	if candidates[0].WorkspaceUsageObservedAt != nil {
		t.Fatal("reset zero masquerades as observation")
	}
	if ok, err := usage.Publish(t.Context(), beforeReset, 0, time.Now()); err != nil || ok {
		t.Fatal("epoch failed after reset", ok, err)
	}
	if ok, err := usage.Publish(t.Context(), candidates[0], 0, time.Now()); err != nil || !ok {
		t.Fatal("fresh empty scan rejected", ok, err)
	}
	// A path/owner edit invalidates prior observation and fences old walkers.
	current, err = r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	next := "/workspaces/openclaw/user-3/instance-7-new"
	current.WorkspacePath = &next
	if err = r.Update(current); err != nil {
		t.Fatal(err)
	}
	current, err = r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	if current.WorkspaceUsageObservedAt != nil {
		t.Fatal("new path retained old proof")
	}
	// Replacement reset fences both identities before old workspace cleanup.
	replacement := *current
	replacement.ID = 8
	replacement.Status = "running"
	replacementPath := "/workspaces/openclaw/user-3/instance-8"
	replacement.WorkspacePath = &replacementPath
	measured := time.Now().UTC()
	replacement.WorkspaceUsageObservedAt = &measured
	replacement.WorkspaceUsageEpoch = 0
	if err = r.Create(&replacement); err != nil {
		t.Fatal(err)
	}
	replacementCandidate, err := r.GetByID(8)
	if err != nil {
		t.Fatal(err)
	}
	replacement = *replacementCandidate
	if err = r.PromoteResetReplacement(t.Context(), 7, 8, "owner", "replacement", "quarantine", "old", "reset"); err != nil {
		t.Fatal(err)
	}
	source, err := r.GetByID(7)
	if err != nil {
		t.Fatal(err)
	}
	if !source.WorkspaceUsageBlocked || source.WorkspaceUsageObservedAt != nil {
		t.Fatal("quarantined source still sampleable")
	}
	promoted, err := r.GetByID(8)
	if err != nil {
		t.Fatal(err)
	}
	if promoted.WorkspaceUsageObservedAt != nil || promoted.WorkspaceUsageEpoch == replacement.WorkspaceUsageEpoch {
		t.Fatal("replacement handoff retained stale proof")
	}
	for _, stale := range []models.Instance{*current, replacement} {
		if ok, err := usage.Publish(t.Context(), stale, 0, time.Now()); err != nil || ok {
			t.Fatal("handoff accepted old scan", ok, err)
		}
	}

}
