package systembackup

import (
	"encoding/json"
	"errors"
	"time"
)

// DataOperationBinding is frozen by the controller in the original Job request.
// It binds the small process result to that execution and immutable artifact;
// it does not grant acceptance or prove credential revocation.
type DataOperationBinding struct {
	Operation    string `json:"operation"`
	RunID        string `json:"run_id"`
	AttemptID    string `json:"attempt_id"`
	TargetUID    string `json:"target_uid"`
	Origin       string `json:"origin_installation_id"`
	BackupID     string `json:"backup_id"`
	CommitSHA256 string `json:"commit_sha256"`
	IntentSHA256 string `json:"intent_sha256,omitempty"`
}

func (b DataOperationBinding) Validate() error {
	if b.Operation == "restore" && b.IntentSHA256 != "" || b.Operation == "verify-restored" && !validHash(b.IntentSHA256) {
		return errors.New("validation_failed")
	}
	if (b.Operation != "restore" && b.Operation != "verify-restored") || !publicIDPattern.MatchString(b.RunID) || len(b.RunID) != 36 || b.RunID[:4] != "sdr_" || !attemptIDPattern.MatchString(b.AttemptID) || !refPattern.MatchString(b.TargetUID) || !refPattern.MatchString(b.Origin) || !publicIDPattern.MatchString(b.BackupID) || b.BackupID[:4] != "sbk_" || !validHash(b.CommitSHA256) {
		return errors.New("validation_failed")
	}
	return nil
}

// DataOperationReceipt deliberately omits unbounded per-table/state details.
// Those remain in the complete target report and encrypted original archive.
type DataOperationReceipt struct {
	Schema string `json:"schema"`
	DataOperationBinding
	Status             string         `json:"status"`
	NormalizedAt       time.Time      `json:"normalized_at"`
	SQLTables          int            `json:"sql_tables"`
	SQLRows            int64          `json:"sql_rows"`
	SQLSHA256          string         `json:"sql_sha256"`
	FileCount          int            `json:"file_count"`
	ObjectCount        int            `json:"object_count"`
	RedisCount         int            `json:"redis_count"`
	HistoryParts       []ArtifactPart `json:"history_parts"`
	IntegrityVerified  bool           `json:"integrity_verified"`
	AcceptanceEligible bool           `json:"acceptance_eligible"`
}

func dataReceipt(b DataOperationBinding, sql SQLResult, at time.Time, files, objects, redis int, history []ArtifactPart) (DataOperationReceipt, error) {
	r := DataOperationReceipt{Schema: "system-backup-data-operation.v1", DataOperationBinding: b, Status: "succeeded", NormalizedAt: at, SQLTables: len(sql.Tables), FileCount: files, ObjectCount: objects, RedisCount: redis, HistoryParts: history, IntegrityVerified: true}
	if b.Validate() != nil || at.IsZero() || files < 0 || objects < 0 || redis < 0 || requireBusinessSQLInventory(sql) != nil || len(history) != 2 {
		return r, errors.New("evidence_incomplete")
	}
	for _, n := range sql.Tables {
		if n < 0 {
			return r, errors.New("evidence_incomplete")
		}
		r.SQLRows += n
		if r.SQLRows < 0 {
			return r, errors.New("evidence_incomplete")
		}
	}
	if len(sql.Hashes) != len(sql.Tables) {
		return r, errors.New("evidence_incomplete")
	}
	for name := range sql.Tables {
		if !validHash(sql.Hashes[name]) {
			return r, errors.New("evidence_incomplete")
		}
	}
	var err error
	r.SQLSHA256, err = RequestHash(sql)
	if err != nil {
		return r, err
	}
	seen := map[string]bool{}
	for _, part := range history {
		if (part.Name != "deployment_resources" && part.Name != "secret_bundle") || seen[part.Name] || part.CiphertextBytes <= 0 || part.PlaintextBytes <= 0 || !validHash(part.CiphertextSHA256) || !validHash(part.PlaintextSHA256) {
			return r, errors.New("evidence_incomplete")
		}
		seen[part.Name] = true
	}
	raw, err := json.Marshal(r)
	if err != nil || len(raw)+1 > 4096 {
		return r, errors.New("evidence_incomplete")
	}
	return r, nil
}

func RestoreReceipt(b DataOperationBinding, r DataRestoreReport) (DataOperationReceipt, error) {
	if b.Operation != "restore" || r.RunID != b.RunID || r.BackupID != b.BackupID || r.OriginInstallationID != b.Origin || r.Status != "succeeded" || r.Phase != "complete" || r.FinishedAt == nil || !r.Team.SQLReadbackVerified || r.AcceptanceEligible || r.FileCount != r.VerifiedFileCount {
		return DataOperationReceipt{}, errors.New("evidence_incomplete")
	}
	return dataReceipt(b, r.SQL, r.StartedAt, r.VerifiedFileCount, r.VerifiedObjectCount, r.VerifiedRedisCount, r.HistoryParts)
}

func RestoredVerificationReceipt(b DataOperationBinding, intent RestoreVerificationIntent, r RestoredDataVerification) (DataOperationReceipt, error) {
	intentHash, err := RequestHash(intent)
	if err != nil || intentHash != b.IntentSHA256 || b.Operation != "verify-restored" || r.RunID != b.RunID || intent.RunID != b.RunID || r.BackupID != b.BackupID || !r.IntegrityVerified || !r.Team.SQLReadbackVerified || r.AcceptanceEligible || r.HistoryPartsVerified != 2 {
		return DataOperationReceipt{}, errors.New("evidence_incomplete")
	}
	return dataReceipt(b, r.SQL, intent.NormalizedAt, r.FileCount, r.ObjectCount, r.RedisCount, intent.HistoryParts)
}

func (r DataOperationReceipt) validateBinding(b DataOperationBinding) error {
	if b.Validate() != nil || r.Schema != "system-backup-data-operation.v1" || r.DataOperationBinding != b || r.Status != "succeeded" || !r.IntegrityVerified || r.AcceptanceEligible || r.NormalizedAt.IsZero() || r.SQLTables != businessTableCount() || r.SQLRows < 0 || !validHash(r.SQLSHA256) || r.FileCount < 0 || r.ObjectCount < 0 || r.RedisCount < 0 || len(r.HistoryParts) != 2 {
		return errors.New("evidence_incomplete")
	}
	seen := map[string]bool{}
	for _, h := range r.HistoryParts {
		if (h.Name != "deployment_resources" && h.Name != "secret_bundle") || seen[h.Name] || h.CiphertextBytes <= 0 || h.PlaintextBytes <= 0 || !validHash(h.CiphertextSHA256) || !validHash(h.PlaintextSHA256) || h.Key.Reference.Validate("encryption") != nil || len(h.Key.Nonce) != 12 || len(h.Key.Ciphertext) != 48 {
			return errors.New("evidence_incomplete")
		}
		seen[h.Name] = true
	}
	if b.Operation == "verify-restored" {
		hash, e := RequestHash(RestoreVerificationIntent{RunID: b.RunID, NormalizedAt: r.NormalizedAt, HistoryParts: r.HistoryParts})
		if e != nil || hash != b.IntentSHA256 {
			return errors.New("evidence_incomplete")
		}
	}
	return nil
}
