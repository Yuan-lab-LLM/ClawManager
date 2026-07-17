package repository

import (
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// InstanceSessionTokenAggregate summarizes token usage for one session on an instance.
type InstanceSessionTokenAggregate struct {
	SessionID        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	InvocationCount  int
	FirstSeenAt      time.Time
	LastSeenAt       time.Time
}

// ModelInvocationRepository defines repository operations for governed model calls.
type ModelInvocationRepository interface {
	Create(invocation *models.ModelInvocation) error
	GetByID(id int) (*models.ModelInvocation, error)
	ListByTraceID(traceID string) ([]models.ModelInvocation, error)
	ListBySessionID(sessionID string, limit int) ([]models.ModelInvocation, error)
	ListByUserID(userID, limit int) ([]models.ModelInvocation, error)
	ListRecent(limit int) ([]models.ModelInvocation, error)
	AggregateByInstanceSession(instanceID int, filter SessionUsageFilter) ([]InstanceSessionTokenAggregate, error)
	ListRecentByInstanceSession(instanceID int, sessionID string, limit int, filter SessionUsageFilter) ([]models.ModelInvocation, error)
	CountDistinctSessionsByInstance(instanceID int, filter SessionUsageFilter) (int, error)
}

type modelInvocationRepository struct {
	sess db.Session
}

// NewModelInvocationRepository creates a new invocation repository and ensures its table exists.
func NewModelInvocationRepository(sess db.Session) ModelInvocationRepository {
	repo := &modelInvocationRepository{sess: sess}
	repo.ensureTable()
	return repo
}

func (r *modelInvocationRepository) ensureTable() {
	const query = `
CREATE TABLE IF NOT EXISTS model_invocations (
  id INT AUTO_INCREMENT PRIMARY KEY,
  trace_id VARCHAR(100) NOT NULL,
  session_id VARCHAR(100) NULL,
  request_id VARCHAR(100) NOT NULL,
  user_id INT NULL,
  instance_id INT NULL,
  instance_mode VARCHAR(16) NULL,
  runtime_type VARCHAR(32) NULL,
  gateway_id VARCHAR(128) NULL,
  runtime_pod_id BIGINT NULL,
  model_id INT NULL,
  provider_type VARCHAR(100) NOT NULL,
  requested_model VARCHAR(255) NOT NULL,
  actual_provider_model VARCHAR(255) NOT NULL,
  traffic_class VARCHAR(50) NOT NULL,
  request_payload LONGTEXT NULL,
  response_payload LONGTEXT NULL,
  prompt_tokens INT NOT NULL DEFAULT 0,
  completion_tokens INT NOT NULL DEFAULT 0,
  total_tokens INT NOT NULL DEFAULT 0,
  cached_tokens INT NULL,
  reasoning_tokens INT NULL,
  latency_ms INT NULL,
  is_streaming BOOLEAN NOT NULL DEFAULT FALSE,
  status VARCHAR(50) NOT NULL,
  error_message TEXT NULL,
  created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
  completed_at TIMESTAMP NULL,
  INDEX idx_model_invocations_trace_id (trace_id),
  INDEX idx_model_invocations_request_id (request_id),
  INDEX idx_model_invocations_user_id (user_id),
  INDEX idx_model_invocations_instance_id (instance_id),
  INDEX idx_model_invocations_gateway_id (gateway_id),
  INDEX idx_model_invocations_model_id (model_id),
  INDEX idx_model_invocations_status (status),
  INDEX idx_model_invocations_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
`

	if _, err := r.sess.SQL().Exec(query); err != nil {
		panic(fmt.Errorf("failed to ensure model_invocations table: %w", err))
	}
	if _, err := r.sess.SQL().Exec("ALTER TABLE model_invocations ADD INDEX idx_model_invocations_session_id (session_id)"); err != nil && !isDuplicateIndexError(err) {
		panic(fmt.Errorf("failed to ensure session index on model_invocations: %w", err))
	}
}

func (r *modelInvocationRepository) Create(invocation *models.ModelInvocation) error {
	now := time.Now()
	if invocation.CreatedAt.IsZero() {
		invocation.CreatedAt = now
	}
	res, err := r.sess.Collection("model_invocations").Insert(invocation)
	if err != nil {
		return fmt.Errorf("failed to create model invocation: %w", err)
	}
	invocation.ID = int(res.ID().(int64))
	return nil
}

func (r *modelInvocationRepository) GetByID(id int) (*models.ModelInvocation, error) {
	var invocation models.ModelInvocation
	err := r.sess.Collection("model_invocations").Find(db.Cond{"id": id}).One(&invocation)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get model invocation by id: %w", err)
	}
	return &invocation, nil
}

func (r *modelInvocationRepository) ListByTraceID(traceID string) ([]models.ModelInvocation, error) {
	var items []models.ModelInvocation
	if err := r.sess.Collection("model_invocations").Find(db.Cond{"trace_id": traceID}).OrderBy("id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list model invocations by trace: %w", err)
	}
	return items, nil
}

func (r *modelInvocationRepository) ListBySessionID(sessionID string, limit int) ([]models.ModelInvocation, error) {
	var items []models.ModelInvocation
	if limit <= 0 {
		limit = 50
	}
	if err := r.sess.Collection("model_invocations").Find(db.Cond{"session_id": sessionID}).OrderBy("-created_at").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list model invocations by session: %w", err)
	}
	return items, nil
}

