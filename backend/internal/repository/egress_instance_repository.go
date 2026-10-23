package repository

import (
	"context"
	"fmt"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// EgressInstanceRepository only resolves authenticated proxy identities.
// Pod IP is deliberately excluded because IPs can be recycled across owners.
type EgressInstanceRepository interface {
	GetByIDContext(context.Context, int) (*models.Instance, error)
}

func NewEgressInstanceRepository(sess db.Session) EgressInstanceRepository {
	return &instanceRepository{sess: sess}
}

func (r *instanceRepository) GetByIDContext(ctx context.Context, id int) (*models.Instance, error) {
	var instance models.Instance
	err := r.sess.WithContext(ctx).Collection("instances").Find(db.Cond{"id": id}).One(&instance)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to resolve egress instance: %w", err)
	}
	return &instance, nil
}
