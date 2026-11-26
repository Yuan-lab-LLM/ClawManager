package records

import (
	"context"
	"database/sql"
	"reflect"
	"strings"
)

type Placement struct{ Namespace, StorageClass, PVC string }
type RestorePlan struct {
	Data             Snapshot
	Users, Instances int
	trialSeeds       map[string][32]byte
}

type DrillTarget struct {
	Database, ServerUUID string
	CheckIsolation       func(context.Context) error
}

func (t DrillTarget) check(ctx context.Context, conn *sql.Conn) error {
	if !identifier.MatchString(t.Database) || !strings.HasPrefix(t.Database, "cm_drill_") || t.ServerUUID == "" || t.CheckIsolation == nil {
		return ErrIsolation
	}
	var database, uuid string
	if conn.QueryRowContext(ctx, "SELECT DATABASE(), @@server_uuid").Scan(&database, &uuid) != nil {
		return ErrDatabase
	}
	if database != t.Database || uuid != t.ServerUUID {
		return ErrIsolation
	}
	if t.CheckIsolation(ctx) != nil {
		return ErrIsolation
	}
	return nil
}

func canonicalizeTargetJSON(ctx context.Context, tx *sql.Tx, plan *RestorePlan) error {
	for _, table := range plan.Data.Tables {
		for _, column := range table.Columns {
			if !strings.EqualFold(column.Type, "json") {
				continue
			}
			for _, row := range table.Rows {
				value := row[column.Name]
				if value.Null {
					continue
				}
				var rendered string
				if err := tx.QueryRowContext(ctx, "SELECT CAST(? AS JSON)", string(value.Bytes)).Scan(&rendered); err != nil {
					return ErrDatabase
				}
				row[column.Name] = Text(rendered)
			}
		}
	}
	return nil
}

