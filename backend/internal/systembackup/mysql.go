package systembackup

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"clawreef/internal/historymaintenance"
	"clawreef/internal/systembackup/team"
)

const maxRowBytes = 64 << 20

type SQLColumn struct {
	Name                 string `json:"name"`
	Type                 string `json:"type"`
	Nullable             string `json:"nullable"`
	Extra                string `json:"extra"`
	GenerationExpression string `json:"generation_expression,omitempty"`
}
type SQLTable struct {
	Name       string      `json:"name"`
	Columns    []SQLColumn `json:"columns"`
	PrimaryKey []string    `json:"primary_key"`
}
type SQLRecord struct {
	Kind           string                `json:"kind"`
	Table          *SQLTable             `json:"table,omitempty"`
	Cells          []team.Cell           `json:"cells,omitempty"`
	Count          int64                 `json:"count,omitempty"`
	SHA256         string                `json:"sha256,omitempty"`
	MonthlyCatalog []MonthlyRegistration `json:"monthly_catalog,omitempty"`
	AutoIncrement  map[string]uint64     `json:"auto_increment,omitempty"`
}
type SQLResult struct {
	Tables         map[string]int64      `json:"tables"`
	Hashes         map[string]string     `json:"hashes"`
	MonthlyCatalog []MonthlyRegistration `json:"monthly_catalog,omitempty"`
	AutoIncrement  map[string]uint64     `json:"auto_increment,omitempty"`
}

type SQLQueryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func DiscoverTables(ctx context.Context, q SQLQueryer) ([]string, error) {
	return discoverTables(ctx, q, false)
}
func discoverTables(ctx context.Context, q SQLQueryer, teamData bool) ([]string, error) {
	rows, err := q.QueryContext(ctx, "SELECT TABLE_NAME,TABLE_TYPE,COALESCE(ENGINE,'') FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() ORDER BY TABLE_NAME")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var name, kind, engine string
		if err = rows.Scan(&name, &kind, &engine); err != nil {
			return nil, err
		}
		if kind != "BASE TABLE" || engine != "InnoDB" {
			return nil, fmt.Errorf("unsupported schema object: %s", name)
		}
		if _, ok := Policy(name); !ok {
			if _, _, monthly := historymaintenance.ParseMonthly(name); monthly {
				out = append(out, name)
				continue
			}
			if teamData && teamDataHistoricalOperationTable(name) {
				continue
			}
			return nil, fmt.Errorf("unassigned schema object: %s", name)
		}
		out = append(out, name)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, name := range out {
		if _, _, monthly := historymaintenance.ParseMonthly(name); monthly {
			if err = historymaintenance.ValidateMonthly(ctx, q, name); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

func ReadTableSchema(ctx context.Context, q SQLQueryer, name string) (SQLTable, error) {
	t := SQLTable{Name: name, Columns: []SQLColumn{}, PrimaryKey: []string{}}
	if _, err := QuoteIdentifier(name); err != nil {
		return t, err
	}
	rows, err := q.QueryContext(ctx, "SELECT COLUMN_NAME,COLUMN_TYPE,IS_NULLABLE,EXTRA,COALESCE(GENERATION_EXPRESSION,'') FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? ORDER BY ORDINAL_POSITION", name)
	if err != nil {
		return t, err
	}
	for rows.Next() {
		var c SQLColumn
		if err = rows.Scan(&c.Name, &c.Type, &c.Nullable, &c.Extra, &c.GenerationExpression); err != nil {
			rows.Close()
			return t, err
		}
		if c.GenerationExpression != "" && !((name == "instances" && c.Name == "owner_normalized") || (name == "users" && c.Name == "local_username_key")) {
			rows.Close()
			return t, fmt.Errorf("generated column requires explicit policy: %s", name)
		}
		t.Columns = append(t.Columns, c)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return t, err
	}
	if len(t.Columns) == 0 {
		return t, errors.New("table schema missing")
	}
	rows, err = q.QueryContext(ctx, "SELECT COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND CONSTRAINT_NAME='PRIMARY' ORDER BY ORDINAL_POSITION", name)
	if err != nil {
		return t, err
	}
	defer rows.Close()
	for rows.Next() {
		var column string
		if err = rows.Scan(&column); err != nil {
			return t, err
		}
		t.PrimaryKey = append(t.PrimaryKey, column)
	}
	if len(t.PrimaryKey) == 0 {
		return t, fmt.Errorf("table requires deterministic primary key: %s", name)
	}
	return t, rows.Err()
}

// ExportMySQL streams a single read-only transaction. This is only the MySQL
// checkpoint; the orchestrator must separately prove the cross-source barrier.
func ExportMySQL(ctx context.Context, db *sql.DB, w io.Writer, tables []string) (SQLResult, error) {
	return exportMySQL(ctx, db, w, tables, false)
}

// ExportTeamDataMySQL retains the complete schema and Team/instance data while
// excluding the unrelated global AI gateway chat, metering and risk logs.
// Empty table records keep the existing isolated restore format and FK schema.
func ExportTeamDataMySQL(ctx context.Context, db *sql.DB, w io.Writer, tables []string) (SQLResult, error) {
	return exportMySQL(ctx, db, w, tables, true)
}

func teamDataOmitsRows(table string) bool {
	if strings.HasPrefix(table, "observation_") || strings.HasPrefix(table, "observability_") {
		return true
	}
	switch table {
	case "model_invocations", "audit_events", "cost_records", "risk_hits", "chat_sessions", "chat_messages", "history_log_locations":
		return true
	}
	return false
}

func teamDataExcludedTables() []string {
	out := []string{"audit_events", "chat_messages", "chat_sessions", "cost_records", "model_invocations", "risk_hits", "history_log_locations"}
	for _, p := range Registry() {
		if (strings.HasPrefix(p.Name, "observation_") || strings.HasPrefix(p.Name, "observability_")) && p.BackupStrategy == "mysql_dump" {
			out = append(out, p.Name)
		}
	}
	sort.Strings(out)
	return out
}

func exportMySQL(ctx context.Context, db *sql.DB, w io.Writer, tables []string, teamData bool) (SQLResult, error) {
	result := SQLResult{Tables: map[string]int64{}, Hashes: map[string]string{}}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return result, err
	}
	defer tx.Rollback()
	if teamData {
		filtered := make([]string, 0, len(tables))
		for _, name := range tables {
			if kind, _, monthly := historymaintenance.ParseMonthly(name); monthly && kind != "team_events" && kind != "team_tasks" {
				continue
			}
			filtered = append(filtered, name)
		}
		tables = filtered
	}
	catalog, err := freezeMonthly(ctx, tx, tables)
	if err != nil {
		return result, err
	}
	result.MonthlyCatalog = catalog
	allocators, err := captureHistoryAllocators(ctx, tx, tables, teamData)
	if err != nil {
		return result, err
	}
	encoder := json.NewEncoder(w)
	if err = encoder.Encode(SQLRecord{Kind: "mysql_rows_v1", MonthlyCatalog: catalog, AutoIncrement: allocators}); err != nil {
		return result, err
	}
	seen := map[string]bool{}
	for _, name := range tables {
		p, ok := capturePolicy(ctx, tx, name)
		if !ok || p.BackupStrategy != "mysql_dump" || seen[name] {
			return result, fmt.Errorf("table excluded or duplicated: %s", name)
		}
		seen[name] = true
		table, e := ReadTableSchema(ctx, tx, name)
		if e != nil {
			return result, e
		}
		if e = encoder.Encode(SQLRecord{Kind: "table", Table: &table}); e != nil {
			return result, e
		}
		quoted, _ := QuoteIdentifier(name)
		var order []string
		for _, column := range table.PrimaryKey {
			q, e := QuoteIdentifier(column)
			if e != nil {
				return result, e
			}
			order = append(order, q)
		}
		where := ""
		if teamData && teamDataOmitsRows(name) {
			where = " WHERE 1=0"
		}
		if teamData && (name == "history_archive_records" || name == "history_month_catalog" || name == "history_identity_receipts" || name == "history_maintenance_cursors") {
			where = " WHERE kind IN ('team_events','team_tasks')"
		}
		rows, e := tx.QueryContext(ctx, "SELECT * FROM "+quoted+where+" ORDER BY "+strings.Join(order, ","))
		if e != nil {
			return result, e
		}
		hash := sha256.New()
		var count int64
		for rows.Next() {
			cells := make([]team.Cell, len(table.Columns))
			raw := make([]any, len(cells))
			targets := make([]any, len(cells))
			for i := range raw {
				targets[i] = &raw[i]
			}
			if e = rows.Scan(targets...); e != nil {
				rows.Close()
				return result, e
			}
			for i, b := range raw {
				cells[i], e = sqlCell(b, table.Columns[i])
				if e != nil {
					rows.Close()
					return result, e
				}
			}
			encoded, e := json.Marshal(SQLRecord{Kind: "row", Cells: cells})
			if e != nil {
				rows.Close()
				return result, e
			}
			if len(encoded) > maxRowBytes {
				rows.Close()
				return result, errors.New("database row exceeds capture budget")
			}
			encoded = append(encoded, '\n')
			hash.Write(encoded)
			if e = writeFull(w, encoded); e != nil {
				rows.Close()
				return result, e
			}
			count++
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return result, e
		}
		digest := hex.EncodeToString(hash.Sum(nil))
		result.Tables[name] = count
		result.Hashes[name] = digest
		if e = encoder.Encode(SQLRecord{Kind: "end_table", Count: count, SHA256: digest}); e != nil {
			return result, e
		}
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, encoder.Encode(SQLRecord{Kind: "end"})
}

// ReadSQLSnapshot parses bounded records and verifies each table's count/hash
// and final EOF. onRow is called only for authenticated archive data supplied
// by the caller; the target remains isolated until the entire stream passes.
func ReadSQLSnapshot(r io.Reader, onTable func(SQLTable) error, onRow func(SQLTable, team.Row) error) (SQLResult, error) {
	result := SQLResult{Tables: map[string]int64{}, Hashes: map[string]string{}}
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64<<10), maxRowBytes+1)
	var table *SQLTable
	hash := sha256.New()
	var count int64
	started, ended := false, false
	monthly := map[string]MonthlyRegistration{}
	floors := map[string]uint64{}
	allocatorTables := map[string]bool{}
	declared := map[string]uint64{}
	for scanner.Scan() {
		if ended {
			return result, errors.New("records after SQL end")
		}
		line := scanner.Bytes()
		var record SQLRecord
		if err := strictDecode(line, &record); err != nil {
			return result, errors.New("invalid SQL snapshot record")
		}
		if !started {
			if record.Kind != "mysql_rows_v1" {
				return result, errors.New("unsupported SQL snapshot")
			}
			var err error
			monthly, err = monthlyManifest(record.MonthlyCatalog)
			if err != nil {
				return result, err
			}
			result.MonthlyCatalog = record.MonthlyCatalog
			for kind, next := range record.AutoIncrement {
				if !historyAllocatorKind(kind) || next == 0 || next > uint64(1<<63-1) {
					return result, errors.New("invalid history allocator floor")
				}
				floors[kind] = next
				declared[kind] = next
			}
			started = true
			continue
		}
		switch record.Kind {
		case "table":
			if table != nil || record.Table == nil {
				return result, errors.New("nested or missing table")
			}
			p, ok := Policy(record.Table.Name)
			if _, registered := monthly[record.Table.Name]; registered {
				if err := validateMonthlySQLTable(*record.Table); err != nil {
					return result, err
				}
				p = monthlyPolicy(record.Table.Name)
				ok = true
			}
			if !ok || p.BackupStrategy != "mysql_dump" {
				return result, errors.New("excluded table in SQL payload")
			}
			if _, ok := result.Tables[record.Table.Name]; ok {
				return result, errors.New("duplicate table")
			}
			if len(record.Table.Columns) == 0 {
				return result, errors.New("empty column schema")
			}
			seen := map[string]bool{}
			for _, c := range record.Table.Columns {
				if _, err := QuoteIdentifier(c.Name); err != nil || seen[c.Name] {
					return result, errors.New("invalid or duplicate column")
				}
				seen[c.Name] = true
			}
			table = record.Table
			for _, c := range table.Columns {
				if historyAllocatorKind(table.Name) && c.Name == "id" && strings.Contains(c.Extra, "auto_increment") {
					allocatorTables[table.Name] = true
				}
			}
			count = 0
			hash.Reset()
			if onTable != nil {
				if err := onTable(*table); err != nil {
					return result, err
				}
			}
		case "row":
			if table == nil || len(record.Cells) != len(table.Columns) {
				return result, errors.New("row schema mismatch")
			}
			row := make(team.Row, len(record.Cells))
			for i, c := range record.Cells {
				if c.IsNull && len(c.Data) > 0 {
					return result, errors.New("ambiguous SQL null")
				}
				row[table.Columns[i].Name] = c
			}
			if err := historyAllocatorRow(floors, table.Name, row); err != nil {
				return result, err
			}
			canonical, _ := json.Marshal(record)
			hash.Write(append(canonical, '\n'))
			count++
			if onRow != nil {
				if err := onRow(*table, row); err != nil {
					return result, err
				}
			}
		case "end_table":
			if table == nil || count != record.Count || hex.EncodeToString(hash.Sum(nil)) != record.SHA256 {
				return result, errors.New("SQL table checksum mismatch")
			}
			result.Tables[table.Name] = count
			result.Hashes[table.Name] = record.SHA256
			table = nil
		case "end":
			if table != nil {
				return result, errors.New("SQL table not sealed")
			}
			ended = true
		default:
			return result, errors.New("unknown SQL snapshot record")
		}
	}
	if err := scanner.Err(); err != nil {
		return result, err
	}
	if !ended {
		return result, errors.New("incomplete SQL snapshot")
	}
	for name := range monthly {
		if _, ok := result.Tables[name]; !ok {
			return result, errors.New("monthly manifest table missing")
		}
	}
	for kind := range declared {
		if !allocatorTables[kind] {
			return result, errors.New("allocator metadata has no native AUTO_INCREMENT source")
		}
	}
	result.AutoIncrement = map[string]uint64{}
	for kind, next := range floors {
		if allocatorTables[kind] {
			result.AutoIncrement[kind] = next
		}
	}
	return result, nil
}

// DependencyOrder produces a deterministic parent-first import order. Cycles
// fail closed instead of disabling foreign-key checks and hiding corruption.
func DependencyOrder(tables []string, dependencies map[string][]string) ([]string, error) {
	remaining := map[string]bool{}
	for _, t := range tables {
		remaining[t] = true
	}
	var out []string
	for len(remaining) > 0 {
		var ready []string
		for t := range remaining {
			blocked := false
			for _, parent := range dependencies[t] {
				if parent == t {
					continue
				}
				if remaining[parent] {
					blocked = true
				}
			}
			if !blocked {
				ready = append(ready, t)
			}
		}
		if len(ready) == 0 {
			return nil, errors.New("cyclic table dependencies require explicit restore policy")
		}
		sort.Strings(ready)
		for _, t := range ready {
			delete(remaining, t)
			out = append(out, t)
		}
	}
	return out, nil
}
