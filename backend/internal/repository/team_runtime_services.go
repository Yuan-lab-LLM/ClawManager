package repository

import (
	"clawreef/internal/models"
	"context"
	"errors"
	"github.com/upper/db/v4"
)

// Do not infer absence from an editable environment map. Multiple associations
// are ambiguous even across Teams; protected authorization refuses them.
func (r *teamRepository) GetRuntimeMemberByInstanceContext(ctx context.Context, instanceID int) (*models.TeamMember, error) {
	var members []models.TeamMember
	if err := r.sess.WithContext(ctx).Collection("team_members").Find(db.Cond{"instance_id": instanceID}).Limit(2).All(&members); err != nil {
		return nil, err
	}
	if len(members) > 1 {
		return nil, errors.New("runtime Team association is ambiguous")
	}
	if len(members) == 0 {
		return nil, ctx.Err()
	}
	return &members[0], ctx.Err()
}

// Context is propagated to the driver; authorization never detaches SQL work.
func (r *teamRepository) GetTeamByIDContext(ctx context.Context, id int) (*models.Team, error) {
	var team models.Team
	if err := r.sess.WithContext(ctx).Collection("teams").Find(db.Cond{"id": id}).One(&team); err != nil {
		return nil, err
	}
	return &team, nil
}
func (r *teamRepository) GetMemberByTeamInstanceContext(ctx context.Context, teamID, instanceID int) (*models.TeamMember, error) {
	var member models.TeamMember
	if err := r.sess.WithContext(ctx).Collection("team_members").Find(db.Cond{"team_id": teamID, "instance_id": instanceID}).One(&member); err != nil {
		return nil, err
	}
	return &member, nil
}
