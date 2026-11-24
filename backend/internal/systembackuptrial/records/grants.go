package records

import (
	"context"
	"database/sql"
	"regexp"
	"strings"
)

// QuotedGrantDatabase differs from ordinary identifier quoting: MySQL schema
// grants interpret underscores as wildcards when partial_revokes is OFF, even
// inside backticks. Never broaden cm_drill_x to other matching database names.
func QuotedGrantDatabase(ctx context.Context, conn *sql.Conn, database string) (string, error) {
	if conn == nil || !identifier.MatchString(database) {
		return "", ErrSchema
	}
	pattern := database
	if strings.Contains(database, "_") {
		var literal bool
		if conn.QueryRowContext(ctx, "SELECT @@GLOBAL.partial_revokes").Scan(&literal) != nil {
			return "", ErrIsolation
		}
		if !literal {
			pattern = strings.ReplaceAll(database, "_", `\_`)
		}
	}
	return "`" + pattern + "`", nil
}

var businessTableGrant = regexp.MustCompile("^GRANT SELECT ON `([A-Za-z0-9_]+)`\\.`([A-Za-z0-9_]+)` TO (?:'[^']+'|`[^`]+`)@(?:'[^']+'|`[^`]+`)$")
