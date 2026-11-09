package repository_test

import (
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
	appdb "clawreef/internal/db"
	"clawreef/internal/models"
	"clawreef/internal/repository"
	"clawreef/internal/teamtemplate"
	"github.com/upper/db/v4/adapter/mysql"
)

// Explicitly opt in to a disposable localhost database. This never connects to
// a configured tenant database or changes production migration history.
func TestTeamIconMySQLMigrationPersistenceAndConcurrentEdit(t *testing.T) {
	portText := os.Getenv("TEST_TEAM_ICON_MYSQL_PORT")
	if portText == "" {
		t.Skip("isolated localhost MySQL required")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		t.Fatal("invalid isolated MySQL port")
	}
	root, err := mysql.Open(mysql.ConnectionURL{Host: "127.0.0.1:" + portText, User: "root", Database: "mysql"})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("team_icons_%d", time.Now().UnixNano())
	if _, err := root.SQL().Exec("CREATE DATABASE " + name); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = root.SQL().Exec("DROP DATABASE " + name); _ = root.Close() })
	cfg := config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Database: name, MaxOpenConns: 8, MaxIdleConns: 4}
	session, err := appdb.Initialize(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = session.Close() })
	repo := repository.NewTeamRepository(session)
	team := &models.Team{UserID: 1, Name: "legacy", Status: "running", CommunicationMode: "leader_mediated", SharedMountPath: "/team", RedisEventsLastID: "0-0"}
	if err := repo.CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	member := &models.TeamMember{TeamID: team.ID, UserID: 1, MemberKey: "analyst", DisplayName: "数据分析师", Role: "analyst", RuntimeType: "hermes", InstanceMode: "lite", Status: "busy", Availability: "busy", Progress: 31}
	if err := repo.CreateMember(member); err != nil {
		t.Fatal(err)
	}
	before, err := repo.GetMemberByID(member.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce the populated previous schema, then use the actual embedded
	// migration runner twice (upgrade + restart), not an ad hoc ALTER in the test.
	for _, sql := range []string{"ALTER TABLE team_members DROP COLUMN icon_key", "DELETE FROM schema_migrations WHERE filename='071_add_team_member_icon.sql'"} {
		if _, err := session.SQL().Exec(sql); err != nil {
			t.Fatal(err)
		}
	}
	_ = session.Close()
	session, err = appdb.Initialize(cfg)
	if err != nil {
		t.Fatal(err)
	}
	repo = repository.NewTeamRepository(session)
	after, err := repo.GetMemberByID(member.ID)
	if err != nil || !reflect.DeepEqual(before, after) || after.IconKey != nil {
		t.Fatalf("old member changed: %v before=%#v after=%#v", err, before, after)
	}
	icon := "data"
	after.IconKey = &icon
	if err := repo.UpdateMember(after); err != nil {
		t.Fatal(err)
	}
	// An interrupted startup may have applied the DDL without recording it.
	// Retrying must preserve saved values rather than failing with duplicate column.
	if _, err := session.SQL().Exec("DELETE FROM schema_migrations WHERE filename='071_add_team_member_icon.sql'"); err != nil {
		t.Fatal(err)
	}
	_ = session.Close()
	session, err = appdb.Initialize(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.NewTeamRepository(session).GetMemberByID(member.ID)
	if err != nil || loaded.IconKey == nil || *loaded.IconKey != icon {
		t.Fatalf("member icon lost at restart: %v %#v", err, loaded)
	}
	loaded.IconKey, after.IconKey = nil, nil
	if !reflect.DeepEqual(after, loaded) {
		t.Fatal("icon persistence changed role/runtime/status")
	}

	templates := repository.NewCustomTeamTemplateRepository(session)
	spec := teamtemplate.TemplateSpec{SchemaVersion: 1, Members: []teamtemplate.MemberSpec{{MemberID: "leader", Role: "leader", IsLeader: true}, {MemberID: "analyst", DisplayName: "数据分析师", Role: "analyst", Mission: "Analyze evidence"}}}
	raw, _ := json.Marshal(spec)
	template := &models.CustomTeamTemplate{UserID: 1, Name: "custom-icon", Intent: "Analyze", SpecJSON: string(raw), ResolvedMemberCount: 2, Revision: 1}
	if err := templates.Create(template); err != nil {
		t.Fatal(err)
	}
	stale, err := templates.GetByUserIDAndID(1, template.ID)
	if err != nil {
		t.Fatal(err)
	}
	service := teamtemplate.NewService(templates, nil) // changing an icon needs no model
	updated, err := service.UpdateMemberIcon(1, template.ID, "analyst", teamtemplate.UpdateMemberIconRequest{IconOverride: "editor", ExpectedRevision: 1})
	if err != nil || updated.Revision != 2 {
		t.Fatalf("real icon edit: %v", err)
	}
	stale.Revision++
	stale.Intent = "stale in-flight model result"
	if err := templates.Update(stale); err == nil || !strings.Contains(err.Error(), "revision conflict") {
		t.Fatalf("stale competing write accepted: %v", err)
	}
	refreshed, err := teamtemplate.NewService(templates, nil).Get(1, template.ID)
	if err != nil || refreshed.Spec.Members[1].IconKey != "editor" || refreshed.Intent != "Analyze" {
		t.Fatalf("concurrent edit lost: %v %#v", err, refreshed)
	}
	if _, err := service.UpdateMemberIcon(2, template.ID, "analyst", teamtemplate.UpdateMemberIconRequest{IconOverride: "qa", ExpectedRevision: 2}); err == nil {
		t.Fatal("different owner modified template")
	}
	if _, err := service.UpdateMetadata(1, template.ID, teamtemplate.UpdateMetadataRequest{Name: "renamed", ExpectedRevision: 2}); err != nil {
		t.Fatal(err)
	}
	cleared, err := service.UpdateMemberIcon(1, template.ID, "analyst", teamtemplate.UpdateMemberIconRequest{ExpectedRevision: 3})
	if err != nil || cleared.Spec.Members[1].IconKey != "data" {
		t.Fatalf("clear/rename/reload chain: %v %#v", err, cleared)
	}
}
