package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"clawreef/internal/models"
)

const MaxRuntimeTaskInspection = 32

// Optional additive interface: inspection never calls Check or creates tasks.
type GatewayTaskInspector interface {
	Inspect(context.Context, models.GatewayTaskIdentity, []models.GatewayTaskIdentity) ([]models.GatewayTaskSnapshot, error)
}

func PrepareRuntimeTaskInspection(scope models.GatewayTaskIdentity, requested []models.GatewayTaskIdentity) ([]models.GatewayTaskIdentity, error) {
	if scope.InstanceID <= 0 || scope.UserID <= 0 || scope.Generation < 0 || len(requested) == 0 || len(requested) > MaxRuntimeTaskInspection {
		return nil, fmt.Errorf("invalid runtime task inspection scope or batch size")
	}
	owned := make([]models.GatewayTaskIdentity, len(requested))
	seen := map[string]bool{}
	for i, task := range requested {
		task.InstanceID, task.UserID = scope.InstanceID, scope.UserID
		key, err := GatewayTaskKey(task)
		if err != nil || task.Generation > scope.Generation || seen[key] {
			return nil, fmt.Errorf("invalid, future or duplicate runtime task identity")
		}
		seen[key] = true
		owned[i] = task
	}
	return owned, nil
}

func (r *gatewayTaskRepository) Inspect(ctx context.Context, scope models.GatewayTaskIdentity, requested []models.GatewayTaskIdentity) ([]models.GatewayTaskSnapshot, error) {
	tasks, err := PrepareRuntimeTaskInspection(scope, requested)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args := make([]any, 0, len(tasks)+3)
	result := make([]models.GatewayTaskSnapshot, len(tasks))
	indexes := map[string]int{}
	for i, task := range tasks {
		key, _ := GatewayTaskKey(task)
		args = append(args, key)
		indexes[key] = i
		result[i].GatewayTaskIdentity = task
	}
	args = append(args, scope.InstanceID, scope.UserID, scope.Generation)
	// Anchor on the CURRENT owner/generation in the same statement. A missing
	// instance/current scope must not be confused with an absent historical task.
	query := `SELECT t.task_key,t.revoked,t.finished,t.runtime_acknowledged
 FROM instances i JOIN users u ON u.id=i.user_id AND u.is_active=TRUE
 LEFT JOIN gateway_runtime_tasks t ON t.instance_id=i.id AND t.owner_user_id=i.user_id
 AND t.task_key IN (` + strings.TrimSuffix(strings.Repeat("?,", len(tasks)), ",") + `)
 WHERE i.id=? AND i.user_id=? AND i.runtime_generation=?`
	rows, err := r.sess.SQL().QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	anchored := false
	for rows.Next() {
		var key sql.NullString
		var revoked, finished, acknowledged sql.NullBool
		if err := rows.Scan(&key, &revoked, &finished, &acknowledged); err != nil {
			return nil, err
		}
		anchored = true
		if !key.Valid {
			continue
		}
		i, ok := indexes[key.String]
		if !ok || !revoked.Valid || !finished.Valid || !acknowledged.Valid {
			return nil, fmt.Errorf("invalid runtime task inspection result")
		}
		result[i].State = &models.GatewayTaskState{Revoked: revoked.Bool, Finished: finished.Bool, RuntimeAcknowledged: acknowledged.Bool}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if !anchored {
		return nil, models.ErrModelUnavailable
	}
	return result, nil
}
