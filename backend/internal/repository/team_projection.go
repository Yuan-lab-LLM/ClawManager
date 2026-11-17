package repository

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// The callback only receives a transaction-scoped repository. The Team row
// owns projection across processes and across inline/recovery workers.
type TeamProjectionRepository interface {
	WithTeamProjection(context.Context, int, string, string, bool, func(TeamRepository, *models.Team) error) (*models.Team, error)
	PreserveTeamProjection(context.Context, int, string, *models.TeamEventOutbox) (*models.Team, error)
	GetTeamProjectionWatermark(context.Context, int) (string, error)
}

var ErrTeamProjectionUnverified = errors.New("Team projection watermark has unresolved or unverified history")

func (r *teamRepository) withTransaction(run func(db.Session) error) error {
	if r.inTransaction {
		return run(r.sess)
	}
	return r.sess.Tx(run)
}

func lockTeamProjectionRow(sess db.Session, teamID int) error {
	row, err := sess.SQL().QueryRow("SELECT id FROM teams WHERE id=? FOR UPDATE", teamID)
	if err != nil {
		return err
	}
	var id int
	return row.Scan(&id)
}

func (r *teamRepository) withTeamTransaction(teamID int, run func(*teamRepository) error) error {
	return r.withTransaction(func(sess db.Session) error {
		row, err := sess.SQL().QueryRow("SELECT id FROM teams WHERE id=? FOR UPDATE", teamID)
		if err != nil {
			return err
		}
		var id int
		if err = row.Scan(&id); err != nil {
			return err
		}
		return run(&teamRepository{sess: sess, inTransaction: true, nfsWriterFences: r.nfsWriterFences})
	})
}

func (r *teamRepository) withProjectionOwner(ctx context.Context, teamID int, run func(*teamRepository, *models.Team) error) (*models.Team, error) {
	var committed *models.Team
	err := r.sess.TxContext(ctx, func(sess db.Session) error {
		row, err := sess.SQL().QueryRowContext(ctx, "SELECT id FROM teams WHERE id=? FOR UPDATE", teamID)
		if err != nil {
			return err
		}
		var id int
		if err = row.Scan(&id); err != nil {
			return err
		}
		scoped := &teamRepository{sess: sess, inTransaction: true, nfsWriterFences: r.nfsWriterFences}
		team, err := scoped.GetTeamByID(id)
		if err != nil {
			return err
		}
		if team == nil || team.Status == models.TeamStatusDeleted || team.Status == models.TeamStatusDeleting {
			return fmt.Errorf("Team is unavailable for projection")
		}
		if err = run(scoped, team); err != nil {
			return err
		}
		committed = team
		return nil
	}, nil)
	if err != nil {
		return nil, err
	}
	return committed, nil
}

func projectionIdentity(kind, identity string) []byte {
	h := sha256.Sum256([]byte(kind + ":" + identity))
	return h[:]
}

