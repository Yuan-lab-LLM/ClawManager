package systembackup

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"io"
	"reflect"
	"sort"

	"clawreef/internal/historymaintenance"
	"clawreef/internal/systembackup/team"
)

// MonthlyRegistration is frozen inside the authenticated SQL snapshot header.
// Discovery validates each registration against its own DB; no global mutable
// policy can make another installation's unregistered table admissible.
type MonthlyRegistration struct {
	Name    string `json:"name"`
	Kind    string `json:"kind"`
	Month   string `json:"month_utc"`
	Version int    `json:"schema_version"`
	SHA256  string `json:"schema_sha256"`
}

func monthlyPolicy(name string) ObjectPolicy {
	return ObjectPolicy{name, "history", "secret", "mysql_dump", "normalize"}
}
func capturePolicy(ctx context.Context, q SQLQueryer, name string) (ObjectPolicy, bool) {
	if p, ok := Policy(name); ok {
		return p, true
	}
	if err := historymaintenance.ValidateMonthly(ctx, q, name); err == nil {
		return monthlyPolicy(name), true
	}
	return ObjectPolicy{}, false
}
func freezeMonthly(ctx context.Context, q SQLQueryer, names []string) ([]MonthlyRegistration, error) {
	var out []MonthlyRegistration
	seen := map[string]bool{}
	for _, name := range names {
		if _, _, ok := historymaintenance.ParseMonthly(name); !ok {
			continue
		}
		if seen[name] {
			return nil, errors.New("duplicate monthly table")
		}
		seen[name] = true
		if err := historymaintenance.ValidateMonthly(ctx, q, name); err != nil {
			return nil, err
		}
		var r MonthlyRegistration
		r.Name = name
		if err := q.QueryRowContext(ctx, `SELECT kind,month_utc,schema_version,schema_sha256 FROM history_month_catalog WHERE table_name=?`, name).Scan(&r.Kind, &r.Month, &r.Version, &r.SHA256); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}
func monthlyManifest(registrations []MonthlyRegistration) (map[string]MonthlyRegistration, error) {
	if len(registrations) > 16800 {
		return nil, errors.New("monthly catalog exceeds bound")
	}
	out := map[string]MonthlyRegistration{}
	for _, r := range registrations {
		kind, month, ok := historymaintenance.ParseMonthly(r.Name)
		if !ok || kind != r.Kind || month != r.Month || r.Version != historymaintenance.SchemaVersion || !validHash(r.SHA256) {
			return nil, errors.New("invalid frozen monthly registration")
		}
		if _, ok = out[r.Name]; ok {
			return nil, errors.New("duplicate frozen monthly registration")
		}
		out[r.Name] = r
	}
	return out, nil
}
func monthlyTableSchema(name string) SQLTable {
	return SQLTable{Name: name, Columns: []SQLColumn{{Name: "original_id", Type: "bigint", Nullable: "NO"}, {Name: "owner_id", Type: "bigint", Nullable: "NO"}, {Name: "created_at", Type: "datetime(6)", Nullable: "NO"}, {Name: "trace_id", Type: "varchar(100)", Nullable: "NO"}, {Name: "instance_id", Type: "bigint", Nullable: "NO"}, {Name: "object_ref", Type: "json", Nullable: "NO"}, {Name: "payload_sha256", Type: "char(64)", Nullable: "NO"}, {Name: "payload_bytes", Type: "bigint", Nullable: "NO"}, {Name: "session_id", Type: "varchar(100)", Nullable: "NO"}, {Name: "team_id", Type: "bigint", Nullable: "NO"}, {Name: "metadata_json", Type: "json", Nullable: "YES"}, {Name: "payload_json", Type: "longtext", Nullable: "YES"}}, PrimaryKey: []string{"original_id"}}
}
func validateMonthlySQLTable(table SQLTable) error {
	if _, _, ok := historymaintenance.ParseMonthly(table.Name); !ok || !reflect.DeepEqual(table, monthlyTableSchema(table.Name)) {
		return errors.New("unsupported monthly SQL schema")
	}
	return nil
}
func sqlHeader(r io.Reader) (SQLRecord, io.Reader, error) {
	reader := bufio.NewReader(r)
	line, err := reader.ReadBytes('\n')
	if err != nil || len(line) > maxRowBytes {
		return SQLRecord{}, nil, errors.New("invalid SQL header")
	}
	var header SQLRecord
	if err = strictDecode(line, &header); err != nil || header.Kind != "mysql_rows_v1" {
		return header, nil, errors.New("invalid SQL header")
	}
	return header, io.MultiReader(bytes.NewReader(line), reader), nil
}

// Provision only trusted CREATE LIKE DDL into an already isolated target after
// the complete authenticated SQL stream is validated. RestoreMySQL remains a
// data-only importer; source text never supplies executable DDL.
func prepareMonthlyRestoreSchema(ctx context.Context, db *sql.DB, r io.Reader) error {
	var name string
	if err := db.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&name); err != nil {
		return err
	}
	if !restoreDatabasePattern.MatchString(name) {
		return errors.New("invalid isolated restore database")
	}
	var tables []SQLTable
	result, err := ReadSQLSnapshot(r, func(t SQLTable) error {
		if _, _, ok := historymaintenance.ParseMonthly(t.Name); ok {
			tables = append(tables, t)
		}
		return nil
	}, nil)
	if err != nil {
		return err
	}
	if len(tables) == 0 {
		return prepareHistoryAllocators(ctx, db, result.AutoIncrement)
	}
	template, err := ReadTableSchema(ctx, db, "history_month_template")
	if err != nil {
		return err
	}
	expected := monthlyTableSchema("history_month_template")
	if !reflect.DeepEqual(template, expected) {
		return errors.New("untrusted monthly restore template")
	}
	for _, table := range tables {
		if err = prepareMonthlyRestoreTable(ctx, db, table); err != nil {
			return err
		}
	}
	return prepareHistoryAllocators(ctx, db, result.AutoIncrement)
}
func prepareMonthlyRestoreTable(ctx context.Context, db *sql.DB, table SQLTable) error {
	if err := validateMonthlySQLTable(table); err != nil {
		return err
	}
	template, err := ReadTableSchema(ctx, db, "history_month_template")
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(template, monthlyTableSchema("history_month_template")) {
		return errors.New("untrusted monthly restore template")
	}
	quoted, _ := QuoteIdentifier(table.Name)
	var exists int
	if err = db.QueryRowContext(ctx, `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`, table.Name).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		if _, err = db.ExecContext(ctx, "CREATE TABLE "+quoted+" LIKE history_month_template"); err != nil {
			return err
		}
	}
	actual, err := ReadTableSchema(ctx, db, table.Name)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual, table) {
		return errors.New("monthly restore target differs")
	}
	var count int
	if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&count); err != nil {
		return err
	}
	if count != 0 {
		return errors.New("monthly restore target nonempty")
	}
	return nil
}
func preservedMonthly(table SQLTable, row team.Row) (team.Change, error) {
	if err := validateMonthlySQLTable(table); err != nil {
		return team.Change{}, err
	}
	return team.Change{Table: table.Name, Before: team.Clone(row), After: team.Clone(row), Reason: "preserved"}, nil
}
