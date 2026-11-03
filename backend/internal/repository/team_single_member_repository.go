package repository

import (
	"fmt"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Kept optional so existing Team repository implementations and ordinary
// collaboration retain their contract. The new mode needs these atomic writes.
type SingleMemberTeamRepository interface {
	CreateSingleMemberTask(task *models.TeamTask, item *models.TeamWorkItem) error
	AcceptSingleMemberCompletion(task *models.TeamTask, item *models.TeamWorkItem, expectedLedgerVersion int64, event *models.TeamEvent, outbox *models.TeamEventOutbox) (bool, error)
}

func (r *teamRepository) CreateSingleMemberTask(task *models.TeamTask, item *models.TeamWorkItem) error {
	if task == nil || item == nil {
		return fmt.Errorf("single-member task and work item are required")
	}
	return r.sess.Tx(func(sess db.Session) error {
		tx := &teamRepository{sess: sess}
		if err := tx.CreateTask(task); err != nil {
			return err
		}
		item.RootTaskID = task.ID
		return tx.UpsertWorkItem(item)
	})
}

func (r *teamRepository) AcceptSingleMemberCompletion(task *models.TeamTask, item *models.TeamWorkItem, version int64, event *models.TeamEvent, outbox *models.TeamEventOutbox) (bool, error) {
	if item == nil || task == nil || item.RootTaskID != task.ID || item.TeamID != task.TeamID || item.OwnerMemberID == nil || *item.OwnerMemberID != task.TargetMemberID {
		return false, fmt.Errorf("single-member completion does not match the issued work item")
	}
	return r.acceptRootCompletion(task, version, event, outbox, item)
}
