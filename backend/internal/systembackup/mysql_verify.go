package systembackup

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"clawreef/internal/systembackup/team"
)

// VerifyMySQL only issues SELECTs, using a read-only repeatable-read transaction.
// Rows are compared through one ordered cursor per table, avoiding a SQL round
// trip for every row while retaining exact value and extra/missing-row checks.
// expected must describe the normalized copy, not the unmodified source archive.
// The caller supplies a separate SELECT-only credential; this code cannot turn
// a privileged restore account into a verifier account.
func VerifyMySQL(ctx context.Context, db *sql.DB, expected io.Reader, maxRows int64) (SQLResult, error) {
	if maxRows < 1 {
		return SQLResult{}, errors.New("verification budget required")
	}
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return SQLResult{}, err
	}
	defer tx.Rollback()
	var checked int64
	var current SQLTable
	var actualRows *sql.Rows
	defer func() {
		if actualRows != nil {
			actualRows.Close()
		}
	}()
	finish := func() error {
		if current.Name == "" {
			return nil
		}
		if actualRows == nil {
			return errors.New("missing verification cursor")
		}
		if actualRows.Next() {
			return fmt.Errorf("row count mismatch: %s", current.Name)
		}
		if e := actualRows.Err(); e != nil {
			return e
		}
		if e := actualRows.Close(); e != nil {
			return e
		}
		actualRows = nil
		return nil
	}
	result, err := ReadSQLSnapshot(expected, func(table SQLTable) error {
		if e := finish(); e != nil {
			return e
		}
		actual, e := ReadTableSchema(ctx, tx, table.Name)
		if e != nil {
			return e
		}
		if !reflect.DeepEqual(table, actual) {
			return fmt.Errorf("schema mismatch: %s", table.Name)
		}
		current = table
		if len(table.PrimaryKey) == 0 {
			return errors.New("missing primary key")
		}
		order := make([]string, 0, len(table.PrimaryKey))
		for _, pk := range table.PrimaryKey {
			quoted, e := QuoteIdentifier(pk)
			if e != nil {
				return e
			}
			order = append(order, quoted)
		}
		quoted, e := QuoteIdentifier(table.Name)
		if e != nil {
			return e
		}
		actualRows, e = tx.QueryContext(ctx, "SELECT * FROM "+quoted+" ORDER BY "+strings.Join(order, ","))
		return e
	}, func(table SQLTable, row team.Row) error {
		checked++
		if checked > maxRows {
			return errors.New("verification row budget exceeded")
		}
		if actualRows == nil || !actualRows.Next() {
			if actualRows != nil && actualRows.Err() != nil {
				return actualRows.Err()
			}
			return errors.New("expected row unavailable")
		}
		cells := make([]any, len(table.Columns))
		targets := make([]any, len(cells))
		for i := range cells {
			targets[i] = &cells[i]
		}
		if e := actualRows.Scan(targets...); e != nil {
			return errors.New("expected row unavailable")
		}
		for i, c := range table.Columns {
			actual, e := sqlCell(cells[i], c)
			if e != nil {
				return e
			}
			want := row[c.Name]
			if want.IsNull != actual.IsNull || !bytes.Equal(want.Data, actual.Data) {
				return fmt.Errorf("value mismatch: %s.%s", table.Name, c.Name)
			}
		}
		return nil
	})
	if err != nil {
		return result, err
	}
	if err = finish(); err != nil {
		return result, err
	}
	return result, tx.Commit()
}
