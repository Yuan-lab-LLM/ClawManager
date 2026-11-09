package systembackup

import (
	"context"
	"errors"
)

// VerificationReport exposes only the allowlisted completed integrity result.
// Frozen Job requests, credential references and raw logs never cross this API.
func (s Store) VerificationReport(ctx context.Context, id string) (IntegrityReport, error) {
	var zero IntegrityReport
	t, e := s.Get(ctx, id)
	if e != nil {
		return zero, e
	}
	if t.Type != ArtifactVerify || t.Status != "succeeded" {
		return zero, errors.New("not_ready")
	}
	a, e := s.GetAttempt(ctx, id, "verify")
	if e != nil {
		return zero, e
	}
	var result VerifyJobResult
	if a.Status != "succeeded" || strictDecode(a.Result, &result) != nil || result.State != "succeeded" || result.Report == nil {
		return zero, errors.New("evidence_incomplete")
	}
	r := *result.Report
	if r.BackupID != t.SourceID || r.Origin != s.InstallationID || !r.IntegrityVerified || r.AcceptanceEligible || r.PartsVerified != len(RequiredParts) {
		return zero, errors.New("evidence_incomplete")
	}
	return r, nil
}
