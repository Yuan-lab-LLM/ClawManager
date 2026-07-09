package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

var ErrStaleRuntimeGeneration = errors.New("stale runtime generation")

// InstanceRepository defines the interface for instance data operations
type InstanceRepository interface {
	Create(instance *models.Instance) error
	GetByID(id int) (*models.Instance, error)
	GetByAccessToken(accessToken string) (*models.Instance, error)
	GetByAgentBootstrapToken(bootstrapToken string) (*models.Instance, error)
	GetAll(offset, limit int) ([]models.Instance, error)
	CountAll() (int, error)
	GetByUserID(userID int, offset, limit int) ([]models.Instance, error)
	CountByUserID(userID int) (int, error)
	CountActiveByMode(ctx context.Context, mode string) (int, error)
	ExistsByUserIDAndName(userID int, name string) (bool, error)
	GetAllRunning() ([]models.Instance, error)
	GetV2DesiredRunning(ctx context.Context, limit int) ([]models.Instance, error)
	GetV2Creating(ctx context.Context, limit int) ([]models.Instance, error)
	UpdateRuntimeState(ctx context.Context, id int, status string, generation int, message *string) error
	SetWorkspacePath(ctx context.Context, id int, workspacePath string) error
	UpdateWorkspaceUsage(ctx context.Context, id int, usageBytes int64) error
	Update(instance *models.Instance) error
	Delete(id int) error
}

// instanceRepository implements InstanceRepository
type instanceRepository struct {
	sess db.Session
}

// NewInstanceRepository creates a new instance repository
func NewInstanceRepository(sess db.Session) InstanceRepository {
	return &instanceRepository{sess: sess}
}

// Create creates a new instance
func (r *instanceRepository) Create(instance *models.Instance) error {
	res, err := r.sess.Collection("instances").Insert(instance)
	if err != nil {
		return fmt.Errorf("failed to create instance: %w", err)
	}
	// Get the generated ID
	if id, ok := res.ID().(int64); ok {
		instance.ID = int(id)
	}
	return nil
}

// GetByID gets an instance by ID
func (r *instanceRepository) GetByID(id int) (*models.Instance, error) {
	var instance models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"id": id}).One(&instance)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	return &instance, nil
}

// GetByAccessToken gets an instance by its lifecycle gateway token or a
// still-valid historical token alias.
func (r *instanceRepository) GetByAccessToken(accessToken string) (*models.Instance, error) {
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return nil, nil
	}

	var instance models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"access_token": token}).One(&instance)
	if err == nil {
		return &instance, nil
	}
	if err != db.ErrNoMoreRows {
		return nil, fmt.Errorf("failed to get instance by access token: %w", err)
	}

	return r.getByGatewayTokenAlias(token)
}

func (r *instanceRepository) getByGatewayTokenAlias(accessToken string) (*models.Instance, error) {
	tokenHash := gatewayTokenHash(accessToken)
	if tokenHash == "" {
		return nil, nil
	}

	var instance models.Instance
	now := time.Now().UTC()
	iter := r.sess.SQL().Iterator(`
		SELECT i.*
		FROM instances i
		JOIN instance_gateway_token_aliases a ON a.instance_id = i.id
		WHERE a.token_hash = ? AND a.expires_at > ?
		LIMIT 1
	`, tokenHash, now)
	defer iter.Close()
	if err := iter.One(&instance); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance by gateway token alias: %w", err)
	}

	if _, err := r.sess.SQL().Exec(`
		UPDATE instance_gateway_token_aliases
		SET last_used_at = ?, updated_at = ?
		WHERE token_hash = ?
	`, now, now, tokenHash); err != nil {
		return nil, fmt.Errorf("failed to mark gateway token alias used: %w", err)
	}
	return &instance, nil
}

