package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// ErrMixedCostCurrencies prevents a single-currency response from adding unlike amounts.
var ErrMixedCostCurrencies = errors.New("mixed cost currencies")

// NormalizeCostCurrency preserves the legacy USD default for empty currencies.
func NormalizeCostCurrency(currency string) string {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if currency == "" {
		return "USD"
	}
	return currency
}

// MergeCostCurrency validates one more fee currency; empty current means no fees yet.
func MergeCostCurrency(current, incoming string) (string, error) {
	incoming = NormalizeCostCurrency(incoming)
	if current != "" && NormalizeCostCurrency(current) != incoming {
		return "", fmt.Errorf("%w: %s and %s", ErrMixedCostCurrencies, NormalizeCostCurrency(current), incoming)
	}
	return incoming, nil
}

const normalizedCostCurrencySQL = "COALESCE(NULLIF(UPPER(TRIM(cr.currency)), ''), 'USD')"

// InstanceSessionCostAggregate summarizes cost usage for one session on an instance.
type InstanceSessionCostAggregate struct {
	SessionID        string
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
	EstimatedCost    float64
	Currency         string
}

// CostRecordRepository defines repository operations for token and money accounting.
type CostRecordRepository interface {
	Create(record *models.CostRecord) error
	ListByTraceID(traceID string) ([]models.CostRecord, error)
	ListByUserID(userID, limit int) ([]models.CostRecord, error)
	ListRecent(limit int) ([]models.CostRecord, error)
	AggregateCostByInstanceSession(instanceID int, filter SessionUsageFilter) ([]InstanceSessionCostAggregate, error)
}

type costRecordRepository struct {
	sess db.Session
}

// NewCostRecordRepository creates a new cost record repository (schema is owned by embedded migrations).
func NewCostRecordRepository(sess db.Session) CostRecordRepository {
	repo := &costRecordRepository{sess: sess}
	return repo
}

func (r *costRecordRepository) Create(record *models.CostRecord) error {
	if record.RecordedAt.IsZero() {
		record.RecordedAt = time.Now()
	}
	if handled, err := persistObservation(context.Background(), r.sess, "cost_records", record); handled {
		return err
	}
	return r.sess.Tx(func(tx db.Session) error {
		if record.TraceID != "" {
			if err := lockAuditTraces(context.Background(), tx, []string{record.TraceID}); err != nil {
				return err
			}
		}
		if err := lockObservationSourceDays(context.Background(), tx, record); err != nil {
			return err
		}
		res, err := tx.Collection("cost_records").Insert(record)
		if err != nil {
			return fmt.Errorf("failed to create cost record: %w", err)
		}
		record.ID = int(res.ID().(int64))
		return maintainObservationSummary(context.Background(), tx, "cost_records", record)
	})
}

func (r *costRecordRepository) ListByTraceID(traceID string) ([]models.CostRecord, error) {
	return routedLogDetails[models.CostRecord](r.sess, "cost_records", traceID, "", 0, 200, false)
}

func (r *costRecordRepository) ListByUserID(userID, limit int) ([]models.CostRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	return routedRecentLogs[models.CostRecord](r.sess, "cost_records", userID, limit)
}

func (r *costRecordRepository) ListRecent(limit int) ([]models.CostRecord, error) {
	return routedRecentLogs[models.CostRecord](r.sess, "cost_records", 0, limit)
}

func (r *costRecordRepository) AggregateCostByInstanceSession(instanceID int, filter SessionUsageFilter) ([]InstanceSessionCostAggregate, error) {
	query := `
SELECT cr.session_id,
       COALESCE(SUM(cr.prompt_tokens), 0),
       COALESCE(SUM(cr.completion_tokens), 0),
       COALESCE(SUM(cr.total_tokens), 0),
       COALESCE(SUM(cr.estimated_cost), 0),
       MAX(` + normalizedCostCurrencySQL + `),
       COUNT(DISTINCT ` + normalizedCostCurrencySQL + `)
FROM cost_records cr
WHERE cr.instance_id = ?
  AND cr.session_id IS NOT NULL
  AND cr.session_id != ''`
	args := []interface{}{instanceID}
	query, args = appendEligibleCostInvocationFilter(query, args, filter)
	query += `
GROUP BY cr.session_id`
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := r.sess.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate cost records by instance session: %w", err)
	}
	defer rows.Close()

	items := make([]InstanceSessionCostAggregate, 0)
	currency := ""
	for rows.Next() {
		var item InstanceSessionCostAggregate
		var currencyCount int
		if err := rows.Scan(
			&item.SessionID,
			&item.PromptTokens,
			&item.CompletionTokens,
			&item.TotalTokens,
			&item.EstimatedCost,
			&item.Currency,
			&currencyCount,
		); err != nil {
			return nil, fmt.Errorf("failed to scan session cost aggregate: %w", err)
		}
		if currencyCount > 1 {
			return nil, fmt.Errorf("%w in session %s", ErrMixedCostCurrencies, item.SessionID)
		}
		currency, err = MergeCostCurrency(currency, item.Currency)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate session cost aggregates: %w", err)
	}
	return items, nil
}

// appendEligibleCostInvocationFilter counts each fee once. Exact associations never
// fall back to a different attempt; legacy fees require a matching scoped trace.
func appendEligibleCostInvocationFilter(query string, args []interface{}, filter SessionUsageFilter) (string, []interface{}) {
	query += ` AND ((cr.invocation_id IS NOT NULL AND EXISTS (SELECT 1 FROM observation_invocation_summaries mi WHERE mi.id=cr.invocation_id AND mi.instance_id=cr.instance_id AND mi.session_id=cr.session_id AND mi.status!=?`
	args = append(args, models.ModelInvocationStatusBlocked)
	query, args = appendTimeFilter(query, args, filter, "mi.created_at")
	query += `)) OR (cr.invocation_id IS NULL AND EXISTS (SELECT 1 FROM observation_invocation_summaries mi WHERE mi.instance_id=cr.instance_id AND mi.session_id=cr.session_id AND mi.trace_id=cr.trace_id AND mi.status!=?`
	args = append(args, models.ModelInvocationStatusBlocked)
	query, args = appendTimeFilter(query, args, filter, "mi.created_at")
	return query + ")))", args
}

// AggregateModelCostByInstanceSession reports full-window fees by their recorded model identity.
func (r *costRecordRepository) AggregateModelCostByInstanceSession(instanceID int, sessionID string, filter SessionUsageFilter) ([]models.CostRecord, error) {
	query := `SELECT cr.model_name, COALESCE(SUM(cr.estimated_cost), 0), ` + normalizedCostCurrencySQL + `
FROM cost_records cr WHERE cr.instance_id = ? AND cr.session_id = ?`
	query, args := appendEligibleCostInvocationFilter(query, []interface{}{instanceID, sessionID}, filter)
	query += " GROUP BY cr.model_name, " + normalizedCostCurrencySQL
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	rows, err := r.sess.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to aggregate session model costs: %w", err)
	}
	defer rows.Close()
	items := make([]models.CostRecord, 0)
	currency := ""
	for rows.Next() {
		var item models.CostRecord
		if err := rows.Scan(&item.ModelName, &item.EstimatedCost, &item.Currency); err != nil {
			return nil, fmt.Errorf("failed to scan session model cost: %w", err)
		}
		currency, err = MergeCostCurrency(currency, item.Currency)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("failed to iterate session model costs: %w", err)
	}
	return items, nil
}
