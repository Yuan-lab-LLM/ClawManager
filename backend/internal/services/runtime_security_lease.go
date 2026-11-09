package services

import (
	"context"
	"net/netip"
	"time"

	"clawreef/internal/runtimeauth"
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
	var empty RuntimeSecurityLease
	snapshot, err := a.validatedSnapshot(ctx, expected.InstanceID)
	identity := snapshot.identity
	if err != nil || identity != expected || !peer.IsValid() || peer.Zone() != "" || snapshot.address.Unmap() != peer.Unmap() {
		return empty, errRuntimeSecurity
	}
	lease := RuntimeSecurityLease{InstanceID: identity.InstanceID, UserID: identity.UserID, Generation: identity.Generation, PodUID: identity.PodUID, ExpiresAt: time.Now().UTC().Truncate(time.Second).Add(15 * time.Minute)}
	for _, entry := range []struct {
		audience string
		token    *string
	}{{runtimeauth.AudienceModel, &lease.ModelToken}, {runtimeauth.AudienceEgress, &lease.EgressToken}, {runtimeauth.AudienceServices, &lease.ServicesToken}} {
		*entry.token, err = a.capabilities.Issue(ctx, identity, entry.audience)
		if err != nil {
			return empty, errRuntimeSecurity
		}
	}
	if ctx.Err() != nil {
		return empty, ctx.Err()
	}
	return lease, nil
}
