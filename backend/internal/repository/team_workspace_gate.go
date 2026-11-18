package repository

import (
	"clawreef/internal/models"
	"context"
	"database/sql"
	"errors"
	"github.com/upper/db/v4"
	"reflect"
	"sort"
	"strings"
)

// Enable only before publishing repositories for the explicit NFS profile.
func ConfigureTeamNFSWriteFences(repo TeamRepository, profile string) {
	if r, ok := repo.(*teamRepository); ok {
		r.nfsWriterFences = profile == "nfs-rootless-v1"
	}
}

func requireNoTeamWorkspaceWrites(ctx context.Context, sess db.Session, teamID int) error {
	// A transaction may already have an older REPEATABLE READ snapshot from
	// instance/command checks. Only a current read sees writers committed before
	// the Team-row lock was acquired; a plain COUNT can admit a replacement.
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT operation_id FROM team_workspace_write_intents WHERE team_id=? LIMIT 1 FOR UPDATE`, teamID)
	if err != nil {
		return err
	}
	var operation string
	if err = row.Scan(&operation); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return err
	}
	return models.ErrRuntimeUpgradeWritersActive
}

// Called with the instance locked. Team admission takes these same Team rows,
// so an unknown shared write cannot race replacement/maintenance admission.
func requireNoInstanceTeamWorkspaceWrites(ctx context.Context, sess db.Session, instanceID int) error {
	// Locking only the outer teams query leaves the nested membership SELECT
	// on an older snapshot too. The join makes both parts a current read.
	rows, err := sess.SQL().QueryContext(ctx, `SELECT t.id FROM teams t INNER JOIN team_members m ON m.team_id=t.id WHERE m.instance_id=? ORDER BY t.id FOR UPDATE`, instanceID)
	if err != nil {
		return err
	}
	var ids []int
	for rows.Next() {
		var id int
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = requireNoTeamWorkspaceWrites(ctx, sess, id); err != nil {
			return err
		}
	}
	return nil
}

// The caller holds the instance row and its native operation fence throughout
// admission. Team Begin uses the same Team row locks before dispatch.
func RequireNoInstanceTeamWorkspaceWriters(ctx context.Context, sess db.Session, instanceID int) error {
	return requireNoInstanceTeamWorkspaceWrites(ctx, sess, instanceID)
}

func (r *teamRepository) withNFSTeamRows(ids []int, run func(*teamRepository) error) error {
	sort.Ints(ids)
	return r.sess.TxContext(context.Background(), func(sess db.Session) error {
		previous := 0
		for _, id := range ids {
			if id <= 0 || id == previous {
				continue
			}
			previous = id
			row, err := sess.SQL().QueryRow(`SELECT id FROM teams WHERE id=? FOR UPDATE`, id)
			if err != nil {
				return err
			}
			var found int
			if err = row.Scan(&found); err != nil {
				if errors.Is(err, sql.ErrNoRows) {
					return models.ErrRuntimeUpgradeConflict
				}
				return err
			}
		}
		return run(&teamRepository{sess: sess})
	}, nil)
}

func teamStorageIdentityChanged(old, next *models.Team) bool {
	terminal := func(s string) bool {
		s = strings.ToLower(strings.TrimSpace(s))
		return s == "deleting" || s == "deleted"
	}
	return old.UserID != next.UserID || old.ResourceScope != next.ResourceScope || !reflect.DeepEqual(old.Owner, next.Owner) || !reflect.DeepEqual(old.SharedPVCName, next.SharedPVCName) || !reflect.DeepEqual(old.SharedPVCNamespace, next.SharedPVCNamespace) || old.SharedMountPath != next.SharedMountPath || !reflect.DeepEqual(old.StorageClass, next.StorageClass) || terminal(next.Status) && old.Status != next.Status
}
func teamMemberIdentityChanged(old, next *models.TeamMember) bool {
	terminal := func(s string) bool {
		s = strings.ToLower(strings.TrimSpace(s))
		return s == "deleting" || s == "deleted"
	}
	return old.TeamID != next.TeamID || old.UserID != next.UserID || !reflect.DeepEqual(old.InstanceID, next.InstanceID) || old.MemberKey != next.MemberKey || old.Role != next.Role || old.RuntimeType != next.RuntimeType || old.InstanceMode != next.InstanceMode || terminal(next.Status) && old.Status != next.Status
}
