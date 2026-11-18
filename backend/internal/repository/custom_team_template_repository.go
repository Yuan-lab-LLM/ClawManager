package repository

import (
	"fmt"
	"strings"
	"time"

	"clawreef/internal/models"

	"github.com/upper/db/v4"
)

type CustomTeamTemplateRepository interface {
	Create(item *models.CustomTeamTemplate) error
	Update(item *models.CustomTeamTemplate) error
	Delete(userID, id int) error
	GetByUserIDAndID(userID, id int) (*models.CustomTeamTemplate, error)
	ListByUserID(userID int) ([]models.CustomTeamTemplate, error)
}

type customTeamTemplateRepository struct {
	sess        db.Session
	owner       *string
	operationID *string
}

func NewCustomTeamTemplateRepository(sess db.Session) CustomTeamTemplateRepository {
	return &customTeamTemplateRepository{sess: sess}
}

func NewOwnerTeamTemplateRepository(sess db.Session, owner string) (CustomTeamTemplateRepository, error) {
	normalized, err := models.NormalizeTeamOwner(owner)
	if err != nil {
		return nil, err
	}
	return &customTeamTemplateRepository{sess: sess, owner: &normalized}, nil
}

func NewOwnerTeamTemplateOperationRepository(sess db.Session, owner, operationID string) (CustomTeamTemplateRepository, error) {
	repo, err := NewOwnerTeamTemplateRepository(sess, owner)
	if err != nil {
		return nil, err
	}
	repo.(*customTeamTemplateRepository).operationID = &operationID
	return repo, nil
}

func (r *customTeamTemplateRepository) ownership(userID int) db.Cond {
	cond := db.Cond{"user_id": userID, "resource_scope": models.TeamScopeCM}
	if r.owner != nil {
		cond["resource_scope"], cond["owner"] = models.TeamScopeNorthbound, *r.owner
	}
	return cond
}

func (r *customTeamTemplateRepository) Create(item *models.CustomTeamTemplate) error {
	item.LastOperationID = r.operationID
	item.ResourceScope, item.Owner = models.TeamScopeCM, nil
	if r.owner != nil {
		item.ResourceScope, item.Owner = models.TeamScopeNorthbound, r.owner
	}
	ensureTimestamps(&item.CreatedAt, &item.UpdatedAt)
	res, err := r.sess.Collection("custom_team_templates").Insert(item)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") && strings.Contains(err.Error(), "uk_custom_team_templates_user_name") {
			return fmt.Errorf("custom team template name already exists")
		}
		return fmt.Errorf("failed to create custom team template: %w", err)
	}
	if id, ok := res.ID().(int64); ok {
		item.ID = int(id)
	}
	return nil
}

func (r *customTeamTemplateRepository) Update(item *models.CustomTeamTemplate) error {
	item.UpdatedAt = time.Now().UTC()
	// Keep the existing expected_revision contract atomic, including competing
	// model edits which began before a decoration edit finished.
	result, err := r.sess.SQL().Exec(`UPDATE custom_team_templates
		SET name=?, intent=?, requested_member_count=?, resolved_member_count=?,
		    spec_json=?, revision=?, last_trace_id=?, updated_at=?, last_operation_id=?
		WHERE id=? AND user_id=? AND revision=? AND resource_scope=? AND owner <=> ?`, item.Name, item.Intent,
		item.RequestedMemberCount, item.ResolvedMemberCount, item.SpecJSON, item.Revision,
		item.LastTraceID, item.UpdatedAt, r.operationID, item.ID, item.UserID, item.Revision-1, r.ownership(item.UserID)["resource_scope"], r.owner)
	if err != nil {
		if strings.Contains(err.Error(), "Duplicate entry") && strings.Contains(err.Error(), "uk_custom_team_templates_user_name") {
			return fmt.Errorf("custom team template name already exists")
		}
		return fmt.Errorf("failed to update custom team template: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to inspect custom team template update: %w", err)
	}
	if rows != 1 {
		return fmt.Errorf("custom team template revision conflict")
	}
	return nil
}

func (r *customTeamTemplateRepository) Delete(userID, id int) error {
	cond := r.ownership(userID)
	cond["id"] = id
	query := r.sess.Collection("custom_team_templates").Find(cond)
	count, err := query.Count()
	if err != nil {
		return fmt.Errorf("failed to find custom team template for deletion: %w", err)
	}
	if count == 0 {
		return fmt.Errorf("custom team template not found")
	}
	err = query.Delete()
	if err != nil {
		return fmt.Errorf("failed to delete custom team template: %w", err)
	}
	return nil
}

func (r *customTeamTemplateRepository) GetByUserIDAndID(userID, id int) (*models.CustomTeamTemplate, error) {
	var item models.CustomTeamTemplate
	cond := r.ownership(userID)
	cond["id"] = id
	if err := r.sess.Collection("custom_team_templates").Find(cond).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get custom team template: %w", err)
	}
	return &item, nil
}

func (r *customTeamTemplateRepository) ListByUserID(userID int) ([]models.CustomTeamTemplate, error) {
	items := []models.CustomTeamTemplate{}
	if err := r.sess.Collection("custom_team_templates").Find(r.ownership(userID)).OrderBy("-updated_at", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list custom team templates: %w", err)
	}
	return items, nil
}
