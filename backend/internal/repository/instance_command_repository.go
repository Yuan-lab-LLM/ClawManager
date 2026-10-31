package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type InstanceCommandRepository interface {
	Create(command *models.InstanceCommand) error
	Update(command *models.InstanceCommand) error
	GetByID(id int) (*models.InstanceCommand, error)
	GetByInstanceIdempotencyKey(instanceID int, idempotencyKey string) (*models.InstanceCommand, error)
	GetNextPendingByInstance(instanceID int) (*models.InstanceCommand, error)
	ListByInstanceID(instanceID int, limit int) ([]models.InstanceCommand, error)
	FindLatestFailedCollectSkillPackage(skillExternalID string) (*models.InstanceCommand, error)
}

type instanceCommandRepository struct {
	sess db.Session
}

func NewInstanceCommandRepository(sess db.Session) InstanceCommandRepository {
	return &instanceCommandRepository{sess: sess}
}

func (r *instanceCommandRepository) Create(command *models.InstanceCommand) error {
	if command == nil || command.InstanceID <= 0 {
		return fmt.Errorf("invalid instance command")
	}
	return (&instanceRepository{r.sess}).withUnfencedInstance(context.Background(), command.InstanceID, func(tx *instanceRepository) error {
		return (&instanceCommandRepository{tx.sess}).create(command)
	})
}

func (r *instanceCommandRepository) create(command *models.InstanceCommand) error {
	ensureTimestamps(&command.CreatedAt, &command.UpdatedAt)
	if command.IssuedAt.IsZero() {
		command.IssuedAt = time.Now().UTC()
	}
	res, err := r.sess.Collection("instance_commands").Insert(command)
	if err != nil {
		return fmt.Errorf("failed to create instance command: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		command.ID = int(id)
	}
	return nil
}

func (r *instanceCommandRepository) Update(command *models.InstanceCommand) error {
	if command == nil || command.InstanceID <= 0 || command.ID <= 0 {
		return fmt.Errorf("invalid instance command")
	}
	return (&instanceRepository{r.sess}).withInstanceRowLock(context.Background(), command.InstanceID, func(tx *instanceRepository) error {
		store := &instanceCommandRepository{tx.sess}
		current, err := store.GetByID(command.ID)
		if err != nil {
			return err
		}
		if current == nil || current.InstanceID != command.InstanceID {
			return fmt.Errorf("instance command identity mismatch")
		}
		terminal := command.Status == "succeeded" || command.Status == "failed"
		if !terminal {
			if err := checkRuntimeUpgrade(context.Background(), tx.sess, command.InstanceID); err != nil {
				return err
			}
		}
		if current.Status == "succeeded" || current.Status == "failed" {
			if current.Status == command.Status {
				return nil
			}
			return fmt.Errorf("instance command is already finished")
		}
		return store.update(command)
	})
}

func (r *instanceCommandRepository) update(command *models.InstanceCommand) error {
	if command.UpdatedAt.IsZero() {
		command.UpdatedAt = time.Now().UTC()
	}
	if err := r.sess.Collection("instance_commands").Find(db.Cond{"id": command.ID}).Update(command); err != nil {
		return fmt.Errorf("failed to update instance command: %w", err)
	}
	return nil
}

func (r *instanceCommandRepository) GetByID(id int) (*models.InstanceCommand, error) {
	var item models.InstanceCommand
	if err := r.sess.Collection("instance_commands").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance command: %w", err)
	}
	return &item, nil
}

func (r *instanceCommandRepository) GetByInstanceIdempotencyKey(instanceID int, idempotencyKey string) (*models.InstanceCommand, error) {
	var item models.InstanceCommand
	if err := r.sess.Collection("instance_commands").Find(db.Cond{"instance_id": instanceID, "idempotency_key": idempotencyKey}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance command by idempotency key: %w", err)
	}
	return &item, nil
}

func (r *instanceCommandRepository) GetNextPendingByInstance(instanceID int) (*models.InstanceCommand, error) {
	var item models.InstanceCommand
	if err := r.sess.Collection("instance_commands").Find(db.Cond{"instance_id": instanceID, "status": "pending"}).OrderBy("issued_at", "id").One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get next pending instance command: %w", err)
	}
	return &item, nil
}

func (r *instanceCommandRepository) ListByInstanceID(instanceID int, limit int) ([]models.InstanceCommand, error) {
	if limit <= 0 {
		limit = 20
	}

	var items []models.InstanceCommand
	if err := r.sess.Collection("instance_commands").Find(db.Cond{"instance_id": instanceID}).OrderBy("-issued_at", "-id").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list instance commands: %w", err)
	}
	return items, nil
}

func (r *instanceCommandRepository) FindLatestFailedCollectSkillPackage(skillExternalID string) (*models.InstanceCommand, error) {
	skillExternalID = strings.TrimSpace(skillExternalID)
	if skillExternalID == "" {
		return nil, nil
	}
	pattern := fmt.Sprintf("%%\"skill_id\":\"%s\"%%", skillExternalID)
	var item models.InstanceCommand
	if err := r.sess.Collection("instance_commands").Find(db.Cond{
		"command_type": "collect_skill_package",
		"status":       "failed",
	}).And("payload_json LIKE ?", pattern).OrderBy("-finished_at", "-id").One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find failed collect skill package command: %w", err)
	}
	return &item, nil
}
