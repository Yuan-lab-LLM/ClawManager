package services

import (
	"context"
	"errors"
	"time"

	"clawreef/internal/models"
)

var errRuntimePublicationTimeout = errors.New("runtime startup confirmation timed out; ownership retained; manual inspection required")

// CreatedAt is the immutable origin of direct G1 creates. Sticky protection is
// installed only after publication, so capacity waiting without a publication
// keeps its existing lifecycle. Running instances and later starts are exempt.
func runtimePublicationCreateTimeout(instance *models.Instance) error {
	if instance == nil || instance.Status == "running" || instance.RuntimeGeneration != 1 || instance.RuntimeSecurityProfile != RuntimeSecurityProfile {
		return nil
	}
	if instance.RuntimeErrorCode != nil && *instance.RuntimeErrorCode == "RUNTIME_PUBLICATION_TIMEOUT" {
		return &runtimeFailure{code: "RUNTIME_PUBLICATION_TIMEOUT", cause: errRuntimePublicationTimeout}
	}
	// Legacy unit adapters may omit timestamps; real SQL Create always sets it.
	if instance.Status != "creating" || instance.CreatedAt.IsZero() || time.Now().Before(instance.CreatedAt.Add(5*time.Minute)) {
		return nil
	}
	return &runtimeFailure{code: "RUNTIME_PUBLICATION_TIMEOUT", cause: errRuntimePublicationTimeout}
}

func recordRuntimePublicationTimeout(ctx context.Context, source any, instance *models.Instance) error {
	timeout := runtimePublicationCreateTimeout(instance)
	if timeout == nil {
		return nil
	}
	if instance.Status == "creating" {
		if marker, ok := source.(interface {
			MarkRuntimePublicationTimeout(context.Context, int, int, time.Time, string) (bool, error)
		}); ok {
			changed, err := marker.MarkRuntimePublicationTimeout(ctx, instance.ID, instance.RuntimeGeneration, instance.CreatedAt, timeout.Error())
			if err != nil || !changed {
				// Keep this stale attempt out of POST and every terminal cleanup branch.
				// A concurrent running report wins; only the CAS may record an error.
				return errors.Join(timeout, runtimeSecurityError("timeout_record_changed", "publication", nil, nil))
			}
		}
	}
	return timeout
}
