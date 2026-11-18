package repository

import (
	"clawreef/internal/models"
	"context"
	"fmt"
	"strings"
)

func (r *instanceRepository) hydrateAutoStopSlice(items []models.Instance) error {
	pointers := make([]*models.Instance, len(items))
	for i := range items {
		pointers[i] = &items[i]
	}
	return r.hydrateAutoStop(pointers)
}
func (r *instanceRepository) hydrateAutoStop(items []*models.Instance) error {
	if len(items) == 0 {
		return nil
	}
	byID := make(map[int]*models.Instance, len(items))
	args := []any{}
	marks := []string{}
	for _, i := range items {
		if i != nil && i.InstanceMode == "lite" {
			byID[i.ID] = i
			args = append(args, i.ID)
			marks = append(marks, "?")
		}
	}
	if len(args) == 0 {
		return nil
	}
	rows, err := r.sess.SQL().QueryContext(context.Background(), `SELECT instance_id,monitoring_since,last_gateway_activity_at,auto_stop_disabled,stopped_reason,blocker,stop_phase FROM instance_model_activity WHERE instance_id IN (`+strings.Join(marks, ",")+`)`, args...)
	if missingAutoStopTable(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read auto-stop metadata: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var a models.InstanceModelActivity
		if err = rows.Scan(&a.InstanceID, &a.MonitoringSince, &a.LastGatewayActivityAt, &a.AutoStopDisabled, &a.StoppedReason, &a.Blocker, &a.StopPhase); err != nil {
			return err
		}
		if i := byID[a.InstanceID]; i != nil {
			i.MonitoringSince = &a.MonitoringSince
			i.LastGatewayActivityAt = a.LastGatewayActivityAt
			i.AutoStopDisabled = a.AutoStopDisabled
			i.StoppedReason = a.StoppedReason
			i.AutoStopBlocker = a.Blocker
			i.AutoStopPhase = a.StopPhase
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	rows.Close()
	return r.hydrateAutoStopPolicyStatus(items)
}