func (r *instanceRepository) UpsertGatewayTokenAlias(ctx context.Context, instanceID int, accessToken string, expiresAt time.Time) error {
	tokenHash := gatewayTokenHash(accessToken)
	if instanceID <= 0 || tokenHash == "" || expiresAt.IsZero() {
		return nil
	}
	now := time.Now().UTC()
	_, err := r.sess.SQL().ExecContext(ctx, `
		INSERT INTO instance_gateway_token_aliases (instance_id, token_hash, expires_at, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			instance_id = VALUES(instance_id),
			expires_at = GREATEST(expires_at, VALUES(expires_at)),
			updated_at = VALUES(updated_at)
	`, instanceID, tokenHash, expiresAt.UTC(), now, now)
	if err != nil {
		return fmt.Errorf("failed to upsert gateway token alias: %w", err)
	}
	return nil
}

func gatewayTokenHash(accessToken string) string {
	token := strings.TrimSpace(accessToken)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}
func (r *instanceRepository) GetByAgentBootstrapToken(bootstrapToken string) (*models.Instance, error) {
	var instance models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"agent_bootstrap_token": bootstrapToken}).One(&instance)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance by agent bootstrap token: %w", err)
	}
	return &instance, nil
}

func (r *instanceRepository) GetAll(offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find().Offset(offset).Limit(limit).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get all instances: %w", err)
	}
	return instances, nil
}

func (r *instanceRepository) CountAll() (int, error) {
	count, err := r.sess.Collection("instances").Find().Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count all instances: %w", err)
	}
	return int(count), nil
}

// GetByUserID gets instances by user ID with pagination
func (r *instanceRepository) GetByUserID(userID int, offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"user_id": userID}).OrderBy("-created_at", "-id").Offset(offset).Limit(limit).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances: %w", err)
	}
	return instances, nil
}

// CountByUserID counts instances by user ID
func (r *instanceRepository) CountByUserID(userID int) (int, error) {
	count, err := r.sess.Collection("instances").Find(db.Cond{"user_id": userID}).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count instances: %w", err)
	}
	return int(count), nil
}

func (r *instanceRepository) CountActiveByMode(ctx context.Context, mode string) (int, error) {
	normalized := strings.TrimSpace(strings.ToLower(mode))
	if normalized == "" {
		return 0, nil
	}
	row, err := r.sess.SQL().QueryRowContext(ctx, `
		SELECT COUNT(*)
		FROM instances
		WHERE instance_mode = ?
			AND status IN ('creating', 'running')
	`, normalized)
	if err != nil {
		return 0, fmt.Errorf("failed to count active instances by mode: %w", err)
	}
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to scan active instances by mode count: %w", err)
	}
	return count, nil
}

// ExistsByUserIDAndName checks whether the user already has an instance with the same display name.
func (r *instanceRepository) ExistsByUserIDAndName(userID int, name string) (bool, error) {
	instances, err := r.GetByUserID(userID, 0, 1000)
	if err != nil {
		return false, err
	}

	normalized := strings.TrimSpace(strings.ToLower(name))
	for _, instance := range instances {
		if strings.TrimSpace(strings.ToLower(instance.Name)) == normalized {
			return true, nil
		}
	}

	return false, nil
}

// GetAllRunning gets all instances that are not in stopped or error state (for sync)
func (r *instanceRepository) GetAllRunning() ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(
		db.Or(
			db.Cond{"status": "running"},
			db.Cond{"status": "creating"},
			db.Cond{"status": "stopped"},
			db.Cond{"status": "error"},
		),
	).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get running instances: %w", err)
	}
	return instances, nil
}

func (r *instanceRepository) GetV2DesiredRunning(ctx context.Context, limit int) ([]models.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	var instances []models.Instance
	query, args := buildV2SchedulerInstanceQuery(v2DesiredRunningStatuses(), limit)
	iter := r.sess.SQL().IteratorContext(ctx, query, args...)
	defer iter.Close()
	if err := iter.All(&instances); err != nil {
		return nil, fmt.Errorf("failed to get v2 desired running instances: %w", err)
	}
	return instances, nil
}

func v2DesiredRunningStatuses() []string {
	return []string{"creating", "running", "error"}
}

