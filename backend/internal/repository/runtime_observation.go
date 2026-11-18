package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"clawreef/internal/models"
)

// RuntimePodObservationStore is optional so older repository fakes remain valid.
// Saving observed facts cannot publish bindings or change allocation counters.
type RuntimePodObservationStore interface {
	SaveRuntimeObservation(context.Context, int64, string, models.RuntimePodObservation) (bool, error)
}

func (r *runtimePodRepository) SaveRuntimeObservation(ctx context.Context, podID int64, podUID string, observation models.RuntimePodObservation) (bool, error) {
	if err := validateRuntimeObservation(podID, podUID, observation); err != nil {
		return false, err
	}
	observation.ObservedAt = observation.ObservedAt.UTC().Truncate(time.Microsecond)
	payload, err := json.Marshal(observation)
	if err != nil {
		return false, fmt.Errorf("encode Runtime observation: %w", err)
	}
	if len(payload) > 256*1024 {
		return false, fmt.Errorf("Runtime observation exceeds size limit")
	}
	result, err := r.sess.SQL().ExecContext(ctx, `UPDATE runtime_pods SET observation_json=?,observation_at=? WHERE id=? AND BINARY pod_uid=BINARY ? AND (observation_at IS NULL OR observation_at<?)`, string(payload), observation.ObservedAt, podID, podUID, observation.ObservedAt)
	if err != nil {
		return false, fmt.Errorf("save Runtime observation: %w", err)
	}
	affected, err := result.RowsAffected()
	return affected == 1, err
}

func validateRuntimeObservation(podID int64, podUID string, o models.RuntimePodObservation) error {
	invalid := func() error { return fmt.Errorf("invalid sanitized Runtime observation") }
	if podID <= 0 || podUID == "" || !runtimeObservationText(podUID, 128) || strings.TrimSpace(podUID) != podUID || o.PodUID != podUID {
		return invalid()
	}
	if o.ObservedAt.IsZero() || o.ObservedAt.Year() < 1000 || o.ObservedAt.Year() > 9999 || o.ObservedAt.After(time.Now().Add(5*time.Second)) {
		return invalid()
	}
	if len(o.Slots) > 256 || (len(o.Slots) == 0 && o.Error == "") || !runtimeObservationText(o.Error, 512) {
		return invalid()
	}
	ports := map[int]bool{}
	containers := map[string]bool{}
	for _, s := range o.Slots {
		if !runtimeObservationText(s.ContainerName, 63) || s.ContainerName == "" || strings.TrimSpace(s.ContainerName) != s.ContainerName || containers[s.ContainerName] || s.GatewayPort < 1 || s.GatewayPort > 65535 || ports[s.GatewayPort] {
			return invalid()
		}
		containers[s.ContainerName] = true
		ports[s.GatewayPort] = true
		if s.State != "free" && s.State != "occupied" && s.State != "unknown" {
			return invalid()
		}
		if s.InstanceID < 0 || s.UserID < 0 || s.Generation < 0 || !runtimeObservationText(s.GatewayID, 128) || !runtimeObservationText(s.InstanceState, 32) || !runtimeObservationText(s.GatewayState, 32) || !runtimeObservationText(s.Error, 512) {
			return invalid()
		}
		if s.State == "free" && (s.InstanceID != 0 || s.UserID != 0 || s.Generation != 0 || s.GatewayID != "" || s.InstanceState != "" || s.IdentityVerified || s.Error != "" || (s.GatewayState != "" && s.GatewayState != "stopped")) {
			return invalid()
		}
		if s.IdentityVerified && (s.State != "occupied" || s.InstanceID == 0 || s.UserID == 0 || s.Generation == 0 || s.GatewayID == "") {
			return invalid()
		}
	}
	return nil
}

func runtimeObservationText(s string, limit int) bool {
	if len(s) > limit || !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}
