package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"clawreef/internal/historymaintenance"
	"clawreef/internal/systembackup/team"
)

func historyAllocatorKind(kind string) bool {
	for _, k := range historymaintenance.MonthlyKinds {
		if kind == k {
			return true
		}
	}
	return false
}

var allocatorOption = regexp.MustCompile(`^\n\) ENGINE=InnoDB(?: AUTO_INCREMENT=([0-9]+))?(?: DEFAULT |$)`)

// information_schema.TABLES caches AUTO_INCREMENT statistics, including across
// ALTER TABLE. SHOW CREATE reads the current engine allocator without changing
// session state and is available to the existing SELECT-only verifier account.
func readHistoryAllocator(ctx context.Context, q SQLQueryer, name string) (sql.NullInt64, error) {
	if !historyAllocatorKind(name) {
		return sql.NullInt64{}, errors.New("unknown history allocator")
	}
	quoted, err := QuoteIdentifier(name)
	if err != nil {
		return sql.NullInt64{}, err
	}
	var table, ddl string
	if err = q.QueryRowContext(ctx, "SHOW CREATE TABLE "+quoted).Scan(&table, &ddl); err != nil {
		return sql.NullInt64{}, err
	}
	if table != name {
		return sql.NullInt64{}, errors.New("allocator table identity differs")
	}
	at := strings.LastIndex(ddl, "\n) ENGINE=")
	if at < 0 {
		return sql.NullInt64{}, errors.New("allocator table options unavailable")
	}
	if !strings.Contains(ddl[:at], " AUTO_INCREMENT") {
		return sql.NullInt64{}, nil
	}
	match := allocatorOption.FindStringSubmatch(ddl[at:])
	if match == nil {
		return sql.NullInt64{}, errors.New("unsupported allocator table options")
	}
	next := int64(1)
	if match[1] != "" {
		next, err = strconv.ParseInt(match[1], 10, 64)
		if err != nil || next < 1 {
			return sql.NullInt64{}, errors.New("invalid native allocator")
		}
	}
	return sql.NullInt64{Int64: next, Valid: true}, nil
}

func captureHistoryAllocators(ctx context.Context, q SQLQueryer, tables []string, teamData bool) (map[string]uint64, error) {
	out := map[string]uint64{}
	for _, name := range tables {
		if !historyAllocatorKind(name) || (teamData && teamDataOmitsRows(name)) {
			continue
		}
		next, err := readHistoryAllocator(ctx, q, name)
		if err != nil {
			return nil, err
		}
		if next.Valid && next.Int64 > 0 {
			out[name] = uint64(next.Int64)
		}
	}
	return out, nil
}

// Floors come from authenticated native rows, monthly rows, archive identities
// and the captured allocator. Older v1 snapshots need no new field to protect
// archived IDs. No counter or serialization is added to ordinary writers.
func historyAllocatorRow(floors map[string]uint64, table string, row team.Row) error {
	kind, column := table, "id"
	if k, _, ok := historymaintenance.ParseMonthly(table); ok {
		kind, column = k, "original_id"
	}
	if table == "history_archive_records" {
		kind, column = string(row["kind"].Data), "original_id"
	}
	if !historyAllocatorKind(kind) {
		return nil
	}
	value, ok := row[column]
	if !ok || value.IsNull {
		return errors.New("history allocator identity unavailable")
	}
	id, err := strconv.ParseUint(string(value.Data), 10, 63)
	if err != nil || id == uint64(1<<63-1) {
		return errors.New("invalid history allocator identity")
	}
	if id+1 > floors[kind] {
		floors[kind] = id + 1
	}
	return nil
}

func verifyHistoryAllocators(ctx context.Context, q SQLQueryer, floors map[string]uint64) error {
	for name, want := range floors {
		next, err := readHistoryAllocator(ctx, q, name)
		if err != nil {
			return err
		}
		if !next.Valid || next.Int64 <= 0 || uint64(next.Int64) < want {
			return fmt.Errorf("native history allocator below authenticated floor: %s", name)
		}
	}
	return nil
}

// Only the already isolated, empty schema preparation phase uses trusted DDL.
// The importer and verifier remain DML-only and SELECT-only respectively.
func prepareHistoryAllocators(ctx context.Context, db *sql.DB, floors map[string]uint64) error {
	var names []string
	for name := range floors {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if !historyAllocatorKind(name) {
			return errors.New("unknown history allocator")
		}
		quoted, err := QuoteIdentifier(name)
		if err != nil {
			return err
		}
		var count int64
		if err = db.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return errors.New("allocator preparation requires empty restore source")
		}
		current, err := readHistoryAllocator(ctx, db, name)
		if err != nil {
			return err
		}
		if current.Valid && current.Int64 > 0 && uint64(current.Int64) >= floors[name] {
			continue
		}
		if _, err = db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE %s AUTO_INCREMENT=%d", quoted, floors[name])); err != nil {
			return err
		}
	}
	return verifyHistoryAllocators(ctx, db, floors)
}