func (r *instanceRepository) GetV2Creating(ctx context.Context, limit int) ([]models.Instance, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 100
	}
	var instances []models.Instance
	query, args := buildV2SchedulerInstanceQuery([]string{"creating"}, limit)
	iter := r.sess.SQL().IteratorContext(ctx, query, args...)
	defer iter.Close()
	if err := iter.All(&instances); err != nil {
		return nil, fmt.Errorf("failed to get v2 creating instances: %w", err)
	}
	return instances, nil
}

func buildV2SchedulerInstanceQuery(statuses []string, limit int) (string, []any) {
	if limit <= 0 {
		limit = 100
	}
	if len(statuses) == 0 {
		statuses = []string{"creating", "running"}
	}
	statusPlaceholders := strings.TrimRight(strings.Repeat("?, ", len(statuses)), ", ")
	args := make([]any, 0, len(statuses)+5)
	for _, status := range statuses {
		args = append(args, status)
	}
	args = append(args, "gateway", "lite", "openclaw", "hermes", limit)
	return fmt.Sprintf(`
		SELECT *
		FROM instances
		WHERE status IN (%s)
			AND runtime_type = ?
			AND instance_mode = ?
			AND workspace_path IS NOT NULL
			AND TRIM(workspace_path) <> ''
			AND type IN (?, ?)
		ORDER BY id
		LIMIT ?
	`, statusPlaceholders), args
}

func (r *instanceRepository) UpdateRuntimeState(ctx context.Context, id int, status string, generation int, message *string) error {
	res, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE instances
		SET status = ?, runtime_generation = ?, runtime_error_message = ?, updated_at = ?
		WHERE id = ? AND runtime_generation <= ?
	`, status, generation, message, time.Now().UTC(), id, generation)
	if err != nil {
		return fmt.Errorf("failed to update instance runtime state: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect instance runtime state update: %w", err)
	}
	if affected == 0 {
		currentGeneration, err := r.getRuntimeGeneration(ctx, id)
		if err != nil {
			return err
		}
		if currentGeneration > generation {
			return ErrStaleRuntimeGeneration
		}
	}
	return nil
}

func (r *instanceRepository) getRuntimeGeneration(ctx context.Context, id int) (int, error) {
	var currentGeneration int
	row, err := r.sess.SQL().QueryRowContext(ctx, `
		SELECT runtime_generation
		FROM instances
		WHERE id = ?
	`, id)
	if err != nil {
		return 0, fmt.Errorf("failed to query instance runtime generation: %w", err)
	}
	if err := row.Scan(&currentGeneration); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, ErrStaleRuntimeGeneration
		}
		return 0, fmt.Errorf("failed to scan instance runtime generation: %w", err)
	}
	return currentGeneration, nil
}

func (r *instanceRepository) SetWorkspacePath(ctx context.Context, id int, workspacePath string) error {
	_, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE instances
		SET workspace_path = ?, updated_at = ?
		WHERE id = ?
	`, workspacePath, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to set instance workspace path: %w", err)
	}
	return nil
}

func (r *instanceRepository) UpdateWorkspaceUsage(ctx context.Context, id int, usageBytes int64) error {
	_, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE instances
		SET workspace_usage_bytes = ?, updated_at = ?
		WHERE id = ?
	`, usageBytes, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("failed to update instance workspace usage: %w", err)
	}
	return nil
}

// Update updates an instance
func (r *instanceRepository) Update(instance *models.Instance) error {
	err := r.sess.Collection("instances").Find(db.Cond{"id": instance.ID}).Update(instance)
	if err != nil {
		return fmt.Errorf("failed to update instance: %w", err)
	}
	return nil
}

// Delete deletes an instance
func (r *instanceRepository) Delete(id int) error {
	err := r.sess.Collection("instances").Find(db.Cond{"id": id}).Delete()
	if err != nil {
		return fmt.Errorf("failed to delete instance: %w", err)
	}
	return nil
}
