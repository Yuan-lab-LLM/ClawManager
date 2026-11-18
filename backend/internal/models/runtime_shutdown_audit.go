package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"
)

// Delivery preserves the ORIGINAL owner and generation across process restarts.
// It is historical diagnostic data, never a lifecycle command or a stop proof.
type RuntimeShutdownEvent struct {
	InstanceID  int    `json:"instance_id"`
	OwnerUserID int    `json:"owner_user_id"`
	Generation  int    `json:"generation"`
	ClaimID     string `json:"claim_id"`
	Outcome     string `json:"outcome"`
}
type RuntimeShutdownDelivery struct {
	ID    string               `json:"id"`
	Event RuntimeShutdownEvent `json:"event"`
}

func (e RuntimeShutdownEvent) Observation() RuntimeShutdownObservation {
	return RuntimeShutdownObservation{Generation: e.Generation, ClaimID: e.ClaimID, Outcome: e.Outcome}
}
func (e RuntimeShutdownEvent) DeliveryID() string {
	raw, _ := json.Marshal(e)
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
func (d RuntimeShutdownDelivery) Validate() error {
	if d.Event.InstanceID <= 0 || d.Event.OwnerUserID <= 0 || d.Event.Observation().Validate() != nil || d.ID != d.Event.DeliveryID() {
		return ErrInvalidShutdownObservation
	}
	return nil
}

// A manager observation is diagnostic evidence, NEVER permission to unlock,
// finish a task, release a port or activate a new runtime generation.
type RuntimeShutdownObservation struct {
	Generation int    `json:"generation"`
	ClaimID    string `json:"claim_id"`
	Outcome    string `json:"outcome"`
}

var shutdownClaimPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var ErrInvalidShutdownObservation = errors.New("invalid runtime shutdown observation")
var ErrShutdownAuditUnavailable = errors.New("runtime shutdown audit storage unavailable")

func (o RuntimeShutdownObservation) Validate() error {
	if o.Generation < 0 || int64(o.Generation) > 2147483647 || !shutdownClaimPattern.MatchString(o.ClaimID) {
		return ErrInvalidShutdownObservation
	}
	switch o.Outcome {
	case "cleanup_unconfirmed", "cleanup_confirmed_release_unconfirmed", "cleanup_confirmed_release_observed":
		return nil
	}
	return ErrInvalidShutdownObservation
}

type RuntimeShutdownAudit struct {
	ID          int64     `db:"id" json:"id"`
	InstanceID  int       `db:"instance_id" json:"-"`
	OwnerUserID int       `db:"owner_user_id" json:"-"`
	Generation  int       `db:"generation" json:"generation"`
	ClaimID     string    `db:"claim_id" json:"claim_id"`
	Outcome     string    `db:"outcome" json:"outcome"`
	Source      string    `db:"source" json:"source"`
	ReceivedAt  time.Time `db:"received_at" json:"received_at"`
}