func checkDrillExecutableSchema(ctx context.Context, tx *sql.Tx) error {
	// Imported code must not activate triggers/events or write through views.
	var executable int
	if tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM information_schema.TRIGGERS WHERE TRIGGER_SCHEMA = DATABASE()) + (SELECT COUNT(*) FROM information_schema.EVENTS WHERE EVENT_SCHEMA = DATABASE()) + (SELECT COUNT(*) FROM information_schema.VIEWS WHERE TABLE_SCHEMA = DATABASE())`).Scan(&executable) != nil {
		return ErrDatabase
	}
	if executable != 0 {
		return ErrSchema
	}
	return nil
}

func checkBlankWithSeeds(ctx context.Context, tx *sql.Tx, tables []Table, trialSeeds map[string][32]byte) error {
	if err := checkDrillExecutableSchema(ctx, tx); err != nil {
		return err
	}
	var admins int
	if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `users` WHERE username = 'admin' AND role = 'admin'").Scan(&admins) != nil {
		return ErrDatabase
	}
	if admins > 1 {
		return ErrNotEmpty
	}
	for _, t := range tables {
		if t.Name == "schema_migrations" {
			continue
		}
		if _, baseline := trialSeeds[t.Name]; baseline {
			continue
		}
		if t.Engine != "InnoDB" {
			return ErrSchema
		}
		name, _ := quoted(t.Name)
		var count int
		if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name).Scan(&count) != nil {
			return ErrDatabase
		}
		if t.Name == "users" {
			if count != admins {
				return ErrNotEmpty
			}
			continue
		}
		if t.Name == "user_quotas" {
			var invalid int
			if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM `user_quotas` q LEFT JOIN `users` u ON q.user_id = u.id WHERE u.id IS NULL OR u.username <> 'admin' OR u.role <> 'admin'").Scan(&invalid) != nil {
				return ErrDatabase
			}
			if invalid > 0 || count > 1 {
				return ErrNotEmpty
			}
			continue
		}
		if count != 0 {
			return ErrNotEmpty
		}
	}
	return nil
}

func VerifyDrill(ctx context.Context, db *sql.DB, target DrillTarget, plan RestorePlan) error {
	if db == nil {
		return ErrDatabase
	}
	conn, err := db.Conn(ctx)
	if err != nil {
		return ErrDatabase
	}
	defer conn.Close()
	if err = target.check(ctx, conn); err != nil {
		return err
	}
	if err = checkSelectOnly(ctx, conn, target.Database); err != nil {
		return err
	}
	return verifyRows(ctx, conn, plan)
}

func checkSelectOnly(ctx context.Context, conn *sql.Conn, database string) error {
	grantDatabase, err := QuotedGrantDatabase(ctx, conn, database)
	if err != nil {
		return err
	}
	grants, err := conn.QueryContext(ctx, "SHOW GRANTS FOR CURRENT_USER")
	if err != nil {
		return ErrDatabase
	}
	found := false
	for grants.Next() {
		var g string
		if grants.Scan(&g) != nil {
			grants.Close()
			return ErrDatabase
		}
		if strings.Contains(g, " WITH GRANT OPTION") {
			grants.Close()
			return ErrIsolation
		}
		if strings.HasPrefix(g, "GRANT USAGE ON *.* TO ") {
			continue
		}
		if !strings.HasPrefix(g, "GRANT SELECT ON "+grantDatabase+".* TO ") {
			grants.Close()
			return ErrIsolation
		}
		found = true
	}
	err = grants.Err()
	grants.Close()
	if err != nil {
		return ErrDatabase
	}
	if !found {
		return ErrIsolation
	}
	return nil
}

func verifyRows(ctx context.Context, conn *sql.Conn, plan RestorePlan) error {
	tx, err := conn.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if err != nil {
		return ErrDatabase
	}
	defer tx.Rollback()
	return verifyRowsInTransaction(ctx, tx, plan)
}

func verifyRowsInTransaction(ctx context.Context, tx *sql.Tx, plan RestorePlan) error {
	expected := map[string]int{}
	for _, t := range plan.Data.Tables {
		expected[t.Name] = len(t.Rows)
		name, e := quoted(t.Name)
		if e != nil {
			return e
		}
		var cols, pk []string
		for _, c := range t.Columns {
			n, e := quoted(c.Name)
			if e != nil {
				return e
			}
			cols = append(cols, n)
		}
		for _, c := range t.PrimaryKey {
			n, e := quoted(c)
			if e != nil {
				return e
			}
			pk = append(pk, n)
		}
		if len(cols) == 0 || len(pk) == 0 {
			return ErrSchema
		}
		rows, err := tx.QueryContext(ctx, "SELECT "+strings.Join(cols, ",")+" FROM "+name+" ORDER BY "+strings.Join(pk, ","))
		if err != nil {
			return ErrDatabase
		}
		index := 0
		for rows.Next() {
			if index >= len(t.Rows) {
				rows.Close()
				return ErrData
			}
			values := make([]any, len(cols))
			pointers := make([]any, len(cols))
			for i := range values {
				pointers[i] = &values[i]
			}
			if rows.Scan(pointers...) != nil {
				rows.Close()
				return ErrDatabase
			}
			for i, v := range values {
				c, e := rawCell(v)
				want := t.Rows[index][t.Columns[i].Name]
				if e != nil || c.Null != want.Null || !reflect.DeepEqual(string(c.Bytes), string(want.Bytes)) {
					rows.Close()
					return ErrData
				}
			}
			index++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return ErrDatabase
		}
		if index != len(t.Rows) {
			return ErrData
		}
	}
	// A row in an omitted/replayable/control table also invalidates the drill.
	catalog, err := Catalog(ctx, tx)
	if err != nil {
		return err
	}
	checkedTrialSeeds := 0
	for _, t := range catalog {
		if t.Name == "schema_migrations" {
			continue
		}
		if want, baseline := plan.trialSeeds[t.Name]; baseline {
			got, err := trialSeedDigest(ctx, tx, t)
			if err != nil {
				return err
			}
			if got != want {
				return ErrData
			}
			checkedTrialSeeds++
			continue
		}
		name, _ := quoted(t.Name)
		var n int
		if tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+name).Scan(&n) != nil {
			return ErrDatabase
		}
		if n != expected[t.Name] {
			return ErrData
		}
	}
	if checkedTrialSeeds != len(plan.trialSeeds) {
		return ErrData
	}
	if tx.Commit() != nil {
		return ErrDatabase
	}
	return nil
}
