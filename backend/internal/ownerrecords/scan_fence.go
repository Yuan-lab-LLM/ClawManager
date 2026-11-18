package ownerrecords

import (
	"context"
	"errors"
	"github.com/upper/db/v4"
	"strconv"
)

var errScanChanged = errors.New("instance changed while history was being read")

type scanRuntimeKey struct{}

func scanReadGeneration(item scanItem) string {
	if item.runtimeGeneration == 0 {
		return item.generation
	}
	return item.generation + "|" + strconv.FormatUint(item.runtimeGeneration, 10)
}
func scanRuntimeClause(ctx context.Context, alias string) string {
	if generation, _ := ctx.Value(scanRuntimeKey{}).(uint64); generation > 0 {
		return " AND " + alias + ".runtime_generation=" + strconv.FormatUint(generation, 10)
	}
	return ""
}

func (s *Store) captureScanRuntime(ctx context.Context, item *scanItem) (context.Context, error) {
	row, e := s.Session.SQL().QueryRowContext(ctx, `SELECT runtime_generation FROM instances WHERE id=? AND user_id=? AND LOWER(TRIM(owner))=? AND status='running' AND SHA2(CONCAT(COALESCE(workspace_path,''),'|',COALESCE(access_token,'')),256)=?`, item.identity.Instance, item.identity.Scope, item.identity.Owner, item.generation)
	if e != nil {
		return ctx, e
	}
	if e = row.Scan(&item.runtimeGeneration); e != nil {
		return ctx, e
	}
	if item.runtimeGeneration == 0 {
		return ctx, errScanChanged
	}
	ctx = context.WithValue(ctx, dshGenerationKey{}, item.generation)
	return context.WithValue(ctx, scanRuntimeKey{}, item.runtimeGeneration), nil
}

// Native reads happen outside transactions. Revalidate and lock the current
// generation inside each fact transaction, so reset/reassignment cannot commit
// an old process result against a new instance generation. Normal ingress facts
// do not carry this private worker context and keep their existing contract.
func validateScanGeneration(ctx context.Context, tx db.Session, i Identity) error {
	generation, _ := ctx.Value(dshGenerationKey{}).(string)
	if generation == "" {
		return nil
	}
	runtimeGeneration, guarded := ctx.Value(scanRuntimeKey{}).(uint64)
	column := "0"
	if guarded {
		column = "runtime_generation"
	}
	row, err := tx.SQL().QueryRowContext(ctx, `SELECT SHA2(CONCAT(COALESCE(workspace_path,''),'|',COALESCE(access_token,'')),256),user_id,LOWER(TRIM(owner)),type,instance_mode,status,`+column+` FROM instances WHERE id=? FOR SHARE`, i.Instance)
	if err != nil {
		return err
	}
	var actual, owner, runtime, mode, status string
	var scope uint64
	var currentGeneration uint64
	if err = row.Scan(&actual, &scope, &owner, &runtime, &mode, &status, &currentGeneration); err != nil {
		return err
	}
	if actual != generation || scope != i.Scope || owner != i.Owner || runtime != i.Runtime || mode != "lite" || status != "running" || guarded && currentGeneration != runtimeGeneration {
		return errScanChanged
	}
	return nil
}
