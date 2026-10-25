package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
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
	FindByPodIP(podIP string) (*models.Instance, error)
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

// InstanceOwnerRepository is an optional repository capability for the
// owner-scoped northbound Lite instance view. Keeping it separate avoids
// widening unrelated repository test doubles.
type InstanceOwnerRepository interface {
	GetLiteByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, error)
	CountLiteByUserIDAndOwner(userID int, owner string) (int, error)
	GetWorkbuddyProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, error)
	CountWorkbuddyProByUserIDAndOwner(userID int, owner string) (int, error)
	GetProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, error)
	CountProByUserIDAndOwner(userID int, owner string) (int, error)
}

// NorthboundInstanceOwnerRepository exposes the compatibility unified,
// owner-scoped view used by /lite-instances. It includes managed Lite runtimes
// and every Pro runtime supported by the northbound contract.
type NorthboundInstanceOwnerRepository interface {
	GetNorthboundByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, error)
	CountNorthboundByUserIDAndOwner(userID int, owner string) (int, error)
}

// IEISystemInstanceRepository is the case-insensitive owner lookup used after
// the unified platform has authenticated an email address. It intentionally
// does not depend on a ClawManager user session.
type IEISystemInstanceRepository interface {
	GetSupportedByOwnerEmail(owner string, offset, limit int) ([]models.Instance, error)
	CountSupportedByOwnerEmail(owner string) (int, error)
}

// InstanceLifecycleStatusRepository provides an atomic status transition used
// to serialize reset operations across API replicas.
type InstanceLifecycleStatusRepository interface {
	ClaimLifecycleStatus(ctx context.Context, id int, allowedStatuses []string, targetStatus string) (bool, error)
}

// InstanceFactoryResetRepository clears only instance-local runtime state. It
// deliberately preserves the instance row, audit history, usage history and
// backups so a factory reset cannot broaden into account or record deletion.
type InstanceFactoryResetRepository interface {
	ResetInstanceRuntimeData(ctx context.Context, id int) error
}

// InstanceResetReplacementRepository atomically hands a healthy replacement
// to the original owner while quarantining the source instance. It uses only
// columns and tables that already exist in deployed installations.
type InstanceResetReplacementRepository interface {
	PromoteResetReplacement(ctx context.Context, sourceID, replacementID int, owner, name, quarantineOwner, quarantineName, reason string) error
}

// InstanceQueryRepository is the optional filtered-list and aggregation
// capability used by the user workspace. It is kept separate from
// InstanceRepository so unrelated repository test doubles remain small.
type InstanceQueryRepository interface {
	GetFilteredByUserID(userID int, filter models.InstanceListFilter, offset, limit int) ([]models.Instance, error)
	CountFilteredByUserID(userID int, filter models.InstanceListFilter) (int, error)
	SummarizeByUserID(userID int) (*models.InstanceSummary, error)
}

