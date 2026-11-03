package repository

import (
	"clawreef/internal/models"
	"context"
	"fmt"
	"github.com/upper/db/v4"
)

type TeamWorkspaceFileAuditRepository interface {
	Create(context.Context, *models.TeamWorkspaceFileAudit) error
}
type teamWorkspaceFileAuditRepository struct{ sess db.Session }

func NewTeamWorkspaceFileAuditRepository(sess db.Session) TeamWorkspaceFileAuditRepository {
	return &teamWorkspaceFileAuditRepository{sess: sess}
}
func (r *teamWorkspaceFileAuditRepository) Create(ctx context.Context, audit *models.TeamWorkspaceFileAudit) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := r.sess.WithContext(ctx).Collection("team_workspace_file_audits").Insert(audit); err != nil {
		return fmt.Errorf("failed to record Team file edit attempt: %w", err)
	}
	return nil
}
