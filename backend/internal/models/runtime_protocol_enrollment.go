package models

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
)

var ErrRuntimeCapabilityEnrollmentConflict = errors.New("runtime capability enrollment changed; inspect again before explicit enrollment")

// EnrollmentRevision identifies the enrolled contract, not its liveness. A
// heartbeat must not invalidate an operator's preflight. Bind owner and instance
// as well as generation so a revision cannot be moved between identities.
func (s *RuntimeProtocolState) EnrollmentRevision() (string, error) {
	if s == nil || s.InstanceID <= 0 || s.OwnerUserID <= 0 {
		return "", ErrRuntimeCapabilityEnrollmentConflict
	}
	var capabilities []string
	if json.Unmarshal([]byte(s.CapabilitiesJSON), &capabilities) != nil {
		return "", ErrRuntimeCapabilityEnrollmentConflict
	}
	report := RuntimeCapabilityReport{Protocol: s.Protocol, Generation: s.Generation, RuntimeVersion: s.RuntimeVersion, AdapterVersion: s.AdapterVersion, Capabilities: capabilities}
	if report.Validate() != nil {
		return "", ErrRuntimeCapabilityEnrollmentConflict
	}
	sort.Strings(report.Capabilities)
	raw, err := json.Marshal(struct {
		InstanceID int
		OwnerID    int
		Report     RuntimeCapabilityReport
	}{s.InstanceID, s.OwnerUserID, report})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:]), nil
}

// nil explicitly means "I inspected and found no enrollment".
func ValidateEnrollmentRevision(revision *string) error {
	if revision == nil {
		return nil
	}
	if len(*revision) != 64 {
		return ErrRuntimeCapabilityEnrollmentConflict
	}
	for _, c := range *revision {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return ErrRuntimeCapabilityEnrollmentConflict
		}
	}
	return nil
}
