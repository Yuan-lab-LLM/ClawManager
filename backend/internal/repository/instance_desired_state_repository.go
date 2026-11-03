package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type InstanceDesiredStateRepository interface {
	GetByInstanceID(instanceID int) (*models.InstanceDesiredState, error)
	Create(state *models.InstanceDesiredState) error
	Update(state *models.InstanceDesiredState) error
}

type instanceDesiredStateRepository struct {
	sess db.Session
}

func NewInstanceDesiredStateRepository(sess db.Session) InstanceDesiredStateRepository {
	return &instanceDesiredStateRepository{sess: sess}
}

func (r *instanceDesiredStateRepository) GetByInstanceID(instanceID int) (*models.InstanceDesiredState, error) {
	var item models.InstanceDesiredState
	if err := r.sess.Collection("instance_desired_state").Find(db.Cond{"instance_id": instanceID}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance desired state: %w", err)
	}
	return &item, nil
}

func (r *instanceDesiredStateRepository) Create(state *models.InstanceDesiredState) error {
	if state == nil || state.InstanceID <= 0 {
		return fmt.Errorf("invalid instance desired state")
	}
	return (&instanceRepository{r.sess}).withUnfencedInstance(context.Background(), state.InstanceID, func(tx *instanceRepository) error {
		return (&instanceDesiredStateRepository{tx.sess}).create(state)
	})
}

func (r *instanceDesiredStateRepository) create(state *models.InstanceDesiredState) error {
	ensureTimestamps(&state.CreatedAt, &state.UpdatedAt)
	res, err := r.sess.Collection("instance_desired_state").Insert(state)
	if err != nil {
		return fmt.Errorf("failed to create instance desired state: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		state.ID = int(id)
	}
	return nil
}

func (r *instanceDesiredStateRepository) Update(state *models.InstanceDesiredState) error {
	if state == nil || state.InstanceID <= 0 || state.ID <= 0 {
		return fmt.Errorf("invalid instance desired state")
	}
	return (&instanceRepository{r.sess}).withUnfencedInstance(context.Background(), state.InstanceID, func(tx *instanceRepository) error {
		store := &instanceDesiredStateRepository{tx.sess}
		current, err := store.GetByInstanceID(state.InstanceID)
		if err != nil {
			return err
		}
		if current == nil || current.ID != state.ID {
			return fmt.Errorf("instance desired state identity mismatch")
		}
		return store.update(state)
	})
}

func (r *instanceDesiredStateRepository) update(state *models.InstanceDesiredState) error {
	if state.UpdatedAt.IsZero() {
		state.UpdatedAt = time.Now().UTC()
	}
	if err := r.sess.Collection("instance_desired_state").Find(db.Cond{"id": state.ID}).Update(state); err != nil {
		return fmt.Errorf("failed to update instance desired state: %w", err)
	}
	return nil
}
