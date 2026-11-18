package repository

import (
	"os"
	"slices"
	"strings"
	"testing"

	"clawreef/internal/models"
	drivermysql "github.com/go-sql-driver/mysql"
	"github.com/upper/db/v4"
	"github.com/upper/db/v4/adapter/mysql"
)

func TestRuntimeModelIdentityMySQL(t *testing.T) {
	dsn := os.Getenv("CLAWMANAGER_IDENTITY_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("requires disposable identity test database")
	}
	cfg, err := drivermysql.ParseDSN(dsn)
	if err != nil || !strings.HasPrefix(cfg.DBName, "clawmanager_test_identity_") {
		t.Fatal("refusing non-test database")
	}
	sess, err := mysql.Open(mysql.ConnectionURL{Host: cfg.Addr, User: cfg.User, Password: cfg.Passwd, Database: cfg.DBName})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	row, err := sess.SQL().QueryRow("SELECT COUNT(*) FROM information_schema.tables WHERE table_schema=?", cfg.DBName)
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := row.Scan(&count); err != nil || count != 0 {
		t.Fatal("requires empty database")
	}
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := sess.SQL().Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("CREATE TABLE llm_models (id INT PRIMARY KEY,display_name VARCHAR(255) NOT NULL) ENGINE=InnoDB")
	t.Cleanup(func() {
		_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS llm_runtime_provider_refs")
		_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS llm_runtime_model_aliases")
		_, _ = sess.SQL().Exec("DROP TABLE IF EXISTS llm_models")
	})
	exec("INSERT INTO llm_models VALUES (1,'Original Name'),(2,'Other URL')")
	wireNames := []string{"Org/Model:V1", "中文 模型", "Vendor/Model (public)", "model\\name", "/leading", "trailing/", "double//slash"}
	for index, name := range wireNames {
		exec("INSERT INTO llm_models (id,display_name) VALUES (?,?)", index+10, name)
	}
	schema, err := os.ReadFile("../db/migrations/077_add_runtime_model_identity.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(schema), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	refsSchema, err := os.ReadFile("../db/migrations/078_add_runtime_provider_refs.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(refsSchema), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	openclawSchema, err := os.ReadFile("../db/migrations/079_add_openclaw_runtime_provider_refs.sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, statement := range strings.Split(string(openclawSchema), ";") {
		if strings.TrimSpace(statement) != "" {
			exec(statement)
		}
	}
	row, err = sess.SQL().QueryRow("SELECT runtime_model_id FROM llm_models WHERE id=1")
	if err != nil {
		t.Fatal(err)
	}
	var stable string
	if err := row.Scan(&stable); err != nil || !strings.HasPrefix(stable, "cm-deploy-v1-") {
		t.Fatal("migration identity missing", err)
	}
	m := &models.LLMModel{ID: 1, DisplayName: "Renamed", RuntimeModelID: stable}
	if err := sess.Tx(func(tx db.Session) error {
		if _, err := tx.SQL().Exec("UPDATE llm_models SET display_name=? WHERE id=1", m.DisplayName); err != nil {
			return err
		}
		if err := reserveRuntimeModelAliases(tx, m); err != nil {
			return err
		}
		return reserveRuntimeProviderRef(tx, m)
	}); err != nil {
		t.Fatal(err)
	}
	for _, alias := range []string{"original name", "renamed", stable, models.RuntimeModelAlias("Original Name")} {
		row, err := sess.SQL().QueryRow("SELECT model_id FROM llm_runtime_model_aliases WHERE alias=?", alias)
		if err != nil {
			t.Fatal(err)
		}
		var id int
		if err := row.Scan(&id); err != nil || id != 1 {
			t.Fatal("lost alias", alias, err)
		}
	}
	reader := &llmModelRepository{sess: sess}
	for index, name := range wireNames {
		ref := models.RuntimeOpenClawProviderRef(models.LLMModel{DisplayName: name})
		id, found, err := reader.resolveRuntimeModelID(ref.ProviderID + "/" + ref.ModelID)
		if err != nil || !found || id != index+10 {
			t.Fatalf("SQL/Go OpenClaw wire mismatch for %q: %d %v %v", name, id, found, err)
		}
	}
	oldOpenClawRef := models.RuntimeOpenClawProviderRef(models.LLMModel{DisplayName: "Original Name"})
	fullOpenClawRef := oldOpenClawRef.ProviderID + "/" + oldOpenClawRef.ModelID
	if id, found, err := reader.resolveRuntimeModelID(fullOpenClawRef); err != nil || !found || id != 1 {
		t.Fatal("OpenClaw migration lost historical identity after rename", id, found, err)
	}
	if _, found, err := reader.resolveRuntimeModelID("wrong-provider/" + oldOpenClawRef.ModelID); err != nil || found {
		t.Fatal("OpenClaw ref must not use suffix guessing", found, err)
	}
	renamed := *m
	renamed.DisplayName = "Renamed Again (private)"
	if err := sess.Tx(func(tx db.Session) error { return reserveRuntimeProviderRef(tx, &renamed) }); err != nil {
		t.Fatal(err)
	}
	newOpenClawRef := models.RuntimeOpenClawProviderRef(renamed)
	if id, found, err := reader.resolveRuntimeModelID(newOpenClawRef.ProviderID + "/" + newOpenClawRef.ModelID); err != nil || !found || id != 1 {
		t.Fatal("new OpenClaw reference was not reserved", id, found, err)
	}
	for _, ref := range []string{"original name/original name", "renamed/renamed", " ORIGINAL NAME/ORIGINAL NAME ", stable} {
		id, found, err := reader.resolveRuntimeModelID(ref)
		if err != nil || !found || id != 1 {
			t.Fatalf("lost persisted ref %q: %d %v %v", ref, id, found, err)
		}
	}
	for _, ref := range []string{"wrong-provider/renamed", "original name/renamed", "renamed/original name"} {
		if _, found, err := reader.resolveRuntimeModelID(ref); err != nil || found {
			t.Fatalf("provider/model suffix fallback: %q %v", ref, err)
		}
	}
	exec("INSERT INTO llm_runtime_provider_refs VALUES ('vendor', 'org/model', 1)")
	if id, found, err := reader.resolveRuntimeModelID("vendor/org/model"); err != nil || !found || id != 1 {
		t.Fatal("namespace model truncated", id, found, err)
	}
	// A model literally named provider/model must not steal another deployment's
	// historical full ref (or vice versa), even if only one is visible to a user.
	exec("INSERT INTO llm_runtime_model_aliases (alias, model_id) VALUES ('vendor/org/model', 2)")
	if _, _, err := reader.resolveRuntimeModelID("vendor/org/model"); err != models.ErrModelUnavailable {
		t.Fatal("ambiguous reference accepted", err)
	}
	aliases, err := reader.RuntimeModelAliases([]int{1})
	if err != nil || len(aliases) != 1 || !slices.Contains(aliases[1], "original name") || !slices.Contains(aliases[1], "renamed") {
		t.Fatalf("scoped historical alias catalog: %v %v", aliases, err)
	}
	if empty, err := reader.RuntimeModelAliases(nil); err != nil || len(empty) != 0 {
		t.Fatalf("empty IDs must not expose all aliases: %v %v", empty, err)
	}
	exec("DELETE FROM llm_models WHERE id=1")
	refs, err := reader.RuntimeProviderRefs([]int{1})
	if err != nil || len(refs[1]) != 6 || !slices.Contains(refs[1], oldOpenClawRef) || !slices.Contains(refs[1], newOpenClawRef) {
		t.Fatalf("lost exact provider refs: %v %v", refs, err)
	}
	if empty, err := reader.RuntimeProviderRefs(nil); err != nil || len(empty) != 0 {
		t.Fatal("unscoped provider refs", empty, err)
	}
	// Deletion retains reservations; a new deployment cannot steal old sessions.
	other := &models.LLMModel{ID: 2, DisplayName: "Original Name", RuntimeModelID: "cm-deploy-v1-new"}
	if err := sess.Tx(func(tx db.Session) error { return reserveRuntimeProviderRef(tx, other) }); err != nil {
		t.Fatal("deleted display name must not block new deployment", err)
	}
	if err := sess.Tx(func(tx db.Session) error { return reserveRuntimeModelAliases(tx, other) }); err != nil {
		t.Fatal("same display name with fresh stable identity must save", err)
	}
	if id, found, err := reader.resolveRuntimeModelID("Original Name"); err != nil || !found || id != 1 {
		t.Fatal("historical name was reassigned", id, err)
	}
	if id, found, err := reader.resolveRuntimeModelID(other.RuntimeModelID); err != nil || !found || id != 2 {
		t.Fatal("new stable identity missing", id, err)
	}
	row, err = sess.SQL().QueryRow("SELECT COUNT(*) FROM llm_runtime_model_aliases WHERE alias='cm-deploy-v1-new'")
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&count); err != nil || count != 1 {
		t.Fatal("new stable alias was not saved", err)
	}
	// A conflict discovered only at the normalized OpenClaw pair must roll back
	// the raw pair inserted earlier in the same transaction.
	collision := &models.LLMModel{ID: 2, DisplayName: "Collision Name"}
	collisionRef := models.RuntimeOpenClawProviderRef(*collision)
	exec("INSERT INTO llm_runtime_provider_refs VALUES (?,?,?)", collisionRef.ProviderID, collisionRef.ModelID, 10)
	if err := sess.Tx(func(tx db.Session) error { return reserveRuntimeProviderRef(tx, collision) }); err == nil {
		t.Fatal("OpenClaw normalized reference conflict was accepted")
	}
	row, err = sess.SQL().QueryRow("SELECT COUNT(*) FROM llm_runtime_provider_refs WHERE provider_id='collision name' AND model_ref='collision name'")
	if err != nil {
		t.Fatal(err)
	}
	if err := row.Scan(&count); err != nil || count != 0 {
		t.Fatal("normalized reference conflict left a partial raw reservation", err)
	}
}
