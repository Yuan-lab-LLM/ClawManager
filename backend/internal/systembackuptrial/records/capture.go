package records

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"
	"strings"
	"time"
)

type queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Catalog reads metadata only. The supplied connection must already select the
// desired database; unlike the application startup path it never runs migrations.
func Catalog(ctx context.Context, q queryer) ([]Table, error) {
	return catalogGuarded(ctx, q, nil)
}

type sourceGuardedQuery struct {
	queryer
	check func() error
}

func (q sourceGuardedQuery) QueryContext(ctx context.Context, statement string, args ...any) (*sql.Rows, error) {
	if sourceReadGuard(ctx, q.check) != nil {
		return nil, ErrIsolation
	}
	finish := BeginCaptureTiming(ctx, CaptureSQLQuery)
	rows, err := q.queryer.QueryContext(ctx, statement, args...)
	finish.Finish(err)
	if err != nil {
		return nil, err
	}
	if sourceReadGuard(ctx, q.check) != nil {
		rows.Close()
		return nil, ErrIsolation
	}
	return rows, nil
}
func sourceReadGuard(ctx context.Context, check func() error) error {
	if ctx == nil || ctx.Err() != nil {
		return ErrIsolation
	}
	if check != nil {
		finish := BeginCaptureTiming(ctx, CaptureGuard)
		err := check()
		finish.Finish(err)
		if err != nil {
			return ErrIsolation
		}
	}
	return nil
}
func sourceRowsNext(ctx context.Context, rows *sql.Rows, check func() error) (bool, error) {
	if sourceReadGuard(ctx, check) != nil {
		rows.Close()
		return false, ErrIsolation
	}
	finish := BeginCaptureTiming(ctx, CaptureSQLNext)
	next := rows.Next()
	finish.Finish(rows.Err())
	if sourceReadGuard(ctx, check) != nil {
		rows.Close()
		return false, ErrIsolation
	}
	return next, nil
}
func catalogGuarded(ctx context.Context, original queryer, check func() error) ([]Table, error) {
	q := sourceGuardedQuery{original, check}
	rows, err := q.QueryContext(ctx, `SELECT TABLE_NAME, COALESCE(ENGINE, '') FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE' ORDER BY TABLE_NAME`)
	if err != nil {
		return nil, ErrDatabase
	}
	var tables []Table
	for {
		next, guardErr := sourceRowsNext(ctx, rows, check)
		if guardErr != nil {
			return nil, guardErr
		}
		if !next {
			break
		}
		var t Table
		if rows.Scan(&t.Name, &t.Engine) != nil {
			rows.Close()
			return nil, ErrDatabase
		}
		if !identifier.MatchString(t.Name) {
			rows.Close()
			return nil, ErrSchema
		}
		tables = append(tables, t)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, ErrDatabase
	}
	for i := range tables {
		t := &tables[i]
		rows, err = q.QueryContext(ctx, `SELECT COLUMN_NAME, COLUMN_TYPE, IS_NULLABLE, COLUMN_DEFAULT, EXTRA FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? ORDER BY ORDINAL_POSITION`, t.Name)
		if err != nil {
			return nil, ErrDatabase
		}
		for {
			next, guardErr := sourceRowsNext(ctx, rows, check)
			if guardErr != nil {
				return nil, guardErr
			}
			if !next {
				break
			}
			var c Column
			var nullable string
			var def sql.NullString
			if rows.Scan(&c.Name, &c.Type, &nullable, &def, &c.Extra) != nil {
				rows.Close()
				return nil, ErrDatabase
			}
			if !identifier.MatchString(c.Name) {
				rows.Close()
				return nil, ErrSchema
			}
			c.Nullable = nullable == "YES"
			if def.Valid {
				s := def.String
				c.Default = &s
			}
			t.Columns = append(t.Columns, c)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, ErrDatabase
		}
		rows, err = q.QueryContext(ctx, `SELECT COLUMN_NAME, CONSTRAINT_NAME, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME FROM information_schema.KEY_COLUMN_USAGE WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND (CONSTRAINT_NAME = 'PRIMARY' OR REFERENCED_TABLE_NAME IS NOT NULL) ORDER BY CONSTRAINT_NAME, ORDINAL_POSITION`, t.Name)
		if err != nil {
			return nil, ErrDatabase
		}
		for {
			next, guardErr := sourceRowsNext(ctx, rows, check)
			if guardErr != nil {
				return nil, guardErr
			}
			if !next {
				break
			}
			var col, key string
			var refTable, refCol sql.NullString
			if rows.Scan(&col, &key, &refTable, &refCol) != nil {
				rows.Close()
				return nil, ErrDatabase
			}
			if key == "PRIMARY" {
				t.PrimaryKey = append(t.PrimaryKey, col)
			} else {
				t.ForeignKeys = append(t.ForeignKeys, ForeignKey{col, refTable.String, refCol.String})
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, ErrDatabase
		}
	}
	return tables, nil
}

type capturePredicate struct {
	clause string
	args   []any
}

func captureRows(ctx context.Context, db *sql.DB, limits Limits, check func(*sql.Conn) error, readCatalog func(context.Context, queryer, func() error) ([]Table, error), predicates map[string]capturePredicate) (Snapshot, error) {
	if sourceReadGuard(ctx, nil) != nil {
		return Snapshot{}, ErrIsolation
	}
	if db == nil || !limits.valid() {
		return Snapshot{}, ErrData
	}
	finishConnection := BeginCaptureTiming(ctx, CaptureConnection)
	conn, err := db.Conn(ctx)
	finishConnection.Finish(err)
	if err != nil {
		return Snapshot{}, ErrDatabase
	}
	defer conn.Close()
	if check != nil {
		finish := BeginCaptureTiming(ctx, CaptureSessionBefore)
		err = check(conn)
		finish.Finish(err)
		if err != nil {
			return Snapshot{}, err
		}
	}
	if sourceReadGuard(ctx, nil) != nil {
		return Snapshot{}, ErrIsolation
	}
	finishBegin := BeginCaptureTiming(ctx, CaptureBeginRR)
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	finishBegin.Finish(err)
	if err != nil {
		return Snapshot{}, ErrDatabase
	}
	defer tx.Rollback()
	finishCatalog := BeginCaptureTiming(ctx, CaptureCatalogInitial)
	tables, err := readCatalog(ctx, tx, nil)
	finishCatalog.Finish(err)
	if err != nil {
		return Snapshot{}, err
	}
	result := Snapshot{Version: Version}
	var byteCount, rowCount int64
	for _, t := range tables {
		if t.Engine != "InnoDB" || len(t.PrimaryKey) == 0 || len(t.Columns) == 0 {
			return Snapshot{}, ErrSchema
		}
		var cols, pk []string
		for _, c := range t.Columns {
			name, _ := quoted(c.Name)
			cols = append(cols, name)
		}
		for _, c := range t.PrimaryKey {
			name, e := quoted(c)
			if e != nil {
				return Snapshot{}, e
			}
			pk = append(pk, name)
		}
		name, _ := quoted(t.Name)
		predicate := predicates[t.Name]
		rows, err := (sourceGuardedQuery{tx, nil}).QueryContext(ctx, "SELECT "+strings.Join(cols, ",")+" FROM "+name+predicate.clause+" ORDER BY "+strings.Join(pk, ","), predicate.args...)
		if err != nil {
			return Snapshot{}, ErrDatabase
		}
		// Reuse only scan slots within this table. rawCell still owns every
		// returned byte slice; rows and authorization boundaries are unchanged.
		scan := newCaptureScanSlots(len(cols))
		for {
			next, guardErr := sourceRowsNext(ctx, rows, nil)
			if guardErr != nil {
				return Snapshot{}, guardErr
			}
			if !next {
				break
			}
			values, scanErr := scan.scan(ctx, rows)
			if scanErr != nil {
				rows.Close()
				return Snapshot{}, ErrDatabase
			}
			r := make(Row, len(cols))
			for i, v := range values {
				cell, e := rawCell(v)
				if e != nil {
					rows.Close()
					return Snapshot{}, e
				}
				// Include map/cell overhead in the bounded memory estimate.
				byteCount += int64(len(cell.Bytes) + len(t.Columns[i].Name) + 128)
				if byteCount > limits.MaxBytes {
					rows.Close()
					return Snapshot{}, ErrCapacity
				}
				r[t.Columns[i].Name] = cell
			}
			clear(values) // release driver-owned intermediate references per row
			rowCount++
			if rowCount > limits.MaxRows {
				rows.Close()
				return Snapshot{}, ErrCapacity
			}
			t.Rows = append(t.Rows, r)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return Snapshot{}, ErrDatabase
		}
		result.Tables = append(result.Tables, t)
	}
	if predicates != nil {
		// Each projected SELECT now holds a metadata lock on its source tables.
		// Refuse schema changes between the initial catalog and the row reads.
		locked, err := readCatalog(ctx, tx, nil)
		if err != nil {
			return Snapshot{}, err
		}
		if !reflect.DeepEqual(tables, locked) {
			return Snapshot{}, ErrSchema
		}
	}
	for _, required := range []string{"users", "user_quotas", "instances"} {
		found := false
		for _, t := range result.Tables {
			if t.Name == required {
				found = true
			}
		}
		if !found {
			return Snapshot{}, ErrSchema
		}
	}
	if sourceReadGuard(ctx, nil) != nil {
		return Snapshot{}, ErrIsolation
	}
	finishCommit := BeginCaptureTiming(ctx, CaptureCommitRR)
	err = tx.Commit()
	finishCommit.Finish(err)
	if err != nil {
		return Snapshot{}, ErrDatabase
	}
	// Recheck the original session after the complete RR, before returning any
	// source bytes to the encrypting collector. A mid-capture grant/identity
	// change cannot be certified by an earlier admission observation.
	if check != nil {
		finish := BeginCaptureTiming(ctx, CaptureSessionAfter)
		err = check(conn)
		finish.Finish(err)
		if err != nil {
			return Snapshot{}, ErrIsolation
		}
	}
	if sourceReadGuard(ctx, nil) != nil {
		return Snapshot{}, ErrIsolation
	}
	return result, nil
}

func rawCell(v any) (Cell, error) {
	switch v := v.(type) {
	case nil:
		return Cell{Null: true}, nil
	case []byte:
		return Cell{Bytes: append([]byte(nil), v...)}, nil
	case string:
		return Text(v), nil
	case int64:
		return Text(fmt.Sprint(v)), nil
	case uint64:
		return Text(fmt.Sprint(v)), nil
	case float64:
		return Text(fmt.Sprint(v)), nil
	case bool:
		if v {
			return Text("1"), nil
		}
		return Text("0"), nil
	case time.Time:
		// Require parseTime=false on a capture connection. Otherwise zero dates
		// and source timezone representations may already have been lost.
		return Cell{}, ErrSchema
	default:
		return Cell{}, ErrSchema
	}
}
