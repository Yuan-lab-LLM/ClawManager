package repository

import (
	"fmt"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// RiskHitRepository defines repository operations for sensitive-data detection records.
type RiskHitRepository interface {
	Create(hit *models.RiskHit) error
	ListByTraceID(traceID string) ([]models.RiskHit, error)
}

type riskHitRepository struct {
	sess db.Session
}

// NewRiskHitRepository creates a new risk hit repository (schema is owned by embedded migrations).
func NewRiskHitRepository(sess db.Session) RiskHitRepository {
	repo := &riskHitRepository{sess: sess}
	return repo
}

func (r *riskHitRepository) Create(hit *models.RiskHit) error {
	if hit.CreatedAt.IsZero() {
		hit.CreatedAt = time.Now()
	}
	res, err := r.sess.Collection("risk_hits").Insert(hit)
	if err != nil {
		return fmt.Errorf("failed to create risk hit: %w", err)
	}
	hit.ID = int(res.ID().(int64))
	return nil
}

func (r *riskHitRepository) ListByTraceID(traceID string) ([]models.RiskHit, error) {
	var items []models.RiskHit
	if err := r.sess.Collection("risk_hits").Find(db.Cond{"trace_id": traceID}).OrderBy("id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list risk hits by trace: %w", err)
	}
	return items, nil
}
