package repository

import (
	"context"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

func (r *browserWorkerRepository) GetContext(ctx context.Context, instanceID int) (*models.InstanceBrowserWorker, error) {
	var config models.InstanceBrowserWorker
	err := r.sess.WithContext(ctx).Collection(config.TableName()).Find(db.Cond{"instance_id": instanceID}).One(&config)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &config, ctx.Err()
}
