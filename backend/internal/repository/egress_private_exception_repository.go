package repository

import (
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

// EgressPrivateExceptionRepository persists egress private-network exceptions.
type EgressPrivateExceptionRepository interface {
	List(scopeType string, scopeID *int) ([]models.EgressPrivateException, error)
	ListEnabled() ([]models.EgressPrivateException, error)
	GetByID(id int) (*models.EgressPrivateException, error)
	Create(item *models.EgressPrivateException) error
	Update(item *models.EgressPrivateException) error
	Delete(id int) error
}

type egressPrivateExceptionRepository struct {
	sess db.Session
}

// NewEgressPrivateExceptionRepository creates a repository for egress private exceptions.
func NewEgressPrivateExceptionRepository(sess db.Session) EgressPrivateExceptionRepository {
	return &egressPrivateExceptionRepository{sess: sess}
}

func (r *egressPrivateExceptionRepository) List(scopeType string, scopeID *int) ([]models.EgressPrivateException, error) {
	cond := db.Cond{}
	if scopeType = strings.TrimSpace(strings.ToLower(scopeType)); scopeType != "" {
		cond["scope_type"] = scopeType
	}
	if scopeID != nil {
		cond["scope_id"] = *scopeID
	}

	var items []models.EgressPrivateException
	err := r.sess.Collection("egress_private_exceptions").Find(cond).OrderBy("-id").All(&items)
	if err != nil {
		return nil, fmt.Errorf("failed to list egress private exceptions: %w", err)
	}
	if items == nil {
		items = []models.EgressPrivateException{}
	}
	return items, nil
}

func (r *egressPrivateExceptionRepository) ListEnabled() ([]models.EgressPrivateException, error) {
	var items []models.EgressPrivateException
	err := r.sess.Collection("egress_private_exceptions").Find(db.Cond{"enabled": true}).OrderBy("id").All(&items)
	if err != nil {
		return nil, fmt.Errorf("failed to list enabled egress private exceptions: %w", err)
	}
	if items == nil {
		items = []models.EgressPrivateException{}
	}
	return items, nil
}

func (r *egressPrivateExceptionRepository) GetByID(id int) (*models.EgressPrivateException, error) {
	var item models.EgressPrivateException
	err := r.sess.Collection("egress_private_exceptions").Find(db.Cond{"id": id}).One(&item)
	if err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get egress private exception: %w", err)
	}
	return &item, nil
}

func (r *egressPrivateExceptionRepository) Create(item *models.EgressPrivateException) error {
	now := time.Now()
	item.CreatedAt = now
	item.UpdatedAt = now
	res, err := r.sess.Collection("egress_private_exceptions").Insert(item)
	if err != nil {
		return fmt.Errorf("failed to create egress private exception: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		item.ID = int(id)
	}
	return nil
}

func (r *egressPrivateExceptionRepository) Update(item *models.EgressPrivateException) error {
	item.UpdatedAt = time.Now()
	err := r.sess.Collection("egress_private_exceptions").Find(db.Cond{"id": item.ID}).Update(item)
	if err != nil {
		return fmt.Errorf("failed to update egress private exception: %w", err)
	}
	return nil
}

func (r *egressPrivateExceptionRepository) Delete(id int) error {
	err := r.sess.Collection("egress_private_exceptions").Find(db.Cond{"id": id}).Delete()
	if err != nil {
		return fmt.Errorf("failed to delete egress private exception: %w", err)
	}
	return nil
}
