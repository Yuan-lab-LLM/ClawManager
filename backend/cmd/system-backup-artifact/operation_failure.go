package main

import (
	"encoding/json"
	"errors"
	"os"

	"clawreef/internal/systembackup"
)

type operationStageError struct {
	stage string
	cause error
}

func (e *operationStageError) Error() string { return e.cause.Error() }
func (e *operationStageError) Unwrap() error { return e.cause }

func writeOperationFailure(path, operation, origin, id string, err error) error {
	stage := "operation"
	var failure *operationStageError
	if errors.As(err, &failure) {
		stage = failure.stage
	}
	raw, e := json.Marshal(map[string]string{"schema": "system-backup-operation-failure.v1", "operation": operation, "step": stage, "code": systembackup.PublicError(err), "origin_installation_id": origin, "backup_id": id})
	if e != nil {
		return e
	}
	return os.WriteFile(path, raw, 0600)
}
