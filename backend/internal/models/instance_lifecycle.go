package models

import "errors"

var ErrInstanceOperationInProgress = errors.New("instance operation already in progress; wait for it to finish")
