package repository

import (
	"clawreef/internal/models"
	"context"
	"github.com/upper/db/v4"
)

type RoutingClassifierSettingRepository interface {
	Get(context.Context) (*models.RoutingClassifierSetting, error)
	Save(context.Context, *models.RoutingClassifierSetting) error
	SaveMode(context.Context, string) error
}
type routingClassifierSettingRepository struct{ session db.Session }

func NewRoutingClassifierSettingRepository(session db.Session) RoutingClassifierSettingRepository {
	return &routingClassifierSettingRepository{session: session}
}
func (r *routingClassifierSettingRepository) Get(ctx context.Context) (*models.RoutingClassifierSetting, error) {
	var setting models.RoutingClassifierSetting
	err := r.session.WithContext(ctx).Collection("routing_classifier_settings").Find(db.Cond{"id": 1}).One(&setting)
	if err == db.ErrNoMoreRows {
		return &models.RoutingClassifierSetting{ID: 1, TimeoutMS: 5000}, nil
	}
	return &setting, err
}
func (r *routingClassifierSettingRepository) Save(ctx context.Context, setting *models.RoutingClassifierSetting) error {
	_, err := r.session.WithContext(ctx).SQL().Exec(`INSERT INTO routing_classifier_settings (id, base_url, timeout_ms, encrypted_api_key)
	VALUES (1, ?, ?, ?) ON DUPLICATE KEY UPDATE base_url=VALUES(base_url), timeout_ms=VALUES(timeout_ms), encrypted_api_key=VALUES(encrypted_api_key)`, setting.BaseURL, setting.TimeoutMS, setting.EncryptedAPIKey)
	return err
}

// Field-specific updates prevent connection saves from overwriting a concurrent
// mode change on another replica (and vice versa).
func (r *routingClassifierSettingRepository) SaveMode(ctx context.Context, mode string) error {
	_, err := r.session.WithContext(ctx).SQL().Exec(`INSERT INTO routing_classifier_settings (id, encrypted_api_key, routing_mode)
	VALUES (1, '', ?) ON DUPLICATE KEY UPDATE routing_mode=VALUES(routing_mode)`, mode)
	return err
}
