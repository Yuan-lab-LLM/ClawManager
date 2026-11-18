package repository

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/upper/db/v4"
	"time"
)

type runtimePreparationClaim struct{ ID, RuntimeType string }
type runtimePreparationContextKey struct{}

func WithRuntimeRolloutPreparation(ctx context.Context, id, runtimeType string) context.Context {
	return context.WithValue(ctx, runtimePreparationContextKey{}, runtimePreparationClaim{ID: id, RuntimeType: runtimeType})
}

// Completed preparation retains a bounded admission reservation during native
// compatibility review. Only the matching rollout may consume it in its insert
// transaction; expiry releases capacity without undoing any native wake.
const readyRuntimePreparationPredicate = "kind='rollout_wake' AND status='completed' AND JSON_UNQUOTE(JSON_EXTRACT(request_json,'$.phase'))='ready' AND COALESCE(JSON_EXTRACT(request_json,'$.ready_until_unix'),0)>UNIX_TIMESTAMP() AND COALESCE(JSON_EXTRACT(request_json,'$.consumed_by'),0)=0"

func CheckRuntimePreparationReservations(ctx context.Context, tx db.Session, except string) error {
	return CheckRuntimePreparationReservationsForType(ctx, tx, except, "")
}
func CheckRuntimePreparationReservationsForType(ctx context.Context, tx db.Session, except, runtimeType string) error {
	return checkRuntimeOperationScopes(ctx, tx, "SELECT id,request_json,items_json FROM runtime_management_operations WHERE id<>? AND ((kind='rollout_wake' AND status IN ('pending','processing','starting','needs_attention')) OR ("+readyRuntimePreparationPredicate+"))", []any{except}, runtimeType)
}

func checkRuntimePreparationClaim(ctx context.Context, tx db.Session, runtimeTypes ...string) error {
	claim, _ := ctx.Value(runtimePreparationContextKey{}).(runtimePreparationClaim)
	runtimeType := claim.RuntimeType
	if len(runtimeTypes) > 0 {
		runtimeType = runtimeTypes[0]
	}
	if claim.ID != "" && claim.RuntimeType != runtimeType {
		return errors.New("Runtime upgrade preparation type mismatch")
	}
	if err := CheckRuntimePreparationReservationsForType(ctx, tx, claim.ID, runtimeType); err != nil {
		return err
	}
	if claim.ID == "" {
		return nil
	}
	var raw string
	row, err := tx.SQL().QueryRowContext(ctx, "SELECT request_json FROM runtime_management_operations WHERE id=? AND "+readyRuntimePreparationPredicate+" FOR UPDATE", claim.ID)
	if err == nil {
		err = row.Scan(&raw)
	}
	if err != nil {
		return errors.New("Runtime upgrade preparation is missing, expired or consumed; prepare and preflight again")
	}
	var j struct {
		RuntimeType string `json:"runtime_type"`
	}
	if json.Unmarshal([]byte(raw), &j) != nil || j.RuntimeType != claim.RuntimeType {
		return errors.New("Runtime upgrade preparation type mismatch")
	}
	return nil
}

func ConsumeRuntimeRolloutPreparation(ctx context.Context, tx db.Session, rolloutID int64) error {
	claim, _ := ctx.Value(runtimePreparationContextKey{}).(runtimePreparationClaim)
	if claim.ID == "" {
		return nil
	}
	if err := checkRuntimePreparationClaim(ctx, tx); err != nil {
		return err
	}
	result, err := tx.SQL().ExecContext(ctx, "UPDATE runtime_management_operations SET request_json=JSON_SET(request_json,'$.consumed_by',?),updated_at=? WHERE id=? AND "+readyRuntimePreparationPredicate, rolloutID, time.Now().UTC(), claim.ID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n != 1 {
		return errors.New("Runtime preparation changed during rollout admission")
	}
	return err
}

type runtimeAdmissionValidationKey struct{}

// Server-side native validation runs inside the same admission transaction as
// publication, closing discovery-to-insert races without exposing a client hook.
func WithRuntimeRolloutAdmissionValidation(ctx context.Context, check func() error) context.Context {
	return context.WithValue(ctx, runtimeAdmissionValidationKey{}, check)
}
func validateRuntimeRolloutAdmission(ctx context.Context) error {
	if check, ok := ctx.Value(runtimeAdmissionValidationKey{}).(func() error); ok {
		return check()
	}
	return nil
}
