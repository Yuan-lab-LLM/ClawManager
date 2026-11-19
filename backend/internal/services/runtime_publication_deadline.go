package services

import (
	"context"
	"errors"
	"time"

	"clawreef/internal/models"
)

var errRuntimePublicationTimeout = errors.New("runtime startup confirmation timed out; ownership retained; manual inspection required")

// The exact reservation's creation time excludes time spent waiting for a slot.
// Running instances and later starts use their existing lifecycle limits.
func runtimePublicationCreateTimeout(instance *models.Instance, origin time.Time) error {
	if instance == nil || instance.Status == "running" || instance.RuntimeGeneration != 1 || instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
		return nil
	}
	if instance.RuntimeErrorCode != nil && *instance.RuntimeErrorCode == "RUNTIME_PUBLICATION_TIMEOUT" {
		return &runtimeFailure{code: "RUNTIME_PUBLICATION_TIMEOUT", cause: errRuntimePublicationTimeout}
	}
	if instance.Status != "creating" || origin.IsZero() || time.Now().Before(origin.Add(5*time.Minute)) {
		return nil
	}
	return &runtimeFailure{code: "RUNTIME_PUBLICATION_TIMEOUT", cause: errRuntimePublicationTimeout}
}

func runtimePublicationOrigin(ctx context.Context, source any, instance *models.Instance) (time.Time, error) {
	if instance == nil || instance.Status != "creating" || instance.RuntimeGeneration != 1 || instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
		return time.Time{}, nil
	}
	if reader, ok := source.(interface {
		RuntimePublicationOrigin(context.Context, int, int) (time.Time, error)
	}); ok {
		origin, err := reader.RuntimePublicationOrigin(ctx, instance.ID, instance.RuntimeGeneration)
		if err != nil {
			return time.Time{}, runtimeSecurityError("publication_origin_unavailable", "publication", nil, errRuntimeGatewayStartPending)
		}
		return origin, nil
	}
	return instance.CreatedAt, nil
}

func recordRuntimePublicationTimeout(ctx context.Context, source any, instance *models.Instance, origin time.Time) error {
	timeout := runtimePublicationCreateTimeout(instance, origin)
	if timeout == nil {
		return nil
	}
	if instance.Status == "creating" {
		if marker, ok := source.(interface {
			MarkRuntimePublicationTimeout(context.Context, int, int, time.Time, string) (bool, error)
		}); ok {
			changed, err := marker.MarkRuntimePublicationTimeout(ctx, instance.ID, instance.RuntimeGeneration, origin, timeout.Error())
			if err != nil || !changed {
				// Keep this stale attempt out of POST and every terminal cleanup branch.
				// A concurrent running report wins; only the CAS may record an error.
				return errors.Join(timeout, runtimeSecurityError("timeout_record_changed", "publication", nil, nil))
			}
		}
	}
	return timeout
}
