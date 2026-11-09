package services

import (
	"strings"

	"clawreef/internal/models"
)

func teamMemberInstanceStarting(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "creating", "starting", "pending":
		return true
	}
	return false
}

// One read-model contract for HTTP details and the durable consistency sweep.
// Instance liveness does not imply assignment success or clear business blockers.
func projectTeamMemberInstanceAvailability(member *models.TeamMember, instance *models.Instance, items []models.TeamWorkItem) (unavailable, changed bool) {
	before := *member
	status := strings.ToLower(strings.TrimSpace(instance.Status))
	runtimeBlock := strings.HasPrefix(strings.TrimSpace(derefTeamString(member.BlockedReason)), teamRuntimeUnavailableReasonPrefix)
	businessBlock := member.BlockedReason != nil && strings.TrimSpace(*member.BlockedReason) != "" && !runtimeBlock
	setRuntimeStatus := func(value string) {
		if derefTeamString(member.RuntimeStatus) != value {
			member.RuntimeStatus = &value
		}
	}
	if teamMemberInstanceStarting(status) {
		// Keep a busy member's assignment binding and progress during recovery.
		if member.CurrentTaskID == nil && member.Status != models.TeamMemberStatusBusy && member.Status != models.TeamMemberStatusFailed {
			member.Status = models.TeamMemberStatusCreating
		}
		member.Availability = models.TeamMemberAvailabilityUnknown
		setRuntimeStatus(status)
		if runtimeBlock {
			member.BlockedReason = nil
		}
		if businessBlock {
			member.Availability = models.TeamMemberAvailabilityBlocked
		}
		return true, *member != before
	}
	if status == "running" {
		wasUnavailable := member.Status == models.TeamMemberStatusOffline || member.Status == models.TeamMemberStatusCreating ||
			member.Availability == models.TeamMemberAvailabilityOffline || member.Availability == models.TeamMemberAvailabilityUnknown ||
			teamMemberInstanceStarting(derefTeamString(member.RuntimeStatus)) || runtimeBlock
		if !wasUnavailable {
			return false, false
		}
		// Reconcile scheduling only from owned actionable ledger facts; a
		// no-change return from that helper is not the absence of those facts.
		ledgerState := false
		schedulingItems := items
		if member.CurrentTaskID != nil {
			schedulingItems = nil
			for _, item := range items {
				// Older receipts must not erase a newer binding. Newer active
				// assignments can still repair an obsolete current-task pointer.
				if item.RootTaskID >= *member.CurrentTaskID {
					schedulingItems = append(schedulingItems, item)
				}
			}
		}
		for _, item := range schedulingItems {
			if item.OwnerMemberID == nil || *item.OwnerMemberID != member.ID || item.SupersededBy != nil {
				continue
			}
			switch item.Status {
			case models.TeamTaskStatusDispatched, models.TeamTaskStatusRunning, models.TeamTaskStatusWaiting,
				models.TeamTaskStatusSucceeded, models.TeamTaskStatusFailed, models.TeamTaskStatusStale:
				ledgerState = true
			}
		}
		if ledgerState {
			reconcileTeamMemberOperationalState(member, schedulingItems)
		} else if member.CurrentTaskID != nil || member.Status == models.TeamMemberStatusBusy {
			member.Status = models.TeamMemberStatusBusy
			member.Availability = models.TeamMemberAvailabilityBusy
		} else if member.Status != models.TeamMemberStatusFailed {
			member.Status = models.TeamMemberStatusIdle
			member.Availability = models.TeamMemberAvailabilityIdle
		}
		if !ledgerState || !isTerminalTeamTaskStatus(derefTeamString(member.RuntimeStatus)) {
			setRuntimeStatus(status)
		}
		if runtimeBlock && strings.HasPrefix(derefTeamString(member.BlockedReason), teamRuntimeUnavailableReasonPrefix) {
			member.BlockedReason = nil
		}
		if businessBlock {
			member.BlockedReason = before.BlockedReason
			member.Availability = models.TeamMemberAvailabilityBlocked
			if before.Status == models.TeamMemberStatusFailed {
				member.Status = before.Status
			}
		}
		return false, *member != before
	}
	// A confirmed stopped/missing/error instance remains a genuine runtime
	// failure. Preserve the existing unavailable/recovery path for those states.
	member.Status = models.TeamMemberStatusOffline
	member.Availability = models.TeamMemberAvailabilityOffline
	setRuntimeStatus(status)
	reason := "linked instance is " + status
	if instance.RuntimeErrorMessage != nil && strings.TrimSpace(*instance.RuntimeErrorMessage) != "" {
		reason = strings.TrimSpace(*instance.RuntimeErrorMessage)
	}
	reason = teamRuntimeUnavailableReasonPrefix + reason
	if derefTeamString(member.BlockedReason) != reason {
		member.BlockedReason = &reason
	}
	return true, *member != before
}