func (r *teamRepository) projectionCommitted(teamID int, stream, business string) (bool, error) {
	ids := [][]byte{projectionIdentity("stream", stream)}
	if business != "" {
		ids = append(ids, projectionIdentity("event", business))
	}
	for _, id := range ids {
		// upper/db adapts []byte as JSON rather than a binary parameter.
		// Bind hex text and convert explicitly to preserve all 32 bytes.
		row, err := r.sess.SQL().QueryRow("SELECT COUNT(*) FROM team_projection_receipts WHERE team_id=? AND identity_hash=UNHEX(?)", teamID, hex.EncodeToString(id))
		if err != nil {
			return false, err
		}
		var n int
		if err = row.Scan(&n); err != nil {
			return false, err
		}
		if n > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (r *teamRepository) WithTeamProjection(ctx context.Context, teamID int, stream, business string, advance bool, run func(TeamRepository, *models.Team) error) (*models.Team, error) {
	return r.withProjectionOwner(ctx, teamID, func(tx *teamRepository, team *models.Team) error {
		done, err := tx.projectionCommitted(teamID, stream, business)
		if err != nil {
			return err
		}
		if !done {
			// Old replay rows may describe partially committed writes. Refuse
			// even a fresh inline duplicate until those unknown effects have
			// been reconciled; a source-event row is not a completion receipt.
			legacy, err := tx.sess.Collection("team_event_outbox").Find(db.Cond{"team_id": teamID, "destination": "internal:team-event-projection", "status !=": "delivered", "source_event_id NOT LIKE": "projection-v2:%"}).Count()
			if err != nil {
				return err
			}
			if legacy > 0 {
				return fmt.Errorf("%w: legacy projection replay requires reconciliation for Team %d", ErrTeamProjectionUnverified, teamID)
			}
			if err = run(tx, team); err != nil {
				return err
			}
			identities := [][]byte{projectionIdentity("stream", stream)}
			if business != "" {
				identities = append(identities, projectionIdentity("event", business))
			}
			for _, identity := range identities {
				if _, err = tx.sess.SQL().ExecContext(ctx, "INSERT IGNORE INTO team_projection_receipts(team_id,identity_hash,stream_id) VALUES(?,UNHEX(?),?)", teamID, hex.EncodeToString(identity), stream); err != nil {
					return err
				}
			}
		}
		if advance {
			return tx.advanceProjectionCursor(ctx, team, stream)
		}
		return nil
	})
}

// Only called after the projection transaction rolled back. Persisting the
// original transport envelope and the consumed cursor is a separate atomic
// handoff; maintenance still refuses this watermark until replay commits.
func (r *teamRepository) PreserveTeamProjection(ctx context.Context, teamID int, stream string, outbox *models.TeamEventOutbox) (*models.Team, error) {
	return r.withProjectionOwner(ctx, teamID, func(tx *teamRepository, team *models.Team) error {
		done, err := tx.projectionCommitted(teamID, stream, "")
		if err != nil {
			return err
		}
		if !done {
			if err = tx.CreateEventOutbox(outbox); err != nil {
				return err
			}
		}
		return tx.advanceProjectionCursor(ctx, team, stream)
	})
}

func newerProjectionCursor(next, previous string) bool {
	a, b := strings.Split(next, "-"), strings.Split(previous, "-")
	if len(a) != 2 {
		return false
	}
	if previous == "" || previous == "0-0" {
		return true
	}
	if len(b) != 2 {
		return false
	}
	for i := 0; i < 2; i++ {
		x, e := strconv.ParseUint(a[i], 10, 64)
		if e != nil {
			return false
		}
		y, e := strconv.ParseUint(b[i], 10, 64)
		if e != nil {
			return false
		}
		if x != y {
			return x > y
		}
	}
	return false
}

func (r *teamRepository) advanceProjectionCursor(ctx context.Context, team *models.Team, next string) error {
	baseline := team.RedisEventsLastID == "" || team.RedisEventsLastID == "0-0"
	if newerProjectionCursor(next, team.RedisEventsLastID) {
		team.RedisEventsLastID = next
	}
	team.UpdatedAt = time.Now().UTC()
	// Avoid stale full-Team replacement and preserve workspace identity fencing.
	if _, err := r.sess.SQL().ExecContext(ctx, "UPDATE teams SET redis_events_last_id=?,updated_at=? WHERE id=?", team.RedisEventsLastID, team.UpdatedAt, team.ID); err != nil {
		return err
	}
	_, err := r.sess.SQL().ExecContext(ctx, `INSERT INTO team_projection_watermarks(team_id,committed_cursor,baseline_verified,updated_at) VALUES(?,?,?,?) ON DUPLICATE KEY UPDATE committed_cursor=VALUES(committed_cursor),updated_at=VALUES(updated_at)`, team.ID, team.RedisEventsLastID, baseline, team.UpdatedAt)
	return err
}

func (r *teamRepository) GetTeamProjectionWatermark(ctx context.Context, teamID int) (string, error) {
	var cursor string
	row, err := r.sess.SQL().QueryRowContext(ctx, `SELECT p.committed_cursor FROM team_projection_watermarks p JOIN teams t ON t.id=p.team_id WHERE p.team_id=? AND p.baseline_verified=1 AND p.committed_cursor=t.redis_events_last_id AND NOT EXISTS(SELECT 1 FROM team_event_outbox o WHERE o.team_id=p.team_id AND o.destination='internal:team-event-projection' AND o.status<>'delivered')`, teamID)
	if err != nil {
		return "", err
	}
	if err = row.Scan(&cursor); err != nil {
		return "", fmt.Errorf("%w: %v", ErrTeamProjectionUnverified, err)
	}
	return cursor, nil
}
