package models

import (
	"fmt"
	"strings"
	"time"
)

// Protocol 0 is the existing wire contract. It is never inferred from a version
// string, and existing instances are not enrolled by a database migration.
const RuntimeGovernanceProtocol = 1

type RuntimeCapabilityReport struct {
	Protocol       int      `json:"protocol"`
	Generation     int      `json:"generation"`
	RuntimeVersion string   `json:"runtime_version"`
	AdapterVersion string   `json:"adapter_version"`
	Capabilities   []string `json:"capabilities"`
}

func (r RuntimeCapabilityReport) Validate() error {
	if r.Protocol != RuntimeGovernanceProtocol || r.Generation < 0 {
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
	for _, required := range []string{"task_identity", "turn_cancel", "cancel_receipt", "live_catalog"} {
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
