package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

type BrowserWorkerRepository interface {
	Get(instanceID int) (*models.InstanceBrowserWorker, error)
	Upsert(config *models.InstanceBrowserWorker) error
	ListDesired(limit int) ([]models.InstanceBrowserWorker, error)
	UpdateObserved(instanceID, observedGeneration int, status string, lastError *string) error
}

type browserWorkerRepository struct{ sess db.Session }

func NewBrowserWorkerRepository(sess db.Session) BrowserWorkerRepository {
	return &browserWorkerRepository{sess: sess}
}

func (r *browserWorkerRepository) Get(instanceID int) (*models.InstanceBrowserWorker, error) {
	var config models.InstanceBrowserWorker
	err := r.sess.Collection(config.TableName()).Find(db.Cond{"instance_id": instanceID}).One(&config)
	if err == db.ErrNoMoreRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get browser worker: %w", err)
	}
	return &config, nil
}

func (r *browserWorkerRepository) Upsert(config *models.InstanceBrowserWorker) error {
	if config == nil || config.InstanceID <= 0 {
		return fmt.Errorf("invalid browser worker config")
	}
	existing, err := r.Get(config.InstanceID)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if existing == nil {
		config.CreatedAt, config.UpdatedAt = now, now
		_, err = r.sess.Collection(config.TableName()).Insert(config)
		return err
	}
	config.Generation = existing.Generation + 1
	config.CreatedAt, config.UpdatedAt = existing.CreatedAt, now
	return r.sess.Collection(config.TableName()).Find(db.Cond{"instance_id": config.InstanceID}).Update(config)
}

func (r *browserWorkerRepository) ListDesired(limit int) ([]models.InstanceBrowserWorker, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []models.InstanceBrowserWorker
	err := r.sess.Collection("instance_browser_workers").Find().OrderBy("updated_at").Limit(limit).All(&rows)
	return rows, err
}

func (r *browserWorkerRepository) UpdateObserved(instanceID, observedGeneration int, status string, lastError *string) error {
	values := map[string]any{"observed_generation": observedGeneration, "status": status, "last_error": lastError, "updated_at": time.Now().UTC()}
	return r.sess.Collection("instance_browser_workers").Find(db.Cond{"instance_id": instanceID}).Update(values)
}
