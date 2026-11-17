package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/historymaintenance"
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
	return r.sess.Tx(func(tx db.Session) error {
		res, err := tx.Collection("risk_hits").Insert(hit)
		if err != nil {
			return fmt.Errorf("failed to create risk hit: %w", err)
		}
		hit.ID = int(res.ID().(int64))
		if err = historymaintenance.WriteLogBatch(context.Background(), monthlySQL{tx.SQL()}, "risk_hits", []any{hit.ID}); err != nil {
			return err
		}
		return historymaintenance.ClearLogPending(context.Background(), monthlySQL{tx.SQL()}, "risk_hits", hit.ID)
	})
}

func (r *riskHitRepository) ListByTraceID(traceID string) ([]models.RiskHit, error) {
	return routedLogDetails[models.RiskHit](r.sess, "risk_hits", traceID, "", 0, 200, false)
}
