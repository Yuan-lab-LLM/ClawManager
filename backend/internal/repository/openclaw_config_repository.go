package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// OpenClawConfigRepository defines storage operations for the OpenClaw config center.
type OpenClawConfigRepository interface {
	ListResources(userID int, resourceType string) ([]models.OpenClawConfigResource, error)
	GetResourceByID(id int) (*models.OpenClawConfigResource, error)
	GetResourceByUserTypeKey(userID int, resourceType, resourceKey string) (*models.OpenClawConfigResource, error)
	CreateResource(resource *models.OpenClawConfigResource) error
	UpdateResource(resource *models.OpenClawConfigResource) error
	DeleteResource(id int) error

	ListBundles(userID int) ([]models.OpenClawConfigBundle, error)
	GetBundleByID(id int) (*models.OpenClawConfigBundle, error)
	CreateBundle(bundle *models.OpenClawConfigBundle) error
	UpdateBundle(bundle *models.OpenClawConfigBundle) error
	DeleteBundle(id int) error
	ListBundleItems(bundleID int) ([]models.OpenClawConfigBundleItem, error)
	ReplaceBundleItems(bundleID int, items []models.OpenClawConfigBundleItem) error

	CreateSnapshot(snapshot *models.OpenClawInjectionSnapshot) error
	UpdateSnapshot(snapshot *models.OpenClawInjectionSnapshot) error
	GetSnapshotByID(id int) (*models.OpenClawInjectionSnapshot, error)
	ListSnapshotsByUser(userID int, limit int) ([]models.OpenClawInjectionSnapshot, error)
	ListActiveSnapshots(userID int) ([]models.OpenClawInjectionSnapshot, error)
	UpdateSnapshotIfUnchanged(snapshot *models.OpenClawInjectionSnapshot, expectedUpdatedAt time.Time) (bool, error)
}

type openClawConfigRepository struct {
	sess db.Session
}

// NewOpenClawConfigRepository creates a new OpenClaw config repository.
func NewOpenClawConfigRepository(sess db.Session) OpenClawConfigRepository {
	return &openClawConfigRepository{sess: sess}
}

func (r *openClawConfigRepository) ListResources(userID int, resourceType string) ([]models.OpenClawConfigResource, error) {
	find := r.sess.Collection("openclaw_config_resources").Find(db.Cond{"user_id": userID})
	if resourceType != "" {
		find = find.And("resource_type", resourceType)
	}

	var items []models.OpenClawConfigResource
	if err := find.OrderBy("-updated_at", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list openclaw config resources: %w", err)
	}
	return items, nil
}

func (r *openClawConfigRepository) GetResourceByID(id int) (*models.OpenClawConfigResource, error) {
	var item models.OpenClawConfigResource
	if err := r.sess.Collection("openclaw_config_resources").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get openclaw config resource: %w", err)
	}
	return &item, nil
}

func (r *openClawConfigRepository) GetResourceByUserTypeKey(userID int, resourceType, resourceKey string) (*models.OpenClawConfigResource, error) {
	var item models.OpenClawConfigResource
	if err := r.sess.Collection("openclaw_config_resources").Find(db.Cond{
		"user_id":       userID,
		"resource_type": resourceType,
		"resource_key":  resourceKey,
	}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get openclaw config resource by key: %w", err)
	}
	return &item, nil
}

func (r *openClawConfigRepository) CreateResource(resource *models.OpenClawConfigResource) error {
	res, err := r.sess.Collection("openclaw_config_resources").Insert(resource)
	if err != nil {
		return fmt.Errorf("failed to create openclaw config resource: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		resource.ID = int(id)
	}
	return nil
}

func (r *openClawConfigRepository) UpdateResource(resource *models.OpenClawConfigResource) error {
	if err := r.sess.Collection("openclaw_config_resources").Find(db.Cond{"id": resource.ID}).Update(resource); err != nil {
		return fmt.Errorf("failed to update openclaw config resource: %w", err)
	}
	return nil
}

func (r *openClawConfigRepository) DeleteResource(id int) error {
	if err := r.sess.Collection("openclaw_config_resources").Find(db.Cond{"id": id}).Delete(); err != nil {
		return fmt.Errorf("failed to delete openclaw config resource: %w", err)
	}
	return nil
}

func (r *openClawConfigRepository) ListBundles(userID int) ([]models.OpenClawConfigBundle, error) {
	var items []models.OpenClawConfigBundle
	if err := r.sess.Collection("openclaw_config_bundles").Find(db.Cond{"user_id": userID}).OrderBy("-updated_at", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list openclaw config bundles: %w", err)
	}
	return items, nil
}

func (r *openClawConfigRepository) GetBundleByID(id int) (*models.OpenClawConfigBundle, error) {
	var item models.OpenClawConfigBundle
	if err := r.sess.Collection("openclaw_config_bundles").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get openclaw config bundle: %w", err)
	}
	return &item, nil
}

