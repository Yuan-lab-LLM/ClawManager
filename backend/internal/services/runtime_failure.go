package services

import (
	"errors"
	"net/http"
)

type runtimeFailure struct {
	code  string
	cause error
}

func (e *runtimeFailure) Error() string     { return e.cause.Error() }
func (e *runtimeFailure) Unwrap() error     { return e.cause }
func (e *runtimeFailure) ErrorCode() string { return e.code }
func (e *runtimeFailure) HTTPStatus() int {
	if e.code == "MODEL_CONFIGURATION_REQUIRED" {
		return http.StatusConflict
	}
	return http.StatusServiceUnavailable
}

func runtimeFailureCode(err error) string {
	var failure interface{ ErrorCode() string }
	if errors.As(err, &failure) {
		return failure.ErrorCode()
	}
	if errors.Is(err, errRuntimeControlPlanePending) {
		return "CONTROL_PLANE_UNAVAILABLE"
	}
	return "GATEWAY_START_FAILED"
}
