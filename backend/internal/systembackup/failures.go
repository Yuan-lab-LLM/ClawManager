package systembackup

// Frozen from the v12 reference, section 6. API-only conflict/authentication
// codes must never become terminal task failure categories.
var FailureCategories = []string{
	"none", "canceled_by_actor", "canceled_by_shutdown", "validation_failed", "permission_denied", "not_found", "not_ready", "conflict_active_job", "unsupported_runner_p1", "unsupported_profile_p1", "unsupported_datasource_p1", "unsupported_schema", "backup_target_risk_rejected", "isolation_invalid", "writer_registry_drift", "quiesce_timeout", "quiesce_resume_failed", "capacity_exceeded", "deadline_exceeded", "preflight_failed", "normalization_failed", "control_evidence_invalid", "artifact_unavailable", "artifact_conflict", "checksum_mismatch", "signature_invalid", "encryption_key_unavailable", "kek_permission_denied", "signing_key_unavailable", "finalizer_request_unavailable", "catalog_unavailable", "catalog_conflict", "catalog_tombstone_failed", "secret_bundle_failed", "redaction_failed", "audit_failed", "evidence_incomplete", "staging_cleanup_failed", "cleanup_unsafe_target", "confirmation_invalid", "fencing_token_lost", "coordination_invalid", "artifact_delete_failed", "runner_failed", "internal_error",
}