func (r *openClawConfigRepository) CreateBundle(bundle *models.OpenClawConfigBundle) error {
	res, err := r.sess.Collection("openclaw_config_bundles").Insert(bundle)
	if err != nil {
		return fmt.Errorf("failed to create openclaw config bundle: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		bundle.ID = int(id)
	}
	return nil
}

func (r *openClawConfigRepository) UpdateBundle(bundle *models.OpenClawConfigBundle) error {
	if err := r.sess.Collection("openclaw_config_bundles").Find(db.Cond{"id": bundle.ID}).Update(bundle); err != nil {
		return fmt.Errorf("failed to update openclaw config bundle: %w", err)
	}
	return nil
}

func (r *openClawConfigRepository) DeleteBundle(id int) error {
	if err := r.sess.Collection("openclaw_config_bundles").Find(db.Cond{"id": id}).Delete(); err != nil {
		return fmt.Errorf("failed to delete openclaw config bundle: %w", err)
	}
	return nil
}

func (r *openClawConfigRepository) ListBundleItems(bundleID int) ([]models.OpenClawConfigBundleItem, error) {
	var items []models.OpenClawConfigBundleItem
	if err := r.sess.Collection("openclaw_config_bundle_items").Find(db.Cond{"bundle_id": bundleID}).OrderBy("sort_order", "id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list openclaw config bundle items: %w", err)
	}
	return items, nil
}

func (r *openClawConfigRepository) ReplaceBundleItems(bundleID int, items []models.OpenClawConfigBundleItem) error {
	if err := r.sess.Collection("openclaw_config_bundle_items").Find(db.Cond{"bundle_id": bundleID}).Delete(); err != nil && err != db.ErrNoMoreRows {
		return fmt.Errorf("failed to delete existing openclaw config bundle items: %w", err)
	}

	for _, item := range items {
		item.BundleID = bundleID
		if _, err := r.sess.Collection("openclaw_config_bundle_items").Insert(item); err != nil {
			return fmt.Errorf("failed to add openclaw config bundle item: %w", err)
		}
	}

	return nil
}

func (r *openClawConfigRepository) CreateSnapshot(snapshot *models.OpenClawInjectionSnapshot) error {
	res, err := r.sess.Collection("openclaw_injection_snapshots").Insert(snapshot)
	if err != nil {
		return fmt.Errorf("failed to create openclaw injection snapshot: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		snapshot.ID = int(id)
	}
	return nil
}

func (r *openClawConfigRepository) UpdateSnapshot(snapshot *models.OpenClawInjectionSnapshot) error {
	if err := r.sess.Collection("openclaw_injection_snapshots").Find(db.Cond{"id": snapshot.ID}).Update(snapshot); err != nil {
		return fmt.Errorf("failed to update openclaw injection snapshot: %w", err)
	}
	return nil
}

func (r *openClawConfigRepository) GetSnapshotByID(id int) (*models.OpenClawInjectionSnapshot, error) {
	var item models.OpenClawInjectionSnapshot
	if err := r.sess.Collection("openclaw_injection_snapshots").Find(db.Cond{"id": id}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get openclaw injection snapshot: %w", err)
	}
	return &item, nil
}

func (r *openClawConfigRepository) ListSnapshotsByUser(userID int, limit int) ([]models.OpenClawInjectionSnapshot, error) {
	if limit <= 0 {
		limit = 50
	}

	var items []models.OpenClawInjectionSnapshot
	if err := r.sess.Collection("openclaw_injection_snapshots").Find(db.Cond{"user_id": userID}).OrderBy("-created_at", "-id").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list openclaw injection snapshots: %w", err)
	}
	return items, nil
}

func (r *openClawConfigRepository) ListActiveSnapshots(userID int) ([]models.OpenClawInjectionSnapshot, error) {
	var items []models.OpenClawInjectionSnapshot
	if err := r.sess.Collection("openclaw_injection_snapshots").Find(db.Cond{
		"user_id":   userID,
		"status IN": []string{"compiled", "active"},
	}).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list active openclaw injection snapshots: %w", err)
	}
	return items, nil
}

func (r *openClawConfigRepository) UpdateSnapshotIfUnchanged(
	snapshot *models.OpenClawInjectionSnapshot,
	expectedUpdatedAt time.Time,
) (bool, error) {
	result, err := r.sess.SQL().Exec(
		`UPDATE openclaw_injection_snapshots
		 SET resolved_resources_json = ?,
		     rendered_env_json = ?,
		     rendered_manifest_json = ?,
		     updated_at = ?
		 WHERE id = ? AND updated_at = ?`,
		snapshot.ResolvedResourcesJSON,
		snapshot.RenderedEnvJSON,
		snapshot.RenderedManifestJSON,
		snapshot.UpdatedAt,
		snapshot.ID,
		expectedUpdatedAt,
	)
	if err != nil {
		return false, fmt.Errorf("failed to cas-update openclaw injection snapshot: %w", err)
	}
	rows, _ := result.RowsAffected()
	return rows > 0, nil
}
