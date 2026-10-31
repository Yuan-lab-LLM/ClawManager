package systembackup

import (
	"context"
	"strings"
	"testing"
)

func TestReadinessNeverInfersHealthFromEnabledConfiguration(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "installation-test"}
	if e := s.Bootstrap(ctx); e != nil {
		t.Fatal(e)
	}
	c, e := s.SaveConfig(ctx, 0, 1, testConfig(), permitConfig)
	if e != nil {
		t.Fatal(e)
	}
	health, e := s.Readiness(ctx)
	if e != nil || health.CanCreate || health.RecoveryPointCount != 0 {
		t.Fatal(health, e)
	}
	if _, _, e = s.Create(ctx, 1, "create-denied", CreateTaskRequest{Type: Backup, Purpose: "acceptance"}, AdmitReady); PublicError(e) != "not_ready" {
		t.Fatal(e)
	}
	for _, dep := range RequiredDependencies {
		if e = s.RecordHealth(ctx, c.Version, dep, "healthy", "none", strings.Repeat("a", 64), 60); e != nil {
			t.Fatal(e)
		}
	}
	health, e = s.Readiness(ctx)
	if e != nil || !health.CanCreate {
		t.Fatal(health, e)
	}
	if _, e = s.DB.Exec("UPDATE system_backup_dependency_health SET checked_at=TIMESTAMPADD(SECOND,-120,UTC_TIMESTAMP(6)),expires_at=TIMESTAMPADD(SECOND,-60,UTC_TIMESTAMP(6)) WHERE dependency='runtime_quiesce'"); e != nil {
		t.Fatal(e)
	}
	health, e = s.Readiness(ctx)
	if e != nil || health.CanCreate {
		t.Fatal("expired evidence accepted", health, e)
	}
}

func TestRoleBindingsCannotInventPrivileges(t *testing.T) {
	b, e := ParseRoleBindings("1:security_admin,2:security_admin,3:viewer,4:operator,5:config_admin")
	if e != nil {
		t.Fatal(e)
	}
	if b.Allows(3, Operate) || b.Allows(4, Configure) || b.Allows(5, Destructive) || b.Allows(0, Read) || !b.Allows(1, SecurityConfigure) {
		t.Fatal("role privilege drift")
	}
	for _, bad := range []string{"1:security_admin", "1:security_admin,1:security_admin", "1:superuser", "1:viewer,2:operator"} {
		if _, e = ParseRoleBindings(bad); e == nil {
			t.Fatal("invalid bootstrap accepted", bad)
		}
	}
}
