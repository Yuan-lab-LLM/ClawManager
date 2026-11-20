package services

import (
	"context"
	"errors"
	"net/netip"
	"time"

	"clawreef/internal/runtimeauth"
	"clawreef/internal/runtimeinventory"
)

// RuntimeSecurityLease is returned only to the trusted slot supervisor. Native
// processes receive local broker endpoints, never these rotating credentials.
type RuntimeSecurityLease struct {
	InstanceID    int       `json:"instance_id"`
	UserID        int       `json:"user_id"`
	Generation    int       `json:"generation"`
	PodUID        string    `json:"pod_uid"`
	ModelToken    string    `json:"model_token"`
	EgressToken   string    `json:"egress_token"`
	ServicesToken string    `json:"services_token"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (l RuntimeSecurityLease) Identity() runtimeauth.Identity {
	return runtimeauth.Identity{InstanceID: l.InstanceID, UserID: l.UserID, Generation: l.Generation, PodUID: l.PodUID}
}

// IssueLease additionally binds the control-authenticated caller's actual TCP
// source to the assigned Pod. Forwarded headers must never supply peer.
func (a *RuntimeSecurityAuthority) IssueLease(ctx context.Context, expected runtimeauth.Identity, peer netip.Addr) (RuntimeSecurityLease, error) {
	if expected.InstanceID == poolProbeInstanceID {
		return a.issuePoolValidationLease(ctx, expected, peer)
	}
	ctx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
	defer cancel()
	delays := [...]time.Duration{25, 50, 100, 200, 250, 250, 250}
	for attempt := 0; ; attempt++ {
		if ctx.Err() != nil {
			return RuntimeSecurityLease{}, ctx.Err()
		}
		lease, err := a.issueLeaseAttempt(ctx, expected, peer)
		if err == nil {
			return lease, nil
		}
		logRuntimeSecurityFailure(ctx, expected, attempt+1, err)
		if ctx.Err() != nil {
			return RuntimeSecurityLease{}, ctx.Err()
		}
		if !errors.Is(err, runtimeinventory.ErrUnavailable) || attempt == len(delays) {
			return RuntimeSecurityLease{}, err
		}
		timer := time.NewTimer(delays[attempt] * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return RuntimeSecurityLease{}, ctx.Err()
		case <-timer.C:
		}
	}
}

func (a *RuntimeSecurityAuthority) issueLeaseAttempt(ctx context.Context, expected runtimeauth.Identity, peer netip.Addr) (RuntimeSecurityLease, error) {
	var empty RuntimeSecurityLease
	snapshot, err := a.validatedSnapshotForIdentity(ctx, expected.InstanceID, &expected)
	identity := snapshot.identity
	if err != nil {
		return empty, err
	}
	fail := func(reason, stage string, cause error) error {
		return &runtimeSecurityDenial{diagnostic: RuntimeSecurityDiagnostic{Reason: reason, Stage: stage, AssignmentUID: snapshot.assignmentUID, AssignmentResourceVersion: snapshot.assignmentVersion}, cause: cause}
	}
	if identity != expected {
		return empty, fail("identity_conflict", "lease", nil)
	}
	if !peer.IsValid() || peer.Zone() != "" || snapshot.address.Unmap() != peer.Unmap() {
		return empty, fail("peer_conflict", "lease", nil)
	}
	lease := RuntimeSecurityLease{InstanceID: identity.InstanceID, UserID: identity.UserID, Generation: identity.Generation, PodUID: identity.PodUID, ExpiresAt: time.Now().UTC().Truncate(time.Second).Add(15 * time.Minute)}
	started := time.Now()
	tokens, err := a.capabilities.IssueAll(ctx, identity)
	runtimeauth.RecordStage(ctx, "capability_sign", started, err)
	if err != nil {
		if ctx.Err() != nil {
			return empty, ctx.Err()
		}
		if errors.Is(err, runtimeinventory.ErrUnavailable) {
			return empty, fail("inventory_unavailable", "issue", runtimeauth.Unavailable(err))
		}
		if runtimeauth.IsUnavailable(err) {
			return empty, fail("authority_unavailable", "issue", err)
		}
		return empty, fail("authorization_denied", "issue", nil)
	}
	lease.ModelToken, lease.EgressToken, lease.ServicesToken = tokens[0], tokens[1], tokens[2]
	final, err := a.validatedSnapshotForIdentity(ctx, expected.InstanceID, &expected)
	if err != nil {
		return empty, err
	}
	if final.identity != expected || final.address.Unmap() != peer.Unmap() {
		return empty, fail("identity_conflict", "lease_complete", nil)
	}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	return lease, nil
}
