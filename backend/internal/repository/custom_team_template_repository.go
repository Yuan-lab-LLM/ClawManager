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
	sess db.Session
}

func NewCustomTeamTemplateRepository(sess db.Session) CustomTeamTemplateRepository {
	return &customTeamTemplateRepository{sess: sess}
}

func (r *customTeamTemplateRepository) Create(item *models.CustomTeamTemplate) error {
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
		    spec_json=?, revision=?, last_trace_id=?, updated_at=?
		WHERE id=? AND user_id=? AND revision=?`, item.Name, item.Intent,
		item.RequestedMemberCount, item.ResolvedMemberCount, item.SpecJSON, item.Revision,
		item.LastTraceID, item.UpdatedAt, item.ID, item.UserID, item.Revision-1)
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
	query := r.sess.Collection("custom_team_templates").Find(db.Cond{"id": id, "user_id": userID})
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
	if err := r.sess.Collection("custom_team_templates").Find(db.Cond{"id": id, "user_id": userID}).One(&item); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get custom team template: %w", err)
	}
	return &item, nil
}

func (r *customTeamTemplateRepository) ListByUserID(userID int) ([]models.CustomTeamTemplate, error) {
	items := []models.CustomTeamTemplate{}
	if err := r.sess.Collection("custom_team_templates").Find(db.Cond{"user_id": userID}).OrderBy("-updated_at", "-id").All(&items); err != nil {
		return nil, fmt.Errorf("failed to list custom team templates: %w", err)
	}
	return items, nil
}
