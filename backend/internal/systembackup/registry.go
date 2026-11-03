package systembackup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
)

const ContractVersion = "system-backup-plan.v12"

var identifierPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

type ObjectPolicy struct {
	Name            string `json:"name"`
	Category        string `json:"category"`
	Classification  string `json:"data_classification"`
	BackupStrategy  string `json:"backup_strategy"`
	RestoreStrategy string `json:"restore_strategy"`
}

// The registry is deliberately explicit. Prefix-based default capture would
// silently include newly introduced credentials or runnable work on restore.
var businessTables = map[string][]string{
	"users":      {"users", "user_quotas", "enterprise_auth_settings", "iei_session_revocations", "northbound_auth_challenges", "northbound_sessions"},
	"ai_gateway": {"llm_models", "model_invocations", "audit_events", "cost_records", "risk_rules", "risk_hits", "chat_sessions", "chat_messages", "instance_gateway_token_aliases", "gateway_keys", "gateway_key_budget_reservations", "gateway_key_budget_ledger", "routing_classifier_settings", "llm_model_user_grants", "gateway_image_artifacts", "gateway_image_jobs"},
	"instances":  {"instances", "persistent_volumes", "backups", "backup_schedules", "instance_usage", "instance_agents", "instance_browser_workers", "instance_runtime_status", "instance_desired_state", "instance_commands", "instance_config_revisions", "instance_runtime_bindings", "instance_external_access", "runtime_pods", "runtime_rollouts", "runtime_rollout_checkpoints", "runtime_upgrade_items", "runtime_upgrade_audits", "instance_lifecycle_batches", "instance_lifecycle_batch_items", "instance_lifecycle_batch_attempts", "instance_lifecycle_manual_guards", "instance_lifecycle_scheduler", "workspace_file_audits", "openclaw_config_resources", "openclaw_config_bundles", "openclaw_config_bundle_items", "openclaw_config_bundle_skills", "openclaw_injection_snapshots"},
	"teams":      {"teams", "team_members", "team_tasks", "team_events", "team_work_items", "team_event_outbox", "team_workflow_phases", "custom_team_templates", "team_workspace_file_audits", "owner_team_provision_steps", "owner_team_admission_locks"},
	"resources":  {"system_image_settings", "egress_private_exceptions", "security_scan_configs", "security_scan_jobs", "security_scan_job_items", "security_scan_reports", "audit_logs", "northbound_admin_settings", "northbound_caller_policies", "northbound_operations", "northbound_owner_records", "northbound_settings_audit"},
	"skill_hub":  {"skill_blobs", "skills", "skill_versions", "instance_skills", "skill_scan_results", "skill_package_materialize_jobs", "skill_hub_tags", "skill_hub_tag_assignments"},
}

var controlTables = []string{"schema_migrations", "runtime_management_admission_lock", "system_backup_installation_state", "system_backup_controller_leases", "system_backup_attempts", "system_backup_capture_plans", "system_backup_capture_jobs", "system_backup_capture_aborts", "system_backup_job_sends", "system_backup_stage_resources", "system_backup_dependency_health", "system_backup_config_versions", "system_backup_tasks", "system_backup_events", "system_backup_gate", "system_backup_gate_admissions", "system_backup_mutation_leases", "system_backup_participants", "system_backup_restore_evidence", "system_backup_restore_jobs", "system_backup_quarantine", "system_backup_artifacts", "system_backup_resources", "system_backup_external_actions", "system_backup_operations", "system_backup_strong_auth_nonces", "system_backup_catalog_records", "system_backup_promotions"}

func Registry() []ObjectPolicy {
	out := []ObjectPolicy{{"system_team_data_backup_jobs", "system_backup", "internal", "metadata_only", "reference_only"}}
	// These rows reference live processes, generations or outstanding writes.
	// Restoring them must never enroll a copied instance or replay remote work.
	// Runtime management journals and deletion fences also contain live native
	// controller identities; copied work must not run or fence another cluster.
	for _, name := range []string{"gateway_runtime_tasks", "gateway_runtime_task_models", "gateway_runtime_protocols", "gateway_runtime_maintenance", "gateway_runtime_upgrades", "gateway_runtime_upgrade_stops", "instance_workspace_write_intents", "runtime_management_operations", "runtime_pod_deletion_intents"} {
		out = append(out, ObjectPolicy{name, "system_backup", "secret", "metadata_only", "reference_only"})
	}
	for category, names := range businessTables {
		for _, name := range names {
			// All raw database payloads are encrypted. Public reports use separate
			// allowlisted statistics; table payloads are never API response DTOs.
			out = append(out, ObjectPolicy{name, category, "secret", "mysql_dump", "normalize"})
		}
	}
	for _, name := range controlTables {
		out = append(out, ObjectPolicy{name, "system_backup", "internal", "metadata_only", "reference_only"})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

func Policy(name string) (ObjectPolicy, bool) {
	for _, p := range Registry() {
		if p.Name == name {
			return p, true
		}
	}
	return ObjectPolicy{}, false
}
func RegistryHash() string {
	raw, _ := json.Marshal(Registry())
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
func QuoteIdentifier(name string) (string, error) {
	if !identifierPattern.MatchString(name) {
		return "", fmt.Errorf("invalid SQL identifier")
	}
	return "`" + name + "`", nil
}

type Coverage struct {
	Known        []string `json:"known"`
	Unassigned   []string `json:"unassigned"`
	Missing      []string `json:"missing"`
	RegistryHash string   `json:"registry_hash"`
}

func CheckCoverage(actual []string) Coverage {
	r := Coverage{Known: []string{}, Unassigned: []string{}, Missing: []string{}, RegistryHash: RegistryHash()}
	for _, name := range actual {
		if _, ok := Policy(name); ok {
			r.Known = append(r.Known, name)
		} else {
			r.Unassigned = append(r.Unassigned, name)
		}
	}
	for _, p := range Registry() {
		if p.Category != "system_backup" && !slices.Contains(actual, p.Name) {
			r.Missing = append(r.Missing, p.Name)
		}
	}
	sort.Strings(r.Known)
	sort.Strings(r.Unassigned)
	return r
}

// ValidateObjectTypes fails closed on executable schema objects until a typed
// object policy and verifier have been registered. It must not silently dump
// MySQL events, views, routines or triggers as table data.
func ValidateObjectTypes(objects map[string]string) error {
	for name, kind := range objects {
		if strings.ToUpper(kind) != "BASE TABLE" {
			return fmt.Errorf("unregistered schema object type: %s", name)
		}
		if _, ok := Policy(name); !ok {
			return fmt.Errorf("unassigned schema object: %s", name)
		}
	}
	return nil
}