// PendingInstanceDeletionRepository is an optional lifecycle capability used
// by the leader-only deletion reconciler. Keeping it separate avoids widening
// every InstanceRepository test double and limits retries to rows that already
// record an explicit user deletion intent.
type PendingInstanceDeletionRepository interface {
	GetByStatus(ctx context.Context, status string, limit int) ([]models.Instance, error)
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

func (r *instanceRepository) ClaimLifecycleStatus(ctx context.Context, id int, allowedStatuses []string, targetStatus string) (bool, error) {
	if id <= 0 || len(allowedStatuses) == 0 || strings.TrimSpace(targetStatus) == "" {
		return false, fmt.Errorf("invalid lifecycle status transition")
	}
	placeholders := make([]string, 0, len(allowedStatuses))
	arguments := make([]any, 0, len(allowedStatuses)+3)
	arguments = append(arguments, strings.TrimSpace(targetStatus), time.Now().UTC(), id)
	for _, status := range allowedStatuses {
		placeholders = append(placeholders, "?")
		arguments = append(arguments, strings.ToLower(strings.TrimSpace(status)))
	}
	result, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE instances
		SET status = ?, updated_at = ?
		WHERE id = ? AND LOWER(status) IN (`+strings.Join(placeholders, ",")+`)`, arguments...)
	if err != nil {
		return false, fmt.Errorf("failed to claim instance lifecycle status: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("failed to inspect instance lifecycle claim: %w", err)
	}
	return rows == 1, nil
}

// GetByProvisioningOperationID returns the instance created for a durable
// northbound operation. It remains an optional repository capability so
// existing InstanceRepository test doubles stay independent of northbound.
func (r *instanceRepository) GetByProvisioningOperationID(operationID string) (*models.Instance, error) {
	operationID = strings.TrimSpace(operationID)
	if operationID == "" {
		return nil, nil
	}
	var instance models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"provisioning_operation_id": operationID}).One(&instance)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance by provisioning operation: %w", err)
	}
	return &instance, nil
}

// FindByPodIP returns the first instance whose recorded pod_ip matches.
func (r *instanceRepository) FindByPodIP(podIP string) (*models.Instance, error) {
	podIP = strings.TrimSpace(podIP)
	if podIP == "" {
		return nil, nil
	}
	var instance models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{"pod_ip": podIP}).OrderBy("id").One(&instance)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance by pod ip: %w", err)
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

func (r *instanceRepository) GetByStatus(ctx context.Context, status string, limit int) ([]models.Instance, error) {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "" {
		return nil, fmt.Errorf("instance status is required")
	}
	var instances []models.Instance
	result := r.sess.Collection("instances").Find(db.Cond{"status": status}).OrderBy("id")
	if limit > 0 {
		result = result.Limit(limit)
	}
	err := result.All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get instances by status: %w", err)
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

func (r *instanceRepository) filteredByUserID(userID int, filter models.InstanceListFilter) db.Result {
	result := r.sess.Collection("instances").Find(db.Cond{"user_id": userID})
	if value := strings.ToLower(strings.TrimSpace(filter.Type)); value != "" {
		result = result.And(db.Cond{"type": value})
	}
	if value := strings.ToLower(strings.TrimSpace(filter.InstanceMode)); value != "" {
		result = result.And(db.Cond{"instance_mode": value})
	}
	if value := strings.ToLower(strings.TrimSpace(filter.Status)); value != "" {
		result = result.And(db.Cond{"status": value})
	} else {
		switch strings.ToLower(strings.TrimSpace(filter.Availability)) {
		case "available":
			result = result.And(db.Cond{"status": "running"})
		case "starting":
			result = result.And(db.Cond{"status": "creating"})
		case "unavailable":
			result = result.And(db.Cond{"status IN": []string{"stopped", "error", "deleting"}})
		}
	}
	if value := strings.ToLower(strings.TrimSpace(filter.Query)); value != "" {
		id, _ := strconv.Atoi(strings.TrimPrefix(value, "#"))
		if strings.HasPrefix(value, "#") {
			return result.And(db.Cond{"id": id})
		}
		pattern := "%" + value + "%"
		result = result.And(`(
			id = ? OR LOWER(name) LIKE ? OR LOWER(type) LIKE ? OR LOWER(instance_mode) LIKE ? OR
			EXISTS (
				SELECT 1
				FROM team_members tm
				JOIN teams t ON t.id = tm.team_id
				WHERE tm.instance_id = instances.id
				  AND (LOWER(t.name) LIKE ? OR LOWER(tm.display_name) LIKE ? OR LOWER(tm.member_key) LIKE ? OR LOWER(tm.role) LIKE ?)
			)
		)`, id, pattern, pattern, pattern, pattern, pattern, pattern, pattern)
	}
	return result
}

func (r *instanceRepository) GetFilteredByUserID(userID int, filter models.InstanceListFilter, offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	if err := r.filteredByUserID(userID, filter).OrderBy("-created_at", "-id").Offset(offset).Limit(limit).All(&instances); err != nil {
		return nil, fmt.Errorf("failed to get filtered instances: %w", err)
	}
	if len(instances) > 0 {
		ids := make([]int, 0, len(instances))
		for _, item := range instances {
			ids = append(ids, item.ID)
		}
		var bindings []models.InstanceRuntimeBinding
		if err := r.sess.Collection("instance_runtime_bindings").Find(db.Cond{"instance_id IN": ids}).All(&bindings); err != nil {
			return nil, err
		}
		var agents []models.InstanceAgent
		if err := r.sess.Collection("instance_agents").Find(db.Cond{"instance_id IN": ids}).All(&agents); err != nil {
			return nil, err
		}
		last := map[int]*time.Time{}
		for _, b := range bindings {
			if b.LastHealthAt != nil && (last[b.InstanceID] == nil || b.LastHealthAt.After(*last[b.InstanceID])) {
				last[b.InstanceID] = b.LastHealthAt
			}
		}
		for _, a := range agents {
			if a.LastHeartbeatAt != nil && (last[a.InstanceID] == nil || a.LastHeartbeatAt.After(*last[a.InstanceID])) {
				last[a.InstanceID] = a.LastHeartbeatAt
			}
		}
		for i := range instances {
			instances[i].LastOnlineAt = last[instances[i].ID]
			if instances[i].RuntimeErrorMessage != nil {
				message := models.SanitizeInstanceError(*instances[i].RuntimeErrorMessage)
				instances[i].RuntimeErrorMessage = &message
			}
		}
	}
	return instances, nil
}

func (r *instanceRepository) CountFilteredByUserID(userID int, filter models.InstanceListFilter) (int, error) {
	count, err := r.filteredByUserID(userID, filter).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count filtered instances: %w", err)
	}
	return int(count), nil
}

func (r *instanceRepository) SummarizeByUserID(userID int) (*models.InstanceSummary, error) {
	summary := &models.InstanceSummary{}
	row, err := r.sess.SQL().QueryRow(`
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN status = 'running' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'creating' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'stopped' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'error' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN status = 'deleting' THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(disk_gb), 0)
		FROM instances
		WHERE user_id = ?
	`, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to query instance summary: %w", err)
	}
	err = row.Scan(
		&summary.Total,
		&summary.Running,
		&summary.Creating,
		&summary.Stopped,
		&summary.Error,
		&summary.Deleting,
		&summary.AllocatedStorageGB,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to summarize instances: %w", err)
	}
	return summary, nil
}

// GetLiteByUserIDAndOwner gets Lite instances for an authenticated user and
// exact owner. The owner column uses a binary collation so comparisons are
// case-sensitive and deterministic.
func (r *instanceRepository) GetLiteByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{
		"user_id":       userID,
		"owner":         owner,
		"instance_mode": "lite",
	}).OrderBy("-created_at", "-id").Offset(offset).Limit(limit).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get owner Lite instances: %w", err)
	}
	return instances, nil
}

// CountLiteByUserIDAndOwner counts Lite instances for an authenticated user
// and exact owner.
func (r *instanceRepository) CountLiteByUserIDAndOwner(userID int, owner string) (int, error) {
	count, err := r.sess.Collection("instances").Find(db.Cond{
		"user_id":       userID,
		"owner":         owner,
		"instance_mode": "lite",
	}).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count owner Lite instances: %w", err)
	}
	return int(count), nil
}

func (r *instanceRepository) GetWorkbuddyProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(db.Cond{
		"user_id":         userID,
		"owner":           owner,
		"instance_mode":   "pro",
		"type":            "workbuddy",
		"runtime_variant": "linux",
	}).OrderBy("-created_at", "-id").Offset(offset).Limit(limit).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get owner WorkBuddy Pro instances: %w", err)
	}
	return instances, nil
}

func (r *instanceRepository) CountWorkbuddyProByUserIDAndOwner(userID int, owner string) (int, error) {
	count, err := r.sess.Collection("instances").Find(db.Cond{
		"user_id":         userID,
		"owner":           owner,
		"instance_mode":   "pro",
		"type":            "workbuddy",
		"runtime_variant": "linux",
	}).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count owner WorkBuddy Pro instances: %w", err)
	}
	return int(count), nil
}

func supportedNorthboundProOwnerInstances(userID int, owner string) db.LogicalExpr {
	return db.And(
		db.Cond{"user_id": userID, "owner": owner, "instance_mode": "pro"},
		db.Or(
			db.Cond{"type IN": []string{"openclaw", "hermes", "opencode", "deepseek-harness"}},
			db.Cond{"type": "workbuddy", "runtime_variant": "linux"},
		),
	)
}

func (r *instanceRepository) GetProByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(supportedNorthboundProOwnerInstances(userID, owner)).
		OrderBy("-created_at", "-id").Offset(offset).Limit(limit).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get owner Pro instances: %w", err)
	}
	return instances, nil
}

func (r *instanceRepository) CountProByUserIDAndOwner(userID int, owner string) (int, error) {
	count, err := r.sess.Collection("instances").Find(supportedNorthboundProOwnerInstances(userID, owner)).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count owner Pro instances: %w", err)
	}
	return int(count), nil
}

func supportedNorthboundOwnerInstances(userID int, owner string) db.LogicalExpr {
	return db.And(
		db.Cond{"user_id": userID, "owner": owner},
		db.Or(
			db.Cond{
				"instance_mode": "lite",
				"type IN":       []string{"openclaw", "hermes", "opencode", "deepseek-harness"},
			},
			db.Cond{
				"instance_mode":   "pro",
				"type":            "workbuddy",
				"runtime_variant": "linux",
			},
			db.Cond{
				"instance_mode": "pro",
				"type IN":       []string{"openclaw", "hermes", "opencode", "deepseek-harness"},
			},
		),
	)
}

func (r *instanceRepository) GetNorthboundByUserIDAndOwner(userID int, owner string, offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(supportedNorthboundOwnerInstances(userID, owner)).
		OrderBy("-created_at", "-id").Offset(offset).Limit(limit).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get owner northbound instances: %w", err)
	}
	return instances, nil
}

func (r *instanceRepository) CountNorthboundByUserIDAndOwner(userID int, owner string) (int, error) {
	count, err := r.sess.Collection("instances").Find(supportedNorthboundOwnerInstances(userID, owner)).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count owner northbound instances: %w", err)
	}
	return int(count), nil
}

func supportedIEIOwnerInstances(owner string) db.LogicalExpr {
	return db.And(
		db.Cond{"owner_normalized": strings.ToLower(strings.TrimSpace(owner))},
		db.Or(
			db.Cond{
				"instance_mode": "lite",
				"type IN":       []string{"openclaw", "hermes", "opencode", "deepseek-harness"},
			},
			db.Cond{
				"instance_mode":   "pro",
				"type":            "workbuddy",
				"runtime_variant": "linux",
			},
			db.Cond{
				"instance_mode": "pro",
				"type IN":       []string{"openclaw", "hermes", "opencode", "deepseek-harness"},
			},
		),
	)
}

func (r *instanceRepository) GetSupportedByOwnerEmail(owner string, offset, limit int) ([]models.Instance, error) {
	var instances []models.Instance
	err := r.sess.Collection("instances").Find(supportedIEIOwnerInstances(owner)).
		OrderBy("-created_at", "-id").Offset(offset).Limit(limit).All(&instances)
	if err != nil {
		return nil, fmt.Errorf("failed to get IEI owner instances: %w", err)
	}
	return instances, nil
}

func (r *instanceRepository) CountSupportedByOwnerEmail(owner string) (int, error) {
	count, err := r.sess.Collection("instances").Find(supportedIEIOwnerInstances(owner)).Count()
	if err != nil {
		return 0, fmt.Errorf("failed to count IEI owner instances: %w", err)
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
	args := make([]any, 0, len(statuses)+7)
	for _, status := range statuses {
		args = append(args, status)
	}
	args = append(args, "gateway", "lite", "openclaw", "hermes", "opencode", "deepseek-harness", limit)
	return fmt.Sprintf(`
		SELECT *
		FROM instances
		WHERE status IN (%s)
			AND LOWER(TRIM(COALESCE(description, ''))) NOT LIKE 'openclaw-upgrade-lab:%%'
			AND runtime_type = ?
			AND instance_mode = ?
			AND workspace_path IS NOT NULL
			AND TRIM(workspace_path) <> ''
			AND type IN (?, ?, ?, ?)
			AND NOT EXISTS (SELECT 1 FROM gateway_runtime_upgrades u WHERE u.instance_id=instances.id AND u.state NOT IN ('cancelled','completed'))
		ORDER BY id
		LIMIT ?
	`, statusPlaceholders), args
}

func (r *instanceRepository) UpdateRuntimeState(ctx context.Context, id int, status string, generation int, message *string) error {
	return r.updateRuntimeStateWithCode(ctx, id, status, generation, message, nil)
}

// Optional capability keeps old repository adapters compatible. The code and
// message are committed atomically with the existing generation fence.
func (r *instanceRepository) UpdateRuntimeFailure(ctx context.Context, id, generation int, message, code string) error {
	return r.updateRuntimeStateWithCode(ctx, id, "error", generation, &message, &code)
}

func (r *instanceRepository) FenceRuntimeRestart(ctx context.Context, id, expectedGeneration int) (bool, error) {
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE instances SET status = 'stopped', runtime_generation = runtime_generation + 1, runtime_error_message = NULL, runtime_error_code = NULL, updated_at = ? WHERE id = ? AND runtime_generation = ? AND status IN ('running','stopped','error') AND EXISTS (SELECT 1 FROM llm_models WHERE is_active = 1)`, time.Now().UTC(), id, expectedGeneration)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (r *instanceRepository) updateRuntimeState(ctx context.Context, id int, status string, generation int, message *string) error {
	return r.updateRuntimeStateWithCodeLocked(ctx, id, status, generation, message, nil)
}