func (r *modelInvocationRepository) ListByUserID(userID, limit int) ([]models.ModelInvocation, error) {
	var items []models.ModelInvocation
	if limit <= 0 {
		limit = 50
	}
	if err := r.sess.Collection("model_invocations").Find(db.Cond{"user_id": userID}).OrderBy("-created_at").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list model invocations by user: %w", err)
	}
	return items, nil
}

func (r *modelInvocationRepository) ListRecent(limit int) ([]models.ModelInvocation, error) {
	var items []models.ModelInvocation
	if limit <= 0 {
		limit = 100
	}
	if err := r.sess.Collection("model_invocations").Find().OrderBy("-created_at").Limit(limit).All(&items); err != nil {
		return nil, fmt.Errorf("failed to list recent model invocations: %w", err)
	}
	return items, nil
}

func (r *modelInvocationRepository) AggregateByInstanceSession(instanceID int, filter SessionUsageFilter) ([]InstanceSessionTokenAggregate, error) {
	query := `
SELECT session_id,
       COALESCE(SUM(prompt_tokens), 0),
       COALESCE(SUM(completion_tokens), 0),
       COALESCE(SUM(total_tokens), 0),
       COUNT(*),
       MIN(created_at),
       MAX(created_at)
FROM model_invocations
WHERE instance_id = ?
  AND session_id IS NOT NULL
  AND session_id != ''
  AND status != ?`
	args := []interface{}{instanceID, models.ModelInvocationStatusBlocked}
	query, args = appendTimeFilter(query, args, filter, "created_at")
	query += `
GROUP BY session_id`
	rows, err := r.sess.SQL().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate model invocations by instance session: %w", err)
	}
	defer rows.Close()

	items := make([]InstanceSessionTokenAggregate, 0)
	for rows.Next() {
		var item InstanceSessionTokenAggregate
		if err := rows.Scan(
			&item.SessionID,
			&item.PromptTokens,
			&item.CompletionTokens,
			&item.TotalTokens,
			&item.InvocationCount,
			&item.FirstSeenAt,
			&item.LastSeenAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan session token aggregate: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate session token aggregates: %w", err)
	}
	return items, nil
}

func (r *modelInvocationRepository) ListRecentByInstanceSession(instanceID int, sessionID string, limit int, filter SessionUsageFilter) ([]models.ModelInvocation, error) {
	if limit <= 0 {
		limit = 20
	}
	query := `
SELECT id, trace_id, session_id, request_id, user_id, instance_id, instance_mode, runtime_type, gateway_id, runtime_pod_id, model_id, provider_type, requested_model, actual_provider_model, traffic_class, request_payload, response_payload, prompt_tokens, completion_tokens, total_tokens, cached_tokens, reasoning_tokens, latency_ms, is_streaming, status, error_message, created_at, completed_at
FROM model_invocations
WHERE instance_id = ?
  AND session_id = ?
  AND status != ?`
	args := []interface{}{instanceID, sessionID, models.ModelInvocationStatusBlocked}
	query, args = appendTimeFilter(query, args, filter, "created_at")
	query += `
ORDER BY created_at DESC
LIMIT ?`
	args = append(args, limit)

	rows, err := r.sess.SQL().Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to list model invocations by instance session: %w", err)
	}
	defer rows.Close()

	items := make([]models.ModelInvocation, 0, limit)
	for rows.Next() {
		var item models.ModelInvocation
		if err := rows.Scan(
			&item.ID,
			&item.TraceID,
			&item.SessionID,
			&item.RequestID,
			&item.UserID,
			&item.InstanceID,
			&item.InstanceMode,
			&item.RuntimeType,
			&item.GatewayID,
			&item.RuntimePodID,
			&item.ModelID,
			&item.ProviderType,
			&item.RequestedModel,
			&item.ActualProviderModel,
			&item.TrafficClass,
			&item.RequestPayload,
			&item.ResponsePayload,
			&item.PromptTokens,
			&item.CompletionTokens,
			&item.TotalTokens,
			&item.CachedTokens,
			&item.ReasoningTokens,
			&item.LatencyMs,
			&item.IsStreaming,
			&item.Status,
			&item.ErrorMessage,
			&item.CreatedAt,
			&item.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan model invocation by instance session: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate model invocations by instance session: %w", err)
	}
	return items, nil
}

func (r *modelInvocationRepository) CountDistinctSessionsByInstance(instanceID int, filter SessionUsageFilter) (int, error) {
	query := `
SELECT COUNT(DISTINCT session_id)
FROM model_invocations
WHERE instance_id = ?
  AND session_id IS NOT NULL
  AND session_id != ''
  AND status != ?`
	args := []interface{}{instanceID, models.ModelInvocationStatusBlocked}
	query, args = appendTimeFilter(query, args, filter, "created_at")
	row, err := r.sess.SQL().QueryRow(query, args...)
	if err != nil {
		return 0, fmt.Errorf("failed to count distinct sessions by instance: %w", err)
	}
	var count int
	if err := row.Scan(&count); err != nil {
		return 0, fmt.Errorf("failed to scan distinct session count: %w", err)
	}
	return count, nil
}

func isDuplicateIndexError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "duplicate key name") || strings.Contains(message, "already exists")
}
