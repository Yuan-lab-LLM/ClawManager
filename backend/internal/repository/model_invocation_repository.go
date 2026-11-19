package repository

import (
	"context"
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

// NewModelInvocationRepository creates a new invocation repository (schema is owned by embedded migrations).
func NewModelInvocationRepository(sess db.Session) ModelInvocationRepository {
	repo := &modelInvocationRepository{sess: sess}
	return repo
}

func (r *modelInvocationRepository) Create(invocation *models.ModelInvocation) error {
	now := time.Now()
	if invocation.CreatedAt.IsZero() {
		invocation.CreatedAt = now
	}
	if handled, err := persistObservation(context.Background(), r.sess, "model_invocations", invocation); handled {
		return err
	}
	return r.sess.Tx(func(tx db.Session) error {
		if invocation.TraceID != "" {
			if err := lockAuditTraces(context.Background(), tx, []string{invocation.TraceID}); err != nil {
				return err
			}
		}
		if err := lockObservationSourceDays(context.Background(), tx, invocation); err != nil {
			return err
		}
		res, err := tx.Collection("model_invocations").Insert(invocation)
		if err != nil {
			return fmt.Errorf("failed to create model invocation: %w", err)
		}
		invocation.ID = int(res.ID().(int64))
		return maintainObservationSummary(context.Background(), tx, "model_invocations", invocation)
	})
}

func (r *modelInvocationRepository) GetByID(id int) (*models.ModelInvocation, error) {
	if item, handled, err := monthlyByID[models.ModelInvocation](context.Background(), r.sess, "model_invocations", id); handled || err != nil {
		return item, err
	}
	var invocation models.ModelInvocation
	err := r.sess.Collection("model_invocations").Find(db.Cond{"id": id}).One(&invocation)
	if err != nil {
		if err == db.ErrNoMoreRows {
			archived, archiveErr := archivedByID[models.ModelInvocation](r.sess, "model_invocations", id)
			if archiveErr != nil {
				return nil, archiveErr
			}
			if archived != nil {
				if err := hydrateObservation(context.Background(), archived); err != nil {
					return nil, err
				}
			}
			return archived, nil
		}
		return nil, fmt.Errorf("failed to get model invocation by id: %w", err)
	}
	if err := hydrateObservation(context.Background(), &invocation); err != nil {
		return nil, err
	}
	return &invocation, nil
}

func (r *modelInvocationRepository) ListByTraceID(traceID string) ([]models.ModelInvocation, error) {
	return routedLogDetails[models.ModelInvocation](r.sess, "model_invocations", traceID, "", 0, 200, false)
}

func (r *modelInvocationRepository) ListBySessionID(sessionID string, limit int) ([]models.ModelInvocation, error) {
	if limit <= 0 {
		limit = 50
	}
	return routedLogDetails[models.ModelInvocation](r.sess, "model_invocations", "", sessionID, 0, limit, true)
}

func (r *modelInvocationRepository) ListByUserID(userID, limit int) ([]models.ModelInvocation, error) {
	if limit <= 0 {
		limit = 50
	}
	return routedRecentLogs[models.ModelInvocation](r.sess, "model_invocations", userID, limit)
}

func (r *modelInvocationRepository) ListRecent(limit int) ([]models.ModelInvocation, error) {
	return routedRecentLogs[models.ModelInvocation](r.sess, "model_invocations", 0, limit)
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
FROM observation_invocation_summaries
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
	if err := hydrateObservations(context.Background(), items); err != nil {
		return nil, err
	}
	return items, nil
}

func (r *modelInvocationRepository) ListRecentByInstanceSession(instanceID int, sessionID string, limit int, filter SessionUsageFilter) ([]models.ModelInvocation, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 200 {
		limit = 200
	}
	cond := db.Cond{"instance_id": instanceID, "session_id": sessionID, "status !=": models.ModelInvocationStatusBlocked}
	if filter.Since != nil {
		cond["created_at >="] = *filter.Since
	}
	if filter.Until != nil {
		cond["created_at <"] = *filter.Until
	}
	var items []models.ModelInvocation
	columns := []interface{}{}
	for _, column := range traceColumns("model_invocations") {
		columns = append(columns, column)
	}
	err := r.sess.Collection("observation_invocation_summaries").Find(cond).Select(columns...).OrderBy("-created_at", "-id").Limit(limit).All(&items)
	return items, err
}

func (r *modelInvocationRepository) CountDistinctSessionsByInstance(instanceID int, filter SessionUsageFilter) (int, error) {
	query := `
SELECT COUNT(DISTINCT session_id)
FROM observation_invocation_summaries
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

// AggregateModelsByInstanceSession keeps model token totals independent of the recent trace page.
func (r *modelInvocationRepository) AggregateModelsByInstanceSession(instanceID int, sessionID string, filter SessionUsageFilter) ([]models.ModelInvocation, error) {
	query := `SELECT COALESCE(NULLIF(TRIM(actual_provider_model), ''), requested_model),
COALESCE(SUM(prompt_tokens), 0), COALESCE(SUM(completion_tokens), 0), COALESCE(SUM(total_tokens), 0)
FROM observation_invocation_summaries WHERE instance_id = ? AND session_id = ? AND status != ?`
	query, args := appendTimeFilter(query, []interface{}{instanceID, sessionID, models.ModelInvocationStatusBlocked}, filter, "created_at")
	query += " GROUP BY COALESCE(NULLIF(TRIM(actual_provider_model), ''), requested_model)"
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := r.sess.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate session models: %w", err)
	}
	defer rows.Close()
	items := make([]models.ModelInvocation, 0)
	for rows.Next() {
		var item models.ModelInvocation
		if err := rows.Scan(&item.ActualProviderModel, &item.PromptTokens, &item.CompletionTokens, &item.TotalTokens); err != nil {
			return nil, fmt.Errorf("failed to scan session model: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate session models: %w", err)
	}
	if err := hydrateObservations(context.Background(), items); err != nil {
		return nil, err
	}
	return items, nil
}