func (r *instanceRepository) updateRuntimeStateWithCode(ctx context.Context, id int, status string, generation int, message, code *string) error {
	return r.withUnfencedInstance(ctx, id, func(local *instanceRepository) error {
		return local.updateRuntimeStateWithCodeLocked(ctx, id, status, generation, message, code)
	})
}

func (r *instanceRepository) updateRuntimeStateWithCodeLocked(ctx context.Context, id int, status string, generation int, message, code *string) error {
	res, err := r.sess.SQL().ExecContext(ctx, `
		UPDATE instances
		SET status = ?, runtime_generation = ?, runtime_error_message = ?, runtime_error_code = ?, updated_at = ?
		WHERE id = ? AND runtime_generation <= ?
		  AND (
			status <> ? OR runtime_generation <> ? OR
			NOT (runtime_error_message <=> ?) OR NOT (runtime_error_code <=> ?)
		  )
	`, status, generation, message, code, time.Now().UTC(), id, generation, status, generation, message, code)
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
	return r.withUnfencedInstance(ctx, id, func(local *instanceRepository) error {
		_, err := local.sess.SQL().ExecContext(ctx, `
		UPDATE instances
		SET workspace_path = ?, updated_at = ?
		WHERE id = ?
	`, workspacePath, time.Now().UTC(), id)
		if err != nil {
			return fmt.Errorf("failed to set instance workspace path: %w", err)
		}
		return nil
	})
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

func (r *instanceRepository) ResetInstanceRuntimeData(ctx context.Context, id int) error {
	if id <= 0 {
		return fmt.Errorf("invalid instance id")
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		// These rows describe the old runtime only. Global Skill Hub records,
		// backups, usage history, workspace audits and the instance identity are
		// intentionally outside this list.
		for _, table := range []string{
			"skill_package_materialize_jobs",
			"instance_skills",
			"instance_commands",
			"instance_desired_state",
			"instance_runtime_status",
			"instance_agents",
			"instance_config_revisions",
			"instance_external_access",
			"instance_gateway_token_aliases",
		} {
			if _, err := tx.SQL().ExecContext(ctx, "DELETE FROM "+table+" WHERE instance_id = ?", id); err != nil {
				return fmt.Errorf("failed to clear %s for instance factory reset: %w", table, err)
			}
		}
		if _, err := tx.SQL().ExecContext(ctx, `
			UPDATE instances
			SET workspace_usage_bytes = 0,
			    runtime_error_message = NULL,
			    pod_name = NULL,
			    pod_namespace = NULL,
			    pod_ip = NULL,
			    access_url = NULL,
			    access_token = NULL,
			    agent_bootstrap_token = NULL,
			    stopped_at = NULL,
			    updated_at = ?
			WHERE id = ?
		`, time.Now().UTC(), id); err != nil {
			return fmt.Errorf("failed to reset instance runtime fields: %w", err)
		}
		return nil
	}, nil)
}

