package repository

import (
	"context"
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type InstanceAgentRepository interface {
	GetByInstanceID(instanceID int) (*models.InstanceAgent, error)
	GetBySessionToken(sessionToken string) (*models.InstanceAgent, error)
	Create(agent *models.InstanceAgent) error
	Update(agent *models.InstanceAgent) error
}

type instanceAgentRepository struct {
	sess db.Session
}

func NewInstanceAgentRepository(sess db.Session) InstanceAgentRepository {
	return &instanceAgentRepository{sess: sess}
}

func (r *instanceAgentRepository) GetByInstanceID(instanceID int) (*models.InstanceAgent, error) {
	var item models.InstanceAgent
	if err := r.sess.Collection("instance_agents").Find(db.Cond{"instance_id": instanceID}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance agent: %w", err)
	}
	return &item, nil
}

func (r *instanceAgentRepository) GetBySessionToken(sessionToken string) (*models.InstanceAgent, error) {
	var item models.InstanceAgent
	if err := r.sess.Collection("instance_agents").Find(db.Cond{"session_token": sessionToken}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get instance agent by session token: %w", err)
	}
	if err := checkLegacyInstanceAgent(context.Background(), r.sess, item.InstanceID); err != nil {
		return nil, err
	}
	return &item, nil
}

func (r *instanceAgentRepository) Create(agent *models.InstanceAgent) error {
	if agent == nil {
		return fmt.Errorf("invalid instance agent")
	}
	return (&instanceRepository{r.sess}).withInstanceRowLock(context.Background(), agent.InstanceID, func(tx *instanceRepository) error {
		if err := checkLegacyInstanceAgent(context.Background(), tx.sess, agent.InstanceID); err != nil {
			return err
		}
		return (&instanceAgentRepository{tx.sess}).create(agent)
	})
}
func (r *instanceAgentRepository) create(agent *models.InstanceAgent) error {
	ensureTimestamps(&agent.CreatedAt, &agent.UpdatedAt)
	res, err := r.sess.Collection("instance_agents").Insert(agent)
	if err != nil {
		return fmt.Errorf("failed to create instance agent: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		agent.ID = int(id)
	}
	return nil
}

func (r *instanceAgentRepository) Update(agent *models.InstanceAgent) error {
	if agent == nil {
		return fmt.Errorf("invalid instance agent")
	}
	return (&instanceRepository{r.sess}).withInstanceRowLock(context.Background(), agent.InstanceID, func(tx *instanceRepository) error {
		if err := checkLegacyInstanceAgent(context.Background(), tx.sess, agent.InstanceID); err != nil {
			return err
		}
		return (&instanceAgentRepository{tx.sess}).update(agent)
	})
}
func (r *instanceAgentRepository) update(agent *models.InstanceAgent) error {
	if agent.UpdatedAt.IsZero() {
		agent.UpdatedAt = time.Now().UTC()
	}
	if err := r.sess.Collection("instance_agents").Find(db.Cond{"id": agent.ID}).Update(agent); err != nil {
		return fmt.Errorf("failed to update instance agent: %w", err)
	}
	return nil
}

func checkLegacyInstanceAgent(ctx context.Context, s db.Session, id int) error {
	row, err := s.SQL().QueryRowContext(ctx, `SELECT instance_mode,runtime_type,status,type FROM instances WHERE id=?`, id)
	if err != nil {
		return err
	}
	var mode, runtime, status, typ string
	if err = row.Scan(&mode, &runtime, &status, &typ); err != nil {
		return err
	}
	if strings.EqualFold(strings.TrimSpace(mode), "lite") {
		native := false
		switch strings.ToLower(strings.TrimSpace(typ)) {
		case "openclaw", "hermes", "opencode", "deepseek-harness":
			native = true
		}
		if native || strings.EqualFold(strings.TrimSpace(runtime), "gateway") || (status != "running" && status != "creating") {
			return models.ErrInstanceStopping
		}
		return checkInstanceAutoStopMutation(ctx, s, id)
	}
	return nil
}
