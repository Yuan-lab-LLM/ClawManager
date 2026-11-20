package models

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Protocol 0 is the existing wire contract. It is never inferred from a version
// string, and existing instances are not enrolled by a database migration.
const RuntimeGovernanceProtocol = 1

const RuntimeCapabilityReportTTL = 30 * time.Second

var ErrRuntimeCapabilityRenewalConflict = errors.New("runtime capability renewal requires the same enrolled generation, versions and capabilities")

// Return a fresh slice so callers cannot mutate the negotiation contract.
func RequiredRuntimeCapabilities() []string {
	return []string{"task_identity", "turn_cancel", "cancel_receipt", "live_catalog"}
}

type RuntimeCapabilityReport struct {
	Protocol       int      `json:"protocol"`
	Generation     int      `json:"generation"`
	RuntimeVersion string   `json:"runtime_version"`
	AdapterVersion string   `json:"adapter_version"`
	Capabilities   []string `json:"capabilities"`
}

func (r RuntimeCapabilityReport) Validate() error {
	if r.Protocol != RuntimeGovernanceProtocol || r.Generation < 0 || int64(r.Generation) > 2147483647 {
		return fmt.Errorf("unsupported runtime governance protocol or generation")
	}
	for _, v := range []string{r.RuntimeVersion, r.AdapterVersion} {
		if strings.TrimSpace(v) != v || v == "" || len(v) > 100 || strings.ContainsAny(v, "\r\n\x00") {
			return fmt.Errorf("invalid runtime version")
		}
	}
	allowed := map[string]bool{"task_identity": true, "turn_cancel": true, "cancel_receipt": true, "live_catalog": true, "maintenance_drain": true}
	seen := map[string]bool{}
	for _, capability := range r.Capabilities {
		if !allowed[capability] || seen[capability] {
			return fmt.Errorf("invalid runtime capability")
		}
		seen[capability] = true
	}
	for _, required := range RequiredRuntimeCapabilities() {
		if !seen[required] {
			return fmt.Errorf("missing runtime capability: %s", required)
		}
	}
	return nil
}

type RuntimeProtocolState struct {
	InstanceID       int       `db:"instance_id" json:"-"`
	OwnerUserID      int       `db:"owner_user_id" json:"-"`
	Generation       int       `db:"generation" json:"generation"`
	Protocol         int       `db:"protocol" json:"protocol"`
	RuntimeVersion   string    `db:"runtime_version" json:"runtime_version"`
	AdapterVersion   string    `db:"adapter_version" json:"adapter_version"`
	CapabilitiesJSON string    `db:"capabilities_json" json:"-"`
	ReportedAt       time.Time `db:"reported_at" json:"reported_at"`
}

// Renewal is not enrollment. A stale report may be refreshed only with the
// exact enrolled contract; reordering capability names is not a contract change.
func (s *RuntimeProtocolState) MatchesReport(ownerID int, report RuntimeCapabilityReport) bool {
	if s == nil || s.OwnerUserID != ownerID || s.Generation != report.Generation || s.Protocol != report.Protocol || s.RuntimeVersion != report.RuntimeVersion || s.AdapterVersion != report.AdapterVersion || report.Validate() != nil {
		return false
	}
	var capabilities []string
	if json.Unmarshal([]byte(s.CapabilitiesJSON), &capabilities) != nil {
		return false
	}
	existing := report
	existing.Capabilities = capabilities
	if existing.Validate() != nil || len(capabilities) != len(report.Capabilities) {
		return false
	}
	seen := make(map[string]bool, len(capabilities))
	for _, capability := range capabilities {
		seen[capability] = true
	}
	for _, capability := range report.Capabilities {
		if !seen[capability] {
			return false
		}
	}
	return true
}

// CurrentReport checks bootstrap liveness without requiring the platform to
// have already marked the instance running (which would deadlock readiness).
func (s *RuntimeProtocolState) CurrentReport(instanceID, ownerID, generation int, now time.Time) bool {
	if s == nil || instanceID <= 0 || ownerID <= 0 || s.InstanceID != instanceID || s.OwnerUserID != ownerID || s.Generation != generation || s.ReportedAt.After(now) || now.Sub(s.ReportedAt) >= RuntimeCapabilityReportTTL {
		return false
	}
	var capabilities []string
	if json.Unmarshal([]byte(s.CapabilitiesJSON), &capabilities) != nil {
		return false
	}
	return (RuntimeCapabilityReport{Protocol: s.Protocol, Generation: s.Generation, RuntimeVersion: s.RuntimeVersion, AdapterVersion: s.AdapterVersion, Capabilities: capabilities}).Validate() == nil
}

// Readiness describes the current report, not enrollment or proof that a task
// stopped. Stale/incomplete reports must never disable sticky enforcement.
func (s *RuntimeProtocolState) Readiness(instance *Instance, capabilities []string, now time.Time) (string, string) {
	if s == nil {
		return "legacy", "not_enrolled"
	}
	reason := "connected"
	report := RuntimeCapabilityReport{Protocol: s.Protocol, Generation: s.Generation, RuntimeVersion: s.RuntimeVersion, AdapterVersion: s.AdapterVersion, Capabilities: capabilities}
	switch {
	case s.Protocol != RuntimeGovernanceProtocol:
		reason = "unsupported_protocol"
	case instance == nil || s.OwnerUserID != instance.UserID:
		reason = "owner_changed"
	case s.Generation != instance.RuntimeGeneration:
		reason = "generation_changed"
	case instance.Status != "running":
		reason = "runtime_not_running"
	case s.ReportedAt.After(now):
		reason = "report_clock_skew"
	case now.Sub(s.ReportedAt) >= RuntimeCapabilityReportTTL:
		reason = "report_expired"
	case report.Validate() != nil:
		reason = "invalid_report"
	}
	if reason != "connected" {
		return "awaiting_runtime", reason
	}
	return "governed", reason
}
