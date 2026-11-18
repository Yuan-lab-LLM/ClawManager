package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNFSTeamWriteGateSeesCommitAfterTransactionSnapshot(t *testing.T) {
	for _, lateMembership := range []bool{false, true} {
		t.Run(fmt.Sprintf("late-membership=%t", lateMembership), func(t *testing.T) {
			r := workspaceFenceMySQL(t)
			ctx := context.Background()
			teams := NewTeamRepository(r.sess)
			ConfigureTeamNFSWriteFences(teams, "nfs-rootless-v1")
			team := &models.Team{UserID: 3, Name: "concurrent", Status: "running"}
			if err := teams.CreateTeam(team); err != nil {
				t.Fatal(err)
			}
			instanceID := 7
			member := &models.TeamMember{TeamID: team.ID, InstanceID: &instanceID, UserID: 3, MemberKey: "worker", Status: "idle"}
			if !lateMembership {
				if err := teams.CreateMember(member); err != nil {
					t.Fatal(err)
				}
			}
			writes := NewWorkspaceWriteRepository(r.sess)
			intent := models.WorkspaceWriteIntent{TeamID: team.ID, OwnerID: 3, Operation: strings.Repeat("d", 32), Action: "edit"}
			err := r.sess.TxContext(ctx, func(tx db.Session) error {
				// The activation/maintenance path has already read unrelated rows. Under
				// REPEATABLE READ this snapshot predates the separately committed writer.
				row, err := tx.SQL().QueryRowContext(ctx, "SELECT COUNT(*) FROM team_workspace_write_intents")
				if err != nil {
					return err
				}
				var before int
				if err := row.Scan(&before); err != nil {
					return err
				}
				if lateMembership {
					if err := teams.CreateMember(member); err != nil {
						return err
					}
				}
				if err := writes.Begin(ctx, intent); err != nil {
					return err
				}
				return requireNoInstanceTeamWorkspaceWrites(ctx, tx, instanceID)
			}, &sql.TxOptions{Isolation: sql.LevelRepeatableRead})
			if !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
				t.Fatalf("committed Team writer was hidden by older transaction snapshot: %v", err)
			}
			if err := writes.Complete(ctx, intent); err != nil {
				t.Fatal(err)
			}
			if err := requireNoInstanceTeamWorkspaceWrites(ctx, r.sess, instanceID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func teamGateFixtureTables(t *testing.T, sess db.Session) {
	t.Helper()
	for _, model := range []interface{}{models.Team{}, models.TeamMember{}} {
		typ := reflect.TypeOf(model)
		table := "teams"
		if typ.Name() == "TeamMember" {
			table = "team_members"
		}
		var columns []string
		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			name := strings.Split(field.Tag.Get("db"), ",")[0]
			if name == "" || name == "-" {
				continue
			}
			kind := field.Type
			nullable := kind.Kind() == reflect.Pointer
			if nullable {
				kind = kind.Elem()
			}
			sqlType := "VARCHAR(255)"
			switch kind.Kind() {
			case reflect.Int, reflect.Int64:
				sqlType = "INT"
			case reflect.Bool:
				sqlType = "BOOLEAN"
			case reflect.Struct:
				if kind != reflect.TypeOf(time.Time{}) {
					t.Fatal(field.Name)
				}
				sqlType = "DATETIME(6)"
			}
			if name == "id" {
				sqlType = "INT AUTO_INCREMENT PRIMARY KEY"
			} else if !nullable {
				switch kind.Kind() {
				case reflect.String:
					sqlType += " NOT NULL DEFAULT ''"
				case reflect.Struct:
					sqlType += " NOT NULL DEFAULT CURRENT_TIMESTAMP(6)"
				default:
					sqlType += " NOT NULL DEFAULT 0"
				}
			}
			columns = append(columns, "`"+name+"` "+sqlType)
		}
		if _, err := sess.SQL().Exec("CREATE TABLE IF NOT EXISTS " + table + " (" + strings.Join(columns, ",") + ") ENGINE=InnoDB"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestNFSTeamUnknownWriteBlocksReplacementAndMetadataButAllowsOrdinaryStop(t *testing.T) {
	r := workspaceFenceMySQL(t)
	ctx := context.Background()
	teamGateFixtureTables(t, r.sess)
	if _, err := r.sess.SQL().Exec("CREATE TABLE instance_commands(instance_id INT,status VARCHAR(32))"); err != nil {
		t.Fatal(err)
	}
	teams := NewTeamRepository(r.sess)
	ConfigureTeamNFSWriteFences(teams, "nfs-rootless-v1")
	team := &models.Team{ID: 11, UserID: 3, Name: "shared", Status: "running", SharedMountPath: "/team"}
	if err := teams.CreateTeam(team); err != nil {
		t.Fatal(err)
	}
	instanceID := 7
	member := &models.TeamMember{TeamID: team.ID, InstanceID: &instanceID, UserID: 3, MemberKey: "worker", Role: "worker", RuntimeType: "opencode", InstanceMode: "lite", Status: "idle"}
	if err := teams.CreateMember(member); err != nil {
		t.Fatal(err)
	}
	fence := NewWorkspaceWriteFenceRepository(r.sess)
	if err := fence.Activate(ctx, 7, 3, 2, "old-pod"); err != nil {
		t.Fatal(err)
	}
	writes := NewWorkspaceWriteRepository(r.sess)
	intent := models.WorkspaceWriteIntent{TeamID: team.ID, OwnerID: 3, Operation: strings.Repeat("e", 32), Action: "edit"}
	if err := writes.Begin(ctx, intent); err != nil {
		t.Fatal(err)
	}
	// Stopping A may proceed while an authorized shared Team writer exists. Its
	// unknown outcome must still block future generation/upgrade admission.
	if err := fence.Fence(ctx, 7, 3, 2, "old-pod"); err != nil {
		t.Fatal("ordinary stop blocked shared Team writer", err)
	}
	if _, err := r.sess.SQL().Exec("UPDATE instances SET runtime_generation=3,status='creating' WHERE id=7"); err != nil {
		t.Fatal(err)
	}
	if err := fence.Activate(ctx, 7, 3, 3, "new-pod"); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("unknown Team write admitted replacement: %v", err)
	}
	if err := requireNoPlatformWriters(ctx, r.sess, 7); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("unknown Team write admitted maintenance: %v", err)
	}
	member.Status = "busy"
	if err := teams.UpdateMember(member); err != nil {
		t.Fatal("ordinary task state blocked", err)
	}
	member.Status = "deleted"
	if err := teams.UpdateMember(member); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("unknown Team write allowed membership removal: %v", err)
	}
	team.SharedMountPath = "/changed"
	if err := teams.UpdateTeam(team); !errors.Is(err, models.ErrRuntimeUpgradeWritersActive) {
		t.Fatalf("unknown Team write allowed shared identity change: %v", err)
	}
	if err := writes.Complete(ctx, intent); err != nil {
		t.Fatal(err)
	}
	if err := fence.Activate(ctx, 7, 3, 3, "new-pod"); err != nil {
		t.Fatal(err)
	}
	if err := teams.UpdateMember(member); err != nil {
		t.Fatal(err)
	}
	t.Log(fmt.Sprintf("Team intent %s survived stopped members and gated replacement, maintenance and identity changes", intent.Operation))
}
