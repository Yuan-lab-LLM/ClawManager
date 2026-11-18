package systembackup

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"testing"
)

func TestSystemRestoreRejectsValidButIncompleteSQLSnapshot(t *testing.T) {
	empty := sha256.Sum256(nil)
	snapshot := func(omit string) []byte {
		var b bytes.Buffer
		e := json.NewEncoder(&b)
		e.Encode(SQLRecord{Kind: "mysql_rows_v1"})
		for _, policy := range Registry() {
			if policy.BackupStrategy != "mysql_dump" || policy.Name == omit {
				continue
			}
			e.Encode(SQLRecord{Kind: "table", Table: &SQLTable{Name: policy.Name, Columns: []SQLColumn{{Name: "id", Type: "int", Nullable: "NO"}}, PrimaryKey: []string{"id"}}})
			e.Encode(SQLRecord{Kind: "end_table", Count: 0, SHA256: hex.EncodeToString(empty[:])})
		}
		e.Encode(SQLRecord{Kind: "end"})
		return b.Bytes()
	}
	partial := snapshot("team_work_items")
	if _, e := ReadSQLSnapshot(bytes.NewReader(partial), nil, nil); e != nil {
		t.Fatal("fixture is not a valid sealed SQL stream", e)
	}
	if e := ValidateBusinessSQLSnapshot(bytes.NewReader(partial)); e == nil {
		t.Fatal("missing Team table accepted despite valid per-table checksums")
	}
	if e := ValidateBusinessSQLSnapshot(bytes.NewReader(snapshot(""))); e != nil {
		t.Fatal("empty registered tables incorrectly rejected", e)
	}
}
