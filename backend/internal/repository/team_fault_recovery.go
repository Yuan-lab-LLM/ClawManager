package repository

import (
	"context"
	"fmt"

	"github.com/upper/db/v4"
)

// The lifecycle row is shared with task, model, and Team power admission.
// Reserve it in the same transaction as the existing fault-recovery journal.
func ReserveTeamFaultRecovery(ctx context.Context, tx db.Session, teamID int, instanceIDs []int, operation string) error {
	if operation == "" {
		return fmt.Errorf("Team fault recovery requires an operation identity")
	}
	if err := checkTeamFaultRecovery(ctx, tx, teamID, instanceIDs, "", true); err != nil {
		return err
	}
	_, err := tx.SQL().ExecContext(ctx, `UPDATE team_runtime_lifecycle SET fault_recovery_operation_id=? WHERE team_id=?`, operation, teamID)
	return err
}

func CheckTeamFaultRecovery(ctx context.Context, s db.Session, teamID int, instanceIDs []int, operation string) error {
	return checkTeamFaultRecovery(ctx, s, teamID, instanceIDs, operation, false)
}

func checkTeamFaultRecovery(ctx context.Context, s db.Session, teamID int, instanceIDs []int, operation string, lock bool) error {
	if teamID < 1 || len(instanceIDs) == 0 {
		return fmt.Errorf("Team fault recovery requires an exact Team and its affected members")
	}
	v, err := readTeamLifecycle(ctx, s, teamID, lock)
	if err != nil {
		return err
	}
	if v == nil || v.State != "running" || v.DesiredState != "running" || v.FaultRecoveryOperationID != operation {
		return fmt.Errorf("Team lifecycle state or recovery ownership changed")
	}
	for _, id := range instanceIDs {
		team, err := teamForInstance(ctx, s, id)
		if err != nil {
			return err
		}
		var members int
		err = teamQueryRow(ctx, s, `SELECT COUNT(*) FROM team_members m JOIN teams t ON t.id=m.team_id JOIN instances i ON i.id=m.instance_id WHERE m.team_id=? AND i.id=? AND m.user_id=t.user_id AND i.user_id=t.user_id AND t.status='running' AND m.status NOT IN ('deleted','deleting','creating','failed')`, teamID, id).Scan(&members)
		if err != nil {
			return err
		}
		if team != teamID || members != 1 {
			return fmt.Errorf("instance %d is outside the current Team recovery scope", id)
		}
	}
	if err := teamHasOpenWork(ctx, s, teamID); err != nil {
		return err
	}
	return teamMembersNoRequests(ctx, s, teamID)
}

func ReleaseTeamFaultRecovery(ctx context.Context, tx db.Session, teamID int, operation string) error {
	result, err := tx.SQL().ExecContext(ctx, `UPDATE team_runtime_lifecycle SET fault_recovery_operation_id='' WHERE team_id=? AND fault_recovery_operation_id=?`, teamID, operation)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return fmt.Errorf("Team fault recovery guard changed")
	}
	return err
}
