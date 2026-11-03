package systembackup

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func receiptFixture(t *testing.T) (DataOperationBinding, DataRestoreReport) {
	t.Helper()
	at := time.Now().UTC()
	b := DataOperationBinding{Operation: "restore", RunID: "sdr_" + strings.Repeat("a", 32), AttemptID: "sat_" + strings.Repeat("b", 32), TargetUID: "owned-target-uid", Origin: "installation", BackupID: "sbk_" + strings.Repeat("c", 32), CommitSHA256: strings.Repeat("d", 64)}
	r := DataRestoreReport{RunID: b.RunID, BackupID: b.BackupID, OriginInstallationID: b.Origin, Status: "succeeded", Phase: "complete", StartedAt: at, FinishedAt: &at, Team: TeamRecoverySummary{SQLReadbackVerified: true}, SQL: SQLResult{Tables: map[string]int64{}, Hashes: map[string]string{}}, FileCount: 4, VerifiedFileCount: 4, VerifiedObjectCount: 1, VerifiedRedisCount: 1}
	for _, p := range Registry() {
		if p.BackupStrategy == "mysql_dump" {
			r.SQL.Tables[p.Name] = 1
			r.SQL.Hashes[p.Name] = strings.Repeat("e", 64)
		}
	}
	for _, name := range []string{"deployment_resources", "secret_bundle"} {
		r.HistoryParts = append(r.HistoryParts, ArtifactPart{Name: name, CiphertextBytes: 1000, CiphertextSHA256: strings.Repeat("e", 64), PlaintextBytes: 900, PlaintextSHA256: strings.Repeat("f", 64), Key: WrappedKey{Reference: SecretRef{Provider: "kubernetes", Name: "history-key", Version: "v1", Purpose: "encryption"}, Nonce: make([]byte, 12), Ciphertext: make([]byte, 48)}})
	}
	return b, r
}

func TestDataRestoreReceiptFitsJobTerminationAndRetainsVerificationIntent(t *testing.T) {
	b, r := receiptFixture(t)
	full, _ := json.Marshal(r)
	if len(full) <= 4096 {
		t.Fatal("fixture must reproduce full report overflow", len(full))
	}
	got, err := RestoreReceipt(b, r)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(got)
	if len(raw)+1 > 4096 || got.SQLTables != len(r.SQL.Tables) || got.SQLRows != int64(len(r.SQL.Tables)) || !validHash(got.SQLSHA256) || !got.IntegrityVerified || got.AcceptanceEligible || len(got.HistoryParts) != 2 || !got.NormalizedAt.Equal(r.StartedAt) {
		t.Fatal("incomplete bounded result", len(raw), got)
	}
	if strings.Contains(string(raw), "team_tasks") {
		t.Fatal("per-table report leaked into bounded receipt")
	}
	b.Operation = "verify-restored"
	verified := RestoredDataVerification{RunID: b.RunID, BackupID: b.BackupID, SQL: r.SQL, Team: r.Team, FileCount: 4, ObjectCount: 1, RedisCount: 1, HistoryPartsVerified: 2, IntegrityVerified: true}
	intent := RestoreVerificationIntent{RunID: b.RunID, NormalizedAt: r.StartedAt, HistoryParts: r.HistoryParts}
	b.IntentSHA256, _ = RequestHash(intent)
	proof, err := RestoredVerificationReceipt(b, intent, verified)
	if err != nil || proof.SQLSHA256 != got.SQLSHA256 {
		t.Fatal("independent verifier result not comparable", err)
	}
}

func TestRestoreReceiptRejectsIncompleteOrMismatchedEvidence(t *testing.T) {
	for _, mutate := range []func(*DataOperationBinding, *DataRestoreReport){
		func(b *DataOperationBinding, r *DataRestoreReport) { b.RunID = "sdr_" + strings.Repeat("f", 32) },
		func(b *DataOperationBinding, r *DataRestoreReport) { r.Status = "failed" },
		func(b *DataOperationBinding, r *DataRestoreReport) { r.Team.SQLReadbackVerified = false },
		func(b *DataOperationBinding, r *DataRestoreReport) { delete(r.SQL.Tables, "team_tasks") },
		func(b *DataOperationBinding, r *DataRestoreReport) { r.VerifiedFileCount-- },
		func(b *DataOperationBinding, r *DataRestoreReport) { r.HistoryParts[1] = r.HistoryParts[0] },
		func(b *DataOperationBinding, r *DataRestoreReport) {
			r.HistoryParts[0].Key.Ciphertext = make([]byte, 8192)
		},
		func(b *DataOperationBinding, r *DataRestoreReport) { r.AcceptanceEligible = true },
	} {
		b, r := receiptFixture(t)
		mutate(&b, &r)
		if _, err := RestoreReceipt(b, r); err == nil {
			t.Fatal("incomplete receipt accepted")
		}
	}
}
