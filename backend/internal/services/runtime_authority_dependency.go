package services

import (
	"context"
	"database/sql"
	"errors"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/runtimeinventory"
	"github.com/upper/db/v4"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

// Called only at dependency I/O boundaries. A confirmed absent assignment or
// missing relation remains a denial; an unavailable dependency grants nothing.
func runtimeAuthorityDependency(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNoMoreRows) || errors.Is(err, sql.ErrNoRows) || apierrors.IsNotFound(err) || errors.Is(err, runtimeinventory.ErrAssignmentAbsent) {
		return errRuntimeSecurity
	}
	return runtimeauth.Unavailable(err)
}
