package services

import "errors"

// The durable stop intent retains its source binding until teardown is proven.
var ErrInstanceStopPending = errors.New("stop accepted; original runtime shutdown is awaiting confirmation")
