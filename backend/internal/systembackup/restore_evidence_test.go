package systembackup

import (
	"bytes"
	"clawreef/internal/systembackup/team"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestRestoreEvidenceRetainsFirstStateAndQuarantine(t *testing.T) {
	ctx := context.Background()
	s := Store{controlDB(t), "control-installation"}
	run, _ := RandomID("sdr_")
	source, _ := RandomID("sbk_")
	target := strings.Repeat("a", 64)
	schema := SQLTable{Name: "team_tasks", PrimaryKey: []string{"id"}, Columns: []SQLColumn{{Name: "id"}, {Name: "status"}, {Name: "result_json"}}}
	row := team.Row{"id": team.Value("7"), "status": team.Value("running"), "result_json": team.Value(`{"partial":"private-value"}`)}
	change, e := team.Normalize(schema.Name, row, time.Now().UTC())
	if e != nil {
		t.Fatal(e)
	}
	wrapper := AESKeyWrapper{SecretRef{"kubernetes", "evidence-key", "v1", "encryption"}, bytes.Repeat([]byte{9}, 32)}
	first, e := s.RecordRestoreEvidence(ctx, run, source, target, schema, change, wrapper)
	if e != nil {
		t.Fatal(e)
	}
	// Evidence identities are a separate historical format. Upgrading API
	// request canonicalization must not orphan the original-state journal.
	legacyBytes, _ := json.Marshal(change.Before)
	legacyDigest := sha256.Sum256(legacyBytes)
	if first.SourceHash != hex.EncodeToString(legacyDigest[:]) {
		t.Fatal("canonical request hashing changed historical evidence identity")
	}
	second, e := s.RecordRestoreEvidence(ctx, run, source, target, schema, change, wrapper)
	if e != nil || first.SourceHash != second.SourceHash {
		t.Fatal(first, second, e)
	}
	if found, e := s.IsQuarantined(ctx, target, schema.Name, first.EntityKeyHash); e != nil || !found {
		t.Fatal(found, e)
	}
	var payload []byte
	var count int
	if e = s.DB.QueryRow("SELECT encrypted_payload FROM system_backup_restore_evidence WHERE run_id=?", run).Scan(&payload); e != nil {
		t.Fatal(e)
	}
	if bytes.Contains(payload, []byte("private-value")) || bytes.Contains(payload, []byte("running")) {
		t.Fatal("plaintext evidence persisted")
	}
	if e = s.DB.QueryRow("SELECT COUNT(*) FROM system_backup_restore_evidence WHERE run_id=?", run).Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
	changed := change
	changed.Before = team.Clone(change.Before)
	changed.Before["status"] = team.Value("waiting")
	if _, e = s.RecordRestoreEvidence(ctx, run, source, target, schema, changed, wrapper); e == nil {
		t.Fatal("overwrote original state")
	}
	if _, e = s.RecordRestoreEvidence(ctx, run, source, strings.Repeat("b", 64), schema, change, wrapper); e == nil {
		t.Fatal("changed target identity")
	}
}
