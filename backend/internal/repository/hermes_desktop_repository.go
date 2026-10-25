package repository

import "github.com/upper/db/v4"

// HermesDesktopTeamGuard keeps the first Desktop Web release out of Team
// profiles, whose shared workers have different ownership semantics.
type HermesDesktopTeamGuard interface {
	IsTeamInstance(instanceID int) (bool, error)
}

type hermesDesktopTeamGuard struct{ sess db.Session }

func NewHermesDesktopTeamGuard(sess db.Session) HermesDesktopTeamGuard {
	return &hermesDesktopTeamGuard{sess: sess}
}

func (r *hermesDesktopTeamGuard) IsTeamInstance(instanceID int) (bool, error) {
	count, err := r.sess.Collection("team_members").Find(db.Cond{"instance_id": instanceID}).Count()
	return count > 0, err
}
