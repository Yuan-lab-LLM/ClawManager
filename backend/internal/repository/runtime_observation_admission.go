package repository

import (
	"context"
	"fmt"
	"time"

	"clawreef/internal/models"
	"github.com/upper/db/v4"
)

// Caller owns the transaction through source publication or the replica CAS.
// Observation updates take this same row lock and cannot race the last check.
func CheckRuntimePodEmptyObservation(ctx context.Context, tx db.Session, id int64, uid string) error {
	var p models.RuntimePod
	row, err := tx.SQL().QueryRowContext(ctx, "SELECT pod_uid,capacity,used_slots,observation_json,observation_at FROM runtime_pods WHERE id=? FOR UPDATE", id)
	if err == nil {
		err = row.Scan(&p.PodUID, &p.Capacity, &p.UsedSlots, &p.ObservationJSON, &p.ObservationAt)
	}
	if err != nil {
		return err
	}
	if p.PodUID == nil || *p.PodUID != uid {
		return fmt.Errorf("Runtime empty observation source identity changed")
	}
	return models.ConfirmedEmptyRuntimeObservation(p, time.Now().UTC())
}
