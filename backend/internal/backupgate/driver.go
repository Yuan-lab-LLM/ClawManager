// Package backupgate fences application SQL connections at the driver boundary.
// A separate, ungated control connection manages the gate. Filesystem, object,
// Redis and Runtime writers still require their own drain participants.
package backupgate

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"regexp"
	"strings"
	"sync"
)

var ErrMaintenance = errors.New("backup maintenance in progress")
var scopePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.:-]{0,127}$`)

type connector struct {
	base  driver.Connector
	scope string
}

// Open wraps every application SQL operation, including prepared statements
// and transactions used by background workers. It does not classify writes by SQL text or depend
// on callers remembering a middleware lease. Gate rows must be bootstrapped
// before opening this pool; a missing gate fails closed.
func Open(base driver.Connector, scope string) (*sql.DB, error) {
	if base == nil || !scopePattern.MatchString(scope) {
		return nil, errors.New("invalid SQL gate scope")
	}
	return sql.OpenDB(connector{base, scope}), nil
}
func (c connector) Driver() driver.Driver { return c.base.Driver() }
func (c connector) Connect(ctx context.Context) (driver.Conn, error) {
	b, e := c.base.Connect(ctx)
	if e != nil {
		return nil, e
	}
	if _, ok := b.(driver.ConnBeginTx); !ok {
		b.Close()
		return nil, errors.New("transactional SQL driver required")
	}
	if _, ok := b.(driver.QueryerContext); !ok {
		b.Close()
		return nil, errors.New("context SQL driver required")
	}
	if _, ok := b.(driver.ExecerContext); !ok {
		b.Close()
		return nil, errors.New("context SQL driver required")
	}
	return &connection{base: b, scope: c.scope}, nil
}

type connection struct {
	base  driver.Conn
	scope string
	inTx  bool
}

func (c *connection) Close() error { return c.base.Close() }
func (c *connection) Prepare(q string) (driver.Stmt, error) {
	return c.PrepareContext(context.Background(), q)
}
func (c *connection) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	if e := checkStatement(q); e != nil {
		return nil, e
	}
	var s driver.Stmt
	var e error
	if p, ok := c.base.(driver.ConnPrepareContext); ok {
		s, e = p.PrepareContext(ctx, q)
	} else {
		s, e = c.base.Prepare(q)
	}
	if e != nil {
		return nil, e
	}
	return &statement{base: s, conn: c}, nil
}
func (c *connection) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}
func (c *connection) BeginTx(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	if c.inTx {
		return nil, errors.New("nested SQL transaction")
	}
	t, e := c.begin(ctx, o)
	if e != nil {
		return nil, e
	}
	c.inTx = true
	return &transaction{base: t, conn: c}, nil
}
func (c *connection) begin(ctx context.Context, o driver.TxOptions) (driver.Tx, error) {
	t, e := c.base.(driver.ConnBeginTx).BeginTx(ctx, o)
	if e != nil {
		return nil, e
	}
	// MySQL itself forbids writes in a read-only transaction, so those remain
	// available during maintenance without taking a locking read.
	if !o.ReadOnly {
		// scope is restricted to scopePattern at construction; no quote, space,
		// slash or escape can occur. Avoid the driver's parameter fast-path
		// ErrSkip here, because this call deliberately bypasses database/sql.
		r, e := c.base.(driver.QueryerContext).QueryContext(ctx, "SELECT phase FROM system_backup_gate WHERE scope='"+c.scope+"' LOCK IN SHARE MODE", nil)
		if e != nil {
			t.Rollback()
			return nil, e
		}
		row := make([]driver.Value, 1)
		e = r.Next(row)
		closeErr := r.Close()
		if e != nil || closeErr != nil {
			t.Rollback()
			return nil, ErrMaintenance
		}
		phase := ""
		switch v := row[0].(type) {
		case []byte:
			phase = string(v)
		case string:
			phase = v
		}
		if phase != "idle" {
			t.Rollback()
			return nil, ErrMaintenance
		}
	}
	return t, nil
}
func (c *connection) exec(ctx context.Context, fn func() (driver.Result, error)) (driver.Result, error) {
	if c.inTx {
		return fn()
	}
	t, e := c.begin(ctx, driver.TxOptions{})
	if e != nil {
		return nil, e
	}
	r, e := fn()
	if e != nil {
		t.Rollback()
		return nil, e
	}
	if e = t.Commit(); e != nil {
		return nil, e
	}
	return r, nil
}
func (c *connection) query(ctx context.Context, fn func() (driver.Rows, error)) (driver.Rows, error) {
	if c.inTx {
		return fn()
	}
	// Autocommit query APIs are made genuinely read-only. Authentication and
	// status reads remain available while the gate is held; using Query for a
	// mutating statement cannot bypass the database's read-only transaction.
	t, e := c.begin(ctx, driver.TxOptions{ReadOnly: true})
	if e != nil {
		return nil, e
	}
	r, e := fn()
	if e != nil {
		t.Rollback()
		return nil, e
	}
	return &rows{Rows: r, tx: t}, nil
}
func (c *connection) ExecContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Result, error) {
	if e := checkStatement(q); e != nil {
		return nil, e
	}
	if len(a) > 0 {
		return nil, driver.ErrSkip
	}
	return c.exec(ctx, func() (driver.Result, error) { return c.base.(driver.ExecerContext).ExecContext(ctx, q, a) })
}
func (c *connection) QueryContext(ctx context.Context, q string, a []driver.NamedValue) (driver.Rows, error) {
	if e := checkStatement(q); e != nil {
		return nil, e
	}
	if len(a) > 0 {
		return nil, driver.ErrSkip
	}
	return c.query(ctx, func() (driver.Rows, error) { return c.base.(driver.QueryerContext).QueryContext(ctx, q, a) })
}

// DDL, explicit transaction control and SET autocommit can implicitly commit
// MySQL's transaction and release the gate before the operation finishes.
// Gated application pools therefore accept only data statements; migrations
// belong to a separate offline identity. Multistatements must remain disabled.
func checkStatement(q string) error {
	q = strings.TrimSpace(q)
	for strings.HasPrefix(q, "/*") {
		if strings.HasPrefix(q, "/*!") {
			return errors.New("unsupported gated SQL statement")
		}
		end := strings.Index(q, "*/")
		if end < 0 {
			return errors.New("unsupported gated SQL statement")
		}
		q = strings.TrimSpace(q[end+2:])
	}
	fields := strings.Fields(q)
	if len(fields) == 0 {
		return errors.New("unsupported gated SQL statement")
	}
	switch strings.ToUpper(fields[0]) {
	case "SELECT", "INSERT", "UPDATE", "DELETE", "REPLACE", "WITH", "SHOW", "DESCRIBE", "EXPLAIN":
		return nil
	case "SET":
		if strings.EqualFold(strings.TrimSuffix(q, ";"), "SET NAMES utf8mb4 COLLATE utf8mb4_unicode_ci") {
			return nil
		}
	}
	return errors.New("unsupported gated SQL statement")
}
func (c *connection) Ping(ctx context.Context) error {
	if p, ok := c.base.(driver.Pinger); ok {
		return p.Ping(ctx)
	}
	return nil
}
func (c *connection) ResetSession(ctx context.Context) error {
	if c.inTx {
		return driver.ErrBadConn
	}
	if p, ok := c.base.(driver.SessionResetter); ok {
		return p.ResetSession(ctx)
	}
	return nil
}
func (c *connection) IsValid() bool {
	if c.inTx {
		return false
	}
	if p, ok := c.base.(driver.Validator); ok {
		return p.IsValid()
	}
	return true
}
func (c *connection) CheckNamedValue(v *driver.NamedValue) error {
	if p, ok := c.base.(driver.NamedValueChecker); ok {
		return p.CheckNamedValue(v)
	}
	return driver.ErrSkip
}

type transaction struct {
	base driver.Tx
	conn *connection
}

func (t *transaction) Commit() error { defer func() { t.conn.inTx = false }(); return t.base.Commit() }
func (t *transaction) Rollback() error {
	defer func() { t.conn.inTx = false }()
	return t.base.Rollback()
}

type statement struct {
	base driver.Stmt
	conn *connection
}

func (s *statement) Close() error  { return s.base.Close() }
func (s *statement) NumInput() int { return s.base.NumInput() }
func (s *statement) Exec(a []driver.Value) (driver.Result, error) {
	return s.conn.exec(context.Background(), func() (driver.Result, error) { return s.base.Exec(a) })
}
func (s *statement) Query(a []driver.Value) (driver.Rows, error) {
	return s.conn.query(context.Background(), func() (driver.Rows, error) { return s.base.Query(a) })
}
func (s *statement) ExecContext(ctx context.Context, a []driver.NamedValue) (driver.Result, error) {
	return s.conn.exec(ctx, func() (driver.Result, error) {
		if p, ok := s.base.(driver.StmtExecContext); ok {
			return p.ExecContext(ctx, a)
		}
		return nil, driver.ErrSkip
	})
}
func (s *statement) QueryContext(ctx context.Context, a []driver.NamedValue) (driver.Rows, error) {
	return s.conn.query(ctx, func() (driver.Rows, error) {
		if p, ok := s.base.(driver.StmtQueryContext); ok {
			return p.QueryContext(ctx, a)
		}
		return nil, driver.ErrSkip
	})
}

type rows struct {
	driver.Rows
	tx       driver.Tx
	failed   bool
	once     sync.Once
	closeErr error
}

func (r *rows) Next(v []driver.Value) error {
	e := r.Rows.Next(v)
	if e != nil && e != io.EOF {
		r.failed = true
	}
	return e
}
func (r *rows) Close() error {
	r.once.Do(func() {
		r.closeErr = r.Rows.Close()
		if r.failed || r.closeErr != nil {
			r.tx.Rollback()
		} else {
			r.closeErr = r.tx.Commit()
		}
	})
	return r.closeErr
}

// Preserve metadata used by scanners without exposing the underlying connection.
func (r *rows) ColumnTypeDatabaseTypeName(i int) string {
	if p, ok := r.Rows.(driver.RowsColumnTypeDatabaseTypeName); ok {
		return p.ColumnTypeDatabaseTypeName(i)
	}
	return ""
}
func (r *rows) ColumnTypeLength(i int) (int64, bool) {
	if p, ok := r.Rows.(driver.RowsColumnTypeLength); ok {
		return p.ColumnTypeLength(i)
	}
	return 0, false
}
func (r *rows) ColumnTypeNullable(i int) (bool, bool) {
	if p, ok := r.Rows.(driver.RowsColumnTypeNullable); ok {
		return p.ColumnTypeNullable(i)
	}
	return false, false
}
func (r *rows) ColumnTypePrecisionScale(i int) (int64, int64, bool) {
	if p, ok := r.Rows.(driver.RowsColumnTypePrecisionScale); ok {
		return p.ColumnTypePrecisionScale(i)
	}
	return 0, 0, false
}

func (r *rows) ColumnTypeScanType(i int) reflect.Type {
	if p, ok := r.Rows.(driver.RowsColumnTypeScanType); ok {
		return p.ColumnTypeScanType(i)
	}
	return reflect.TypeOf((*any)(nil)).Elem()
}
