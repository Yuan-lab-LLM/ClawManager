package models

import (
	"errors"
	"fmt"
)

var ErrInstanceOperationInProgress = errors.New("instance operation already in progress; wait for it to finish")

// Lock contention can be retried; a durable lifecycle reservation cannot.
var ErrInstanceLifecycleLockBusy = fmt.Errorf("%w: lifecycle lock is busy", ErrInstanceOperationInProgress)
