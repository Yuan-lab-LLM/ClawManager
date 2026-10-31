package systembackup

import (
	"sort"
	"strings"

	"clawreef/internal/systembackup/team"
)

// TeamRecoverySummary contains counts and registered states only. Task bodies,
// results, owner identifiers, file paths and credentials remain in the encrypted
// artifact; this summary does not duplicate them into a plaintext report.
type TeamRecoverySummary struct {
	Tables              []TeamRecoveryTable `json:"tables"`
	States              []TeamRecoveryState `json:"states"`
	SQLReadbackVerified bool                `json:"sql_readback_verified"`
}
type TeamRecoveryTable struct {
	Table           string `json:"table"`
	Rows            int64  `json:"rows"`
	QuarantinedRows int64  `json:"quarantined_rows"`
}
type TeamRecoveryState struct {
	Table    string  `json:"table"`
	Field    string  `json:"field"`
	Captured *string `json:"captured"`
	Restored *string `json:"restored"`
	Rows     int64   `json:"rows"`
}
type teamReportCollector struct {
	tables map[string]*TeamRecoveryTable
	states map[string]*TeamRecoveryState
}

func (c *teamReportCollector) record(change team.Change) {
	switch change.Table {
	case "teams", "team_members", "team_tasks", "team_work_items", "team_workflow_phases", "team_events", "team_event_outbox", "team_workspace_file_audits", "owner_team_provision_steps", "northbound_operations":
	default:
		return
	}
	if c.tables == nil {
		c.tables = map[string]*TeamRecoveryTable{}
		c.states = map[string]*TeamRecoveryState{}
	}
	table := c.tables[change.Table]
	if table == nil {
		table = &TeamRecoveryTable{Table: change.Table}
		c.tables[change.Table] = table
	}
	table.Rows++
	if change.Quarantined {
		table.QuarantinedRows++
	}
	for _, field := range []string{"status", "workflow_state"} {
		before, ok := change.Before[field]
		if !ok {
			continue
		}
		after, ok := change.After[field]
		if !ok {
			continue
		}
		var captured, restored *string
		if !before.IsNull {
			v := string(before.Data)
			captured = &v
		}
		if !after.IsNull {
			v := string(after.Data)
			restored = &v
		}
		// Normalization has already validated these finite state enums.
		cellKey := func(v *string) string {
			if v == nil {
				return "null"
			}
			return "value:" + *v
		}
		key := strings.Join([]string{change.Table, field, cellKey(captured), cellKey(restored)}, "\x00")
		state := c.states[key]
		if state == nil {
			state = &TeamRecoveryState{Table: change.Table, Field: field, Captured: captured, Restored: restored}
			c.states[key] = state
		}
		state.Rows++
	}
}
func (c *teamReportCollector) summary(verified bool) TeamRecoverySummary {
	out := TeamRecoverySummary{Tables: []TeamRecoveryTable{}, States: []TeamRecoveryState{}, SQLReadbackVerified: verified}
	keys := make([]string, 0, len(c.tables))
	for key := range c.tables {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.Tables = append(out.Tables, *c.tables[key])
	}
	keys = keys[:0]
	for key := range c.states {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.States = append(out.States, *c.states[key])
	}
	return out
}