func (r *instanceRepository) PromoteResetReplacement(ctx context.Context, sourceID, replacementID int, owner, name, quarantineOwner, quarantineName, reason string) error {
	if sourceID <= 0 || replacementID <= 0 || sourceID == replacementID {
		return fmt.Errorf("invalid factory-reset replacement ids")
	}
	owner = strings.TrimSpace(owner)
	name = strings.TrimSpace(name)
	quarantineOwner = strings.TrimSpace(quarantineOwner)
	quarantineName = strings.TrimSpace(quarantineName)
	if name == "" || quarantineOwner == "" || quarantineName == "" {
		return fmt.Errorf("invalid factory-reset replacement identity")
	}
	return r.sess.TxContext(ctx, func(tx db.Session) error {
		now := time.Now().UTC()
		oldResult, err := tx.SQL().ExecContext(ctx, `
			UPDATE instances
			SET owner = ?, name = ?, status = 'stopped', runtime_error_message = ?,
			    access_url = NULL, access_token = NULL, agent_bootstrap_token = NULL,
			    pod_name = NULL, pod_namespace = NULL, pod_ip = NULL,
			    stopped_at = ?, updated_at = ?
			WHERE id = ?
		`, quarantineOwner, quarantineName, reason, now, now, sourceID)
		if err != nil {
			return fmt.Errorf("failed to quarantine factory-reset source: %w", err)
		}
		if affected, affectedErr := oldResult.RowsAffected(); affectedErr != nil || affected != 1 {
			if affectedErr != nil {
				return fmt.Errorf("failed to inspect factory-reset source quarantine: %w", affectedErr)
			}
			return fmt.Errorf("factory-reset source instance not found")
		}

		newResult, err := tx.SQL().ExecContext(ctx, `
			UPDATE instances
			SET owner = ?, name = ?, updated_at = ?
			WHERE id = ? AND status = 'running'
		`, owner, name, now, replacementID)
		if err != nil {
			return fmt.Errorf("failed to promote factory-reset replacement: %w", err)
		}
		if affected, affectedErr := newResult.RowsAffected(); affectedErr != nil || affected != 1 {
			if affectedErr != nil {
				return fmt.Errorf("failed to inspect factory-reset replacement promotion: %w", affectedErr)
			}
			return fmt.Errorf("factory-reset replacement is not running")
		}

		for _, table := range []string{"instance_external_access", "instance_gateway_token_aliases"} {
			if _, err := tx.SQL().ExecContext(ctx, "DELETE FROM "+table+" WHERE instance_id = ?", sourceID); err != nil {
				return fmt.Errorf("failed to revoke factory-reset source access in %s: %w", table, err)
			}
		}
		return nil
	}, nil)
}

// Update updates an instance
func (r *instanceRepository) Update(instance *models.Instance) error {
	return r.withUnfencedInstance(context.Background(), instance.ID, func(local *instanceRepository) error {
		err := local.sess.Collection("instances").Find(db.Cond{"id": instance.ID}).Update(instance)
		if err != nil {
			return fmt.Errorf("failed to update instance: %w", err)
		}
		return nil
	})
}

// Delete deletes an instance
func (r *instanceRepository) Delete(id int) error {
	return r.withUnfencedInstance(context.Background(), id, func(local *instanceRepository) error {
		// Only terminal fences reach here. Remove them in the same transaction;
		// the FK protects active operations from accidental cascading deletion.
		if err := local.sess.Collection("gateway_runtime_upgrades").Find(db.Cond{"instance_id": id}).Delete(); err != nil {
			return err
		}
		err := local.sess.Collection("instances").Find(db.Cond{"id": id}).Delete()
		if err != nil {
			return fmt.Errorf("failed to delete instance: %w", err)
		}
		return nil
	})
}
