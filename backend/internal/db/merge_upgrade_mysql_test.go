package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"clawreef/internal/config"
)

// Opt-in only: uses new, uniquely named databases on a disposable local MySQL.
// Never point this at a production server. The test does not drop databases.
func TestMergedMigrationsMySQL(t *testing.T) {
	port, err := strconv.Atoi(os.Getenv("CLAWMANAGER_MERGE_TEST_MYSQL_PORT"))
	if err != nil || port <= 0 {
		t.Skip("requires disposable local MySQL")
	}
	password := os.Getenv("CLAWMANAGER_MERGE_TEST_MYSQL_PASSWORD")
	server, err := sql.Open("mysql", fmt.Sprintf("root:%s@tcp(127.0.0.1:%d)/?parseTime=true", password, port))
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	for _, scenario := range []string{"fresh", "upgrade"} {
		t.Run(scenario, func(t *testing.T) {
			baseline := os.Getenv("CLAWMANAGER_MERGE_TEST_BASELINE_MIGRATIONS")
			if scenario == "upgrade" && baseline == "" {
				t.Skip("baseline checkout not supplied")
			}
			name := fmt.Sprintf("cm_merge_%s_%d", scenario, time.Now().UnixNano())
			if _, err := server.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
				t.Fatal(err)
			}
			session, err := connect(config.DatabaseConfig{Host: "127.0.0.1", Port: port, User: "root", Password: password, Database: name})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			var modelID int64
			baselineHasModelPolicy := false
			if scenario == "upgrade" {
				driver := session.Driver().(*sql.DB)
				conn, err := driver.Conn(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if err := ensureSchemaMigrationsTable(conn); err != nil {
					t.Fatal(err)
				}
				entries, err := os.ReadDir(baseline)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
						continue
					}
					raw, err := os.ReadFile(filepath.Join(baseline, entry.Name()))
					if err != nil {
						t.Fatal(err)
					}
					for i, statement := range splitSQLStatements(string(raw)) {
						if _, err := conn.ExecContext(context.Background(), statement); err != nil {
							t.Fatalf("baseline %s statement %d: %v", entry.Name(), i+1, err)
						}
					}
					if _, err := conn.ExecContext(context.Background(), "INSERT INTO schema_migrations(filename,applied_at) VALUES (?,?)", entry.Name(), time.Now().UTC()); err != nil {
						t.Fatal(err)
					}
				}
				var policyColumns int
				if err := conn.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='llm_models' AND COLUMN_NAME='visibility_scope'").Scan(&policyColumns); err != nil {
					t.Fatal(err)
				}
				baselineHasModelPolicy = policyColumns == 1
				seedModel := "INSERT INTO llm_models(display_name,provider_type,base_url,provider_model_name) VALUES ('merge-test','local','https://example.invalid','test-model')"
				if baselineHasModelPolicy {
					seedModel = "INSERT INTO llm_models(display_name,provider_type,base_url,provider_model_name,model_group,visibility_scope,access_policy_version) VALUES ('merge-test','local','https://example.invalid','test-model','test-group','selected_users',3)"
				}
				result, err := conn.ExecContext(context.Background(), seedModel)
				if err != nil {
					t.Fatal(err)
				}
				modelID, err = result.LastInsertId()
				if err != nil {
					t.Fatal(err)
				}
				if baselineHasModelPolicy {
					if _, err := conn.ExecContext(context.Background(), "INSERT INTO llm_model_user_grants(model_id,user_id) SELECT ?,id FROM users ORDER BY id LIMIT 1", modelID); err != nil {
						t.Fatal(err)
					}
				}
				conn.Close()
			}
			for attempt := 0; attempt < 2; attempt++ {
				if err := applyEmbeddedMigrations(session); err != nil {
					t.Fatalf("%s startup %d: %v", scenario, attempt+1, err)
				}
			}
			if scenario == "upgrade" {
				var scope string
				var version, grants int
				row, err := session.SQL().QueryRow("SELECT visibility_scope,access_policy_version,(SELECT COUNT(*) FROM llm_model_user_grants WHERE model_id=?) FROM llm_models WHERE id=?", modelID, modelID)
				if err != nil {
					t.Fatal(err)
				}
				if err := row.Scan(&scope, &version, &grants); err != nil {
					t.Fatal(err)
				}
				wantScope, wantVersion, wantGrants := "all", 1, 0
				if baselineHasModelPolicy {
					wantScope, wantVersion, wantGrants = "selected_users", 3, 1
				}
				if scope != wantScope || version != wantVersion || grants != wantGrants {
					t.Fatalf("authorization changed: scope=%s version=%d grants=%d", scope, version, grants)
				}
			}
		})
	}
}
