package repository

import (
	"clawreef/internal/models"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/upper/db/v4"
	"strings"
	"unicode/utf8"
)

type GatewayTaskRepository interface {
	Check(context.Context, models.GatewayTaskIdentity) (*models.GatewayTaskState, error)
	Bind(context.Context, models.GatewayTaskIdentity, int) error
	Finish(context.Context, models.GatewayTaskIdentity, bool) error
}

type gatewayTaskRepository struct{ sess db.Session }

func NewGatewayTaskRepository(sess db.Session) GatewayTaskRepository {
	return &gatewayTaskRepository{sess}
}

func GatewayTaskKey(t models.GatewayTaskIdentity) (string, error) {
	if t.InstanceID <= 0 || t.UserID <= 0 || t.Generation < 0 {
		return "", fmt.Errorf("invalid task owner")
	}
	for _, id := range []string{t.SessionID, t.TaskID} {
		if strings.TrimSpace(id) != id || id == "" || !utf8.ValidString(id) || len(id) > 191 || strings.ContainsAny(id, "\r\n\x00") {
			return "", fmt.Errorf("invalid runtime task identity")
		}
	}
	raw, _ := json.Marshal([]any{t.InstanceID, t.UserID, t.Generation, t.SessionID, t.TaskID})
	hash := sha256.Sum256(raw)
	return hex.EncodeToString(hash[:]), nil
}

// This update also runs INSIDE model-policy transactions. A revoke followed by
// a rapid regrant cannot revive a turn, even when no runtime polled in between.
func revokeUnauthorizedTasks(sess db.Session, modelID int) error {
	_, err := sess.SQL().Exec(`UPDATE gateway_runtime_tasks t
 JOIN gateway_runtime_task_models b ON b.task_key=t.task_key
 LEFT JOIN llm_models m ON m.id=b.model_id
 LEFT JOIN users u ON u.id=t.owner_user_id
 SET t.revoked=TRUE
 WHERE t.finished=FALSE AND t.revoked=FALSE AND (?=0 OR b.model_id=?) AND
 (m.id IS NULL OR m.is_active=FALSE OR u.id IS NULL OR u.is_active=FALSE OR
 NOT (m.visibility_scope='all' OR (m.visibility_scope='selected_users' AND EXISTS
 (SELECT 1 FROM llm_model_user_grants g WHERE g.model_id=m.id AND g.user_id=t.owner_user_id))))`, modelID, modelID)
	return err
}

func (r *gatewayTaskRepository) Check(ctx context.Context, t models.GatewayTaskIdentity) (*models.GatewayTaskState, error) {
	key, err := GatewayTaskKey(t)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Instance generation and owner are checked again in SQL, covering restarts
	// and ownership changes racing with middleware authentication.
	_, err = r.sess.SQL().ExecContext(ctx, `INSERT IGNORE INTO gateway_runtime_tasks
 (task_key,instance_id,owner_user_id,generation,session_id,task_id)
 SELECT ?,i.id,i.user_id,?,?,? FROM instances i JOIN users u ON u.id=i.user_id
 WHERE i.id=? AND i.user_id=? AND i.runtime_generation=? AND u.is_active=TRUE`, key, t.Generation, t.SessionID, t.TaskID, t.InstanceID, t.UserID, t.Generation)
	if err != nil {
		return nil, err
	}
	_, err = r.sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_tasks t
 LEFT JOIN gateway_runtime_tasks previous ON previous.instance_id=t.instance_id AND previous.owner_user_id=t.owner_user_id
 AND previous.session_id=t.session_id AND previous.task_id=t.task_id AND previous.generation<t.generation AND previous.revoked=TRUE
 SET t.revoked=TRUE WHERE t.task_key=? AND t.finished=FALSE AND t.revoked=FALSE AND
 (NOT EXISTS (SELECT 1 FROM instances i JOIN users u ON u.id=i.user_id AND u.is_active=TRUE
 WHERE i.id=t.instance_id AND i.user_id=t.owner_user_id AND i.runtime_generation=t.generation)
 OR previous.task_key IS NOT NULL
 OR EXISTS (SELECT 1 FROM gateway_runtime_task_models b LEFT JOIN llm_models m ON m.id=b.model_id
 WHERE b.task_key=t.task_key AND (m.id IS NULL OR m.is_active=FALSE OR NOT
 (m.visibility_scope='all' OR (m.visibility_scope='selected_users' AND EXISTS
 (SELECT 1 FROM llm_model_user_grants g WHERE g.model_id=m.id AND g.user_id=t.owner_user_id))))))`, key)
	if err != nil {
		return nil, err
	}
	var state models.GatewayTaskState
	if err := r.sess.Collection("gateway_runtime_tasks").Find(db.Cond{"task_key": key}).Select("revoked", "finished", "runtime_acknowledged").One(&state); err != nil {
		if err == db.ErrNoMoreRows {
			return nil, models.ErrModelUnavailable
		}
		return nil, err
	}
	return &state, nil
}

func (r *gatewayTaskRepository) Bind(ctx context.Context, t models.GatewayTaskIdentity, modelID int) error {
	state, err := r.Check(ctx, t)
	if err != nil {
		return err
	}
	if state.Revoked || state.Finished {
		return models.ErrModelUnavailable
	}
	key, _ := GatewayTaskKey(t)
	return r.sess.Tx(func(sess db.Session) error {
		// Same model-row lock as an administrator's policy update.
		row, err := sess.SQL().QueryRowContext(ctx, "SELECT id FROM llm_models WHERE id=? FOR UPDATE", modelID)
		if err != nil {
			return err
		}
		var id int
		if err = row.Scan(&id); err != nil {
			return models.ErrModelUnavailable
		}
		checker := &llmModelRepository{sess: sess}
		if err = checker.CheckModelAccess(ctx, t.UserID, modelID); err != nil {
			return err
		}
		row, err = sess.SQL().QueryRowContext(ctx, "SELECT revoked,finished FROM gateway_runtime_tasks WHERE task_key=? FOR UPDATE", key)
		if err != nil {
			return err
		}
		var revoked, finished bool
		if err = row.Scan(&revoked, &finished); err != nil {
			return err
		}
		if revoked || finished {
			return models.ErrModelUnavailable
		}
		_, err = sess.SQL().ExecContext(ctx, "INSERT IGNORE INTO gateway_runtime_task_models(task_key,model_id) VALUES (?,?)", key, modelID)
		return err
	})
}

func (r *gatewayTaskRepository) Finish(ctx context.Context, t models.GatewayTaskIdentity, cancelled bool) error {
	key, err := GatewayTaskKey(t)
	if err != nil {
		return err
	}
	// A normal completion racing revocation is NOT a cancellation acknowledgment.
	_, err = r.sess.SQL().ExecContext(ctx, `UPDATE gateway_runtime_tasks SET finished=TRUE,
 runtime_acknowledged=IF(revoked AND ?,TRUE,runtime_acknowledged) WHERE task_key=?`, cancelled, key)
	return err
}
