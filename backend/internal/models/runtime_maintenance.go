package models

import (
	"errors"
	"time"
)

var ErrRuntimeMaintenanceConflict = errors.New("runtime maintenance requires a fresh drain-capable runtime and matching operation; old instances remain unchanged")
var ErrOpenCodeSafeUpgradeRequired = errors.New("OpenCode shared-pool rolling replacement is disabled: per-instance drain, backup and verified migration are required; existing instances are unchanged")

// This is a drain lease, NOT permission to delete a pod or a backup certificate.
type RuntimeMaintenance struct {
	UpgradeFenced bool       `db:"-" json:"upgrade_fenced"`
	InstanceID    int        `db:"instance_id" json:"-"`
	OwnerID       int        `db:"owner_user_id" json:"-"`
	Generation    int        `db:"generation" json:"generation"`
	Operation     string     `db:"operation_id" json:"operation_id"`
	RequestedBy   int        `db:"requested_by" json:"requested_by"`
	Deadline      time.Time  `db:"deadline" json:"deadline"`
	Cancelled     bool       `db:"cancelled" json:"cancelled"`
	GateClosed    bool       `db:"gate_closed" json:"gate_closed"`
	Active        int        `db:"active_executions" json:"active_executions"`
	Admissions    int        `db:"active_admissions" json:"active_admissions"`
	Jobs          int        `db:"active_jobs" json:"active_jobs"`
	Queued        int        `db:"queued_inputs" json:"queued_inputs"`
	ReportedAt    *time.Time `db:"reported_at" json:"reported_at"`
}

func (m RuntimeMaintenance) Draining(now time.Time) bool {
	return !m.Cancelled && (m.UpgradeFenced || now.Before(m.Deadline))
}
func (m RuntimeMaintenance) Drained(now time.Time) bool {
	return m.Draining(now) && m.GateClosed && m.ReportedAt != nil && !m.ReportedAt.After(now) &&
		now.Sub(*m.ReportedAt) < 5*time.Second && m.Active == 0 && m.Admissions == 0 && m.Jobs == 0 && m.Queued == 0
}

type RuntimeMaintenanceReport struct {
	Generation int    `json:"generation"`
	Operation  string `json:"operation_id"`
	GateClosed bool   `json:"gate_closed"`
	Active     int    `json:"active_executions"`
	Admissions int    `json:"active_admissions"`
	Jobs       int    `json:"active_jobs"`
	Queued     int    `json:"queued_inputs"`
}

func (r RuntimeMaintenanceReport) Valid() bool {
	return r.Generation >= 0 && (r.Operation == "" || len(r.Operation) == 32) &&
		r.Active >= 0 && r.Admissions >= 0 && r.Jobs >= 0 && r.Queued >= 0 &&
		r.Active <= 1000000 && r.Admissions <= 1000000 && r.Jobs <= 1000000 && r.Queued <= 1000000
}
