package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"reflect"
	"strings"

	"clawreef/internal/systembackup/team"
)

// Validate the same trusted schema used for alignment, without provisioning a
// target database or accepting any source-provided DDL.
func validateTeamDataSQLSnapshot(r io.Reader, b CaptureBudget) (SQLResult, error) {
	expected, e := BuiltinSchema()
	if e != nil {
		return SQLResult{}, e
	}
	tables := map[string]SQLTable{}
	for _, t := range expected {
		tables[t.Name] = t
	}
	rows := 0
	return ReadSQLSnapshot(r, func(t SQLTable) error {
		base, ok := tables[t.Name]
		if !ok {
			return errors.New("unsupported_schema")
		}
		return validateTeamDataTable(t, base)
	}, func(SQLTable, team.Row) error {
		rows++
		if rows > b.MaxItems {
			return errors.New("capacity_exceeded")
		}
		return nil
	})
}

// Older test installations contain runtime/Workbuddy columns from previous
// releases. Preserve their data without accepting arbitrary source DDL.
func teamDataLegacyColumns() map[string][]SQLColumn {
	col := func(name, typ, nullable string) SQLColumn {
		return SQLColumn{Name: name, Type: typ, Nullable: nullable}
	}
	return map[string][]SQLColumn{
		"instances":                 {col("workbuddy_storage_identity", "varchar(512)", "YES")},
		"instance_runtime_bindings": {col("storage_identity", "varchar(512)", "NO")},
		"runtime_pods":              {col("storage_identity", "varchar(512)", "NO"), col("desired_draining", "tinyint(1)", "NO")},
		"runtime_rollouts": {
			col("namespace", "varchar(128)", "YES"), col("deployment_name", "varchar(255)", "YES"), col("source_runtime_pod_id", "bigint", "YES"), col("old_image_ref", "varchar(512)", "YES"), col("compatibility_key", "varchar(128)", "YES"), col("batch_key", "varchar(64)", "YES"),
			col("batch_position", "int", "NO"), col("batch_total", "int", "NO"), col("drain_timeout_seconds", "int", "NO"), col("drain_deadline", "datetime", "YES"), col("phase_deadline", "datetime", "YES"), col("blocked_instances", "int", "NO"), col("restored_instances", "int", "NO"), col("total_instances", "int", "NO"), col("retry_count", "int", "NO"), col("active_deployment_key", "varchar(512)", "YES"), col("active_runtime_type_key", "varchar(32)", "YES"),
		},
	}
}

// These historical operational tables do not contain Team tasks or artifacts.
// They are outside a Team data copy; no corresponding workers are restored.
func teamDataHistoricalOperationTable(name string) bool {
	switch name {
	case "runtime_rollout_instances", "workbuddy_browser_grants", "workbuddy_instance_recoveries", "workbuddy_linux_identities", "workbuddy_linux_identity_allocator", "workbuddy_pod_terminations":
		return true
	}
	return false
}

func validateTeamDataTable(actual SQLTable, expected SQLTable) error {
	allowed := map[string]SQLColumn{}
	for _, c := range teamDataLegacyColumns()[actual.Name] {
		allowed[c.Name] = c
	}
	stripped := actual
	stripped.Columns = []SQLColumn{}
	for _, c := range actual.Columns {
		if legacy, ok := allowed[c.Name]; ok {
			if !reflect.DeepEqual(c, legacy) {
				return errors.New("unsupported_schema")
			}
		} else {
			stripped.Columns = append(stripped.Columns, c)
		}
	}
	if !reflect.DeepEqual(stripped, expected) {
		return errors.New("unsupported_schema")
	}
	return nil
}

func ValidateTeamDataSchema(ctx context.Context, q SQLQueryer) error {
	expected, e := BuiltinSchema()
	if e != nil {
		return e
	}
	names, e := discoverTables(ctx, q, true)
	if e != nil {
		return e
	}
	actual := map[string]bool{}
	for _, name := range names {
		if p, _ := Policy(name); p.Category != "system_backup" {
			actual[name] = true
		}
	}
	if len(actual) != len(expected) {
		return errors.New("unsupported_schema")
	}
	for _, table := range expected {
		if !actual[table.Name] {
			return errors.New("unsupported_schema")
		}
		found, e := ReadTableSchema(ctx, q, table.Name)
		if e != nil {
			return e
		}
		if e = validateTeamDataTable(found, table); e != nil {
			return e
		}
	}
	var executable int
	e = q.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()) + (SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE()) + (SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA=DATABASE())").Scan(&executable)
	if e == nil && executable != 0 {
		e = errors.New("unsupported_schema")
	}
	return e
}

// Only a new, owned, empty restore database receives these static additions.
// The caller authenticates the archive; types/defaults never come from SQL text.
func alignTeamDataRestoreSchema(ctx context.Context, db *sql.DB, r io.Reader) error {
	var name string
	if e := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); e != nil {
		return e
	}
	if !restoreDatabasePattern.MatchString(name) {
		return errors.New("invalid isolated restore database")
	}
	expected, e := BuiltinSchema()
	if e != nil {
		return e
	}
	byName := map[string]SQLTable{}
	for _, table := range expected {
		byName[table.Name] = table
	}
	_, e = ReadSQLSnapshot(r, func(table SQLTable) error {
		base, ok := byName[table.Name]
		if !ok {
			return errors.New("unsupported_schema")
		}
		if e := validateTeamDataTable(table, base); e != nil {
			return e
		}
		actual, e := ReadTableSchema(ctx, db, table.Name)
		if e != nil {
			return e
		}
		if reflect.DeepEqual(actual, table) {
			return nil
		}
		var count int
		quoted, _ := QuoteIdentifier(table.Name)
		if e = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return errors.New("restore refuses nonempty target table")
		}
		existing := map[string]bool{}
		for _, c := range actual.Columns {
			existing[c.Name] = true
		}
		allowed := map[string]SQLColumn{}
		for _, c := range teamDataLegacyColumns()[table.Name] {
			allowed[c.Name] = c
		}
		for i, c := range table.Columns {
			if existing[c.Name] {
				continue
			}
			trusted, ok := allowed[c.Name]
			if !ok || !reflect.DeepEqual(c, trusted) {
				return errors.New("unsupported_schema")
			}
			column, _ := QuoteIdentifier(c.Name)
			nullable := " NULL"
			if c.Nullable == "NO" {
				nullable = " NOT NULL"
			}
			position := " FIRST"
			if i > 0 {
				previous, _ := QuoteIdentifier(table.Columns[i-1].Name)
				position = " AFTER " + previous
			}
			if _, e = db.ExecContext(ctx, "ALTER TABLE "+quoted+" ADD COLUMN "+column+" "+strings.ToUpper(trusted.Type)+nullable+position); e != nil {
				return e
			}
		}
		actual, e = ReadTableSchema(ctx, db, table.Name)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(actual, table) {
			return errors.New("restore schema differs")
		}
		return nil
	}, nil)
	return e
}
