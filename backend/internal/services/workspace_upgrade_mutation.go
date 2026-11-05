package services

import (
	"context"
	"errors"
	"fmt"
	"github.com/upper/db/v4"
	"io"
)

var ErrWorkspaceUpgradeBusy = errors.New("实例正在升级或恢复，暂不可修改文件或运行状态")

type WorkspaceUpgradeMutationGuard interface {
	WithWorkspaceMutation(context.Context, WorkspaceFileScope, func() error) error
}

func ConfigureWorkspaceUpgradeGuard(files WorkspaceFileService, guard WorkspaceUpgradeMutationGuard) {
	if s, ok := files.(*workspaceFileService); ok {
		s.mutationGuard = guard
	}
}
func (s *workspaceFileService) withMutation(ctx context.Context, scope WorkspaceFileScope, run func() error) error {
	if s.mutationGuard == nil {
		return run()
	}
	return s.mutationGuard.WithWorkspaceMutation(ctx, scope, run)
}
func (s *workspaceFileService) Upload(ctx context.Context, scope WorkspaceFileScope, dir, name string, reader io.Reader, size int64) (*WorkspaceEntry, error) {
	var result *WorkspaceEntry
	err := s.withMutation(ctx, scope, func() error { var e error; result, e = s.upload(ctx, scope, dir, name, reader, size); return e })
	return result, err
}
func (s *workspaceFileService) Mkdir(ctx context.Context, scope WorkspaceFileScope, name string) (*WorkspaceEntry, error) {
	var result *WorkspaceEntry
	err := s.withMutation(ctx, scope, func() error { var e error; result, e = s.mkdir(ctx, scope, name); return e })
	return result, err
}
func (s *workspaceFileService) Rename(ctx context.Context, scope WorkspaceFileScope, old, next string) (*WorkspaceEntry, error) {
	var result *WorkspaceEntry
	err := s.withMutation(ctx, scope, func() error { var e error; result, e = s.rename(ctx, scope, old, next); return e })
	return result, err
}
func (s *workspaceFileService) Delete(ctx context.Context, scope WorkspaceFileScope, name string) error {
	return s.withMutation(ctx, scope, func() error { return s.delete(ctx, scope, name) })
}
func (s *workspaceFileService) SaveText(ctx context.Context, scope WorkspaceFileScope, name, text, version string) (*WorkspaceText, error) {
	var result *WorkspaceText
	err := s.withMutation(ctx, scope, func() error { var e error; result, e = s.saveText(ctx, scope, name, text, version); return e })
	return result, err
}

// The same ordered instance-row locks are taken by ConfirmPreflight. Thus an
// in-flight editor finishes before recovery admission, or is rejected after it.
func (s *RuntimeUpgradeService) WithWorkspaceMutation(ctx context.Context, scope WorkspaceFileScope, run func() error) error {
	var ids []int
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT id FROM instances WHERE id=? OR id IN (SELECT instance_id FROM team_members WHERE team_id=? AND status NOT IN ('deleted','deleting')) ORDER BY id`, scope.InstanceID, scope.TeamID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
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
	release, err := s.lockWorkspaceOperations(ids)
	if err != nil {
		return err
	}
	defer release()
	err = s.sess.TxContext(ctx, func(tx db.Session) error {
		rows, err := tx.SQL().QueryContext(ctx, `SELECT id FROM instances WHERE id=? OR id IN (SELECT instance_id FROM team_members WHERE team_id=? AND status NOT IN ('deleted','deleting')) ORDER BY id FOR UPDATE`, scope.InstanceID, scope.TeamID)
		if err != nil {
			return err
		}
		var lockedIDs []int
		for rows.Next() {
			var id int
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			lockedIDs = append(lockedIDs, id)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
		if fmt.Sprint(ids) != fmt.Sprint(lockedIDs) {
			return ErrWorkspaceUpgradeBusy
		}
		for _, id := range lockedIDs {
			if err := s.validateInstanceMutation(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	}, nil)
	if err != nil {
		return err
	}
	// No SQL row lock is held while lifecycle repositories/audit writers use
	// their own connections. The NFS fence spans the entire real mutation.
	return run()
}

func (s *RuntimeUpgradeService) WithInstanceMutation(ctx context.Context, id int, run func() error) error {
	return s.WithWorkspaceMutation(ctx, WorkspaceFileScope{InstanceID: id}, run)
}

// Called while WithWorkspaceMutation holds the ordered Team workspace fences.
// Only active members with an open matching generation receive intents. Stopped
// members must not prevent authorized writers using their Team shared directory.
// Each active member gets its own durable intent because slot/container teardown cannot
// establish completion of an independently running management file writer.
func (s *RuntimeUpgradeService) WorkspaceTeamWriteScopes(ctx context.Context, scope WorkspaceFileScope) ([]WorkspaceFileScope, error) {
	if scope.TeamID <= 0 || scope.UserID <= 0 {
		return nil, ErrWorkspacePathInvalid
	}
	rows, err := s.sess.SQL().QueryContext(ctx, `SELECT DISTINCT i.id,i.user_id,i.runtime_generation FROM instances i JOIN team_members m ON m.instance_id=i.id JOIN teams t ON t.id=m.team_id JOIN instance_workspace_write_fences f ON f.instance_id=i.id AND f.owner_user_id=i.user_id AND f.generation=i.runtime_generation AND f.state='open' WHERE m.team_id=? AND t.user_id=? AND m.status NOT IN ('deleted','deleting') AND i.status IN ('creating','running') ORDER BY i.id`, scope.TeamID, scope.UserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var scopes []WorkspaceFileScope
	for rows.Next() {
		var member WorkspaceFileScope
		if err = rows.Scan(&member.InstanceID, &member.UserID, &member.RuntimeGeneration); err != nil {
			return nil, err
		}
		scopes = append(scopes, member)
	}
	return scopes, rows.Err()
}

// Shared by lifecycle execution and management preflight. The execution caller
// also holds the workspace fence for the complete mutation.
func (s *RuntimeUpgradeService) validateInstanceMutation(ctx context.Context, sess db.Session, id int) error {
	var count int
	row, err := sess.SQL().QueryRowContext(ctx, `SELECT COUNT(*) FROM runtime_upgrade_items ui JOIN runtime_rollouts r ON ui.rollout_id=r.id WHERE ui.instance_id=? AND (r.status IN ('pending','running') OR (r.status='error' AND ui.recovery_status='needs_attention' AND NOT EXISTS (SELECT 1 FROM runtime_upgrade_items n JOIN runtime_rollouts nr ON nr.id=n.rollout_id WHERE n.instance_id=ui.instance_id AND nr.id>r.id AND nr.status='finished')))`, id)
	if err != nil {
		return err
	}
	if err = row.Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("%w: #%d", ErrWorkspaceUpgradeBusy, id)
	}
	return nil
}
