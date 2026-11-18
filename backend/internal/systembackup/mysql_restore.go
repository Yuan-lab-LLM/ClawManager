package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"strings"

	"clawreef/internal/systembackup/team"
)

var restoreDatabasePattern = regexp.MustCompile(`^cm_restore_[a-f0-9]{32}$`)

// RestoreMySQL is a data-only importer for a freshly provisioned target schema.
// The runner must verify Kubernetes/provider isolation and schema ownership
// before invoking it. It never creates databases, changes DDL, disables foreign
// keys, deletes existing rows or connects to the source database.
func RestoreMySQL(ctx context.Context, db *sql.DB, sourceDatabase, targetDatabase string, r io.Reader, normalize func(SQLTable, team.Row) (team.Row, error)) (SQLResult, error) {
	if !restoreDatabasePattern.MatchString(targetDatabase) || sourceDatabase == targetDatabase {
		return SQLResult{}, errors.New("invalid isolated restore database")
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return SQLResult{}, err
	}
	defer conn.Close()
	var current string
	if err = conn.QueryRowContext(ctx, "SELECT DATABASE()").Scan(&current); err != nil {
		return SQLResult{}, err
	}
	if current != targetDatabase {
		return SQLResult{}, errors.New("restore database identity mismatch")
	}
	var executable int
	if err = conn.QueryRowContext(ctx, "SELECT (SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA=DATABASE()) + (SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA=DATABASE()) + (SELECT COUNT(*) FROM information_schema.ROUTINES WHERE ROUTINE_SCHEMA=DATABASE())").Scan(&executable); err != nil {
		return SQLResult{}, err
	}
	if executable != 0 {
		return SQLResult{}, errors.New("executable target schema is not permitted")
	}
	// Exact IDs (including zero) and historical timestamps must not be remapped
	// by MySQL defaults. sql.Conn keeps these settings private to this import.
	var oldMode string
	if err = conn.QueryRowContext(ctx, "SELECT @@SESSION.sql_mode").Scan(&oldMode); err != nil {
		return SQLResult{}, err
	}
	defer conn.ExecContext(context.Background(), "SET SESSION sql_mode=?", oldMode)
	mode := oldMode
	if !strings.Contains(mode, "NO_AUTO_VALUE_ON_ZERO") {
		mode += ",NO_AUTO_VALUE_ON_ZERO"
	}
	if _, err = conn.ExecContext(ctx, "SET SESSION sql_mode=?", mode); err != nil {
		return SQLResult{}, err
	}
	tx, err := conn.BeginTx(ctx, nil)
	if err != nil {
		return SQLResult{}, err
	}
	defer tx.Rollback()
	var insert *sql.Stmt
	defer func() {
		if insert != nil {
			insert.Close()
		}
	}()
	result, err := ReadSQLSnapshot(r, func(table SQLTable) error {
		actual, e := ReadTableSchema(ctx, tx, table.Name)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(actual, table) {
			return fmt.Errorf("restore schema differs: %s", table.Name)
		}
		quoted, _ := QuoteIdentifier(table.Name)
		var count int64
		if e = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+quoted).Scan(&count); e != nil {
			return e
		}
		if count != 0 {
			return errors.New("restore refuses nonempty target table")
		}
		if insert != nil {
			insert.Close()
		}
		var columns, values []string
		for _, c := range table.Columns {
			if c.GenerationExpression != "" {
				continue
			}
			q, e := QuoteIdentifier(c.Name)
			if e != nil {
				return e
			}
			columns = append(columns, q)
			values = append(values, "?")
		}
		insert, e = tx.PrepareContext(ctx, "INSERT INTO "+quoted+" ("+strings.Join(columns, ",")+") VALUES ("+strings.Join(values, ",")+")")
		return e
	}, func(table SQLTable, row team.Row) error {
		if normalize != nil {
			var e error
			row, e = normalize(table, row)
			if e != nil {
				return e
			}
		}
		if len(row) != len(table.Columns) {
			return errors.New("normalizer changed row shape")
		}
		values := make([]any, 0, len(table.Columns))
		for _, c := range table.Columns {
			v, ok := row[c.Name]
			if !ok {
				return errors.New("normalizer lost a column")
			}
			if c.GenerationExpression != "" {
				continue
			}
			if !v.IsNull {
				values = append(values, nonNullSQLBytes(v.Data))
			} else {
				values = append(values, nil)
			}
		}
		_, e := insert.ExecContext(ctx, values...)
		return e
	})
	if err != nil {
		return result, err
	}
	if err = tx.Commit(); err != nil {
		return result, err
	}
	return result, nil
}
