package ownerrecords

import (
	"fmt"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"testing"
)

func TestSyncDatabaseDiagnosticNeverExposesMessage(t *testing.T) {
	for _, code := range []uint16{1052, 1213, 3024} {
		err := fmt.Errorf("private query context: %w", &mysqlDriver.MySQLError{Number: code, Message: "synthetic-password-or-email"})
		want := fmt.Sprintf("mysql_%d", code)
		if got := syncReason(err); got != want || safeSyncReason(got) != want {
			t.Fatal("database error lost safe code", got)
		}
	}
	for _, raw := range []string{"mysql_1052 password=private", "mysql_abc1", "mysql_0001"} {
		if safeSyncReason(raw) != "read_or_store" {
			t.Fatal("untrusted diagnostic allowed", raw)
		}
	}
}
