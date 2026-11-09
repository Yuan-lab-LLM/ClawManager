package systembackup

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// RequireReadOnlyVerifier accepts the deliberately narrow P0 identity: USAGE
// plus SELECT on the exact restore database, no roles or grant option. Merely
// using a different username does not establish an independent read-only role.
func RequireReadOnlyVerifier(ctx context.Context, db *sql.DB, database string) error {
	if db == nil || !restoreDatabasePattern.MatchString(database) {
		return errors.New("isolation_invalid")
	}
	var selected, roles string
	if e := db.QueryRowContext(ctx, "SELECT DATABASE(),CURRENT_ROLE()").Scan(&selected, &roles); e != nil {
		return errors.New("isolation_invalid")
	}
	if selected != database || roles != "NONE" {
		return errors.New("isolation_invalid")
	}
	rows, e := db.QueryContext(ctx, "SHOW GRANTS FOR CURRENT_USER")
	if e != nil {
		return errors.New("isolation_invalid")
	}
	defer rows.Close()
	selectPrefix := "GRANT SELECT ON `" + database + "`.* TO "
	count, readGrant := 0, false
	for rows.Next() {
		var grant string
		if e = rows.Scan(&grant); e != nil {
			return errors.New("isolation_invalid")
		}
		count++
		if count > 8 || len(grant) > 16384 || strings.Contains(grant, "WITH GRANT OPTION") {
			return errors.New("isolation_invalid")
		}
		if strings.HasPrefix(grant, selectPrefix) {
			readGrant = true
			continue
		}
		if !strings.HasPrefix(grant, "GRANT USAGE ON *.* TO ") {
			return errors.New("isolation_invalid")
		}
	}
	if rows.Err() != nil || !readGrant {
		return errors.New("isolation_invalid")
	}
	return nil
}
