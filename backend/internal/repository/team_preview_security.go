package repository

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"github.com/upper/db/v4"
	"time"
)

// ProtectTeamPreview permanently retires legacy signing for the entire Team.
// Binary comparison keeps unknown future/case/whitespace markers fail-closed.
func (r *teamRepository) ProtectTeamPreview(ctx context.Context, id, user int) error {
	if id < 1 || user < 1 {
		return errors.New("invalid Team preview transition")
	}
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE teams SET preview_security_profile = ?, updated_at = ? WHERE id = ? AND user_id = ? AND status = 'running' AND CAST(preview_security_profile AS BINARY) IN ('', ?)`, models.TeamPreviewSecurityProfile, time.Now().UTC(), id, user, models.TeamPreviewSecurityProfile)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		team, err := r.GetTeamByIDContext(ctx, id)
		if err != nil || team.UserID != user || team.Status != models.TeamStatusRunning || team.PreviewSecurityProfile != models.TeamPreviewSecurityProfile {
			return errors.New("Team preview transition lost authority")
		}
	}
	return nil
}

// Limit bounds both SQL materialization and migration authorization work. The
// extra row lets the caller distinguish a full page from an oversized Team.
func (r *teamRepository) ListMembersByTeamIDContext(ctx context.Context, teamID int) ([]models.TeamMember, error) {
	var members []models.TeamMember
	err := r.sess.WithContext(ctx).Collection("team_members").Find(db.Cond{"team_id": teamID}).Limit(257).All(&members)
	return members, err
}
