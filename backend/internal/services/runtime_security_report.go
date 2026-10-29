package services

import (
	"clawreef/internal/runtimeauth"
	"context"
	"fmt"
)

// VerifyReportedGateway binds a report to one current SQL/assignment/Ready-Pod
// observation. It authorizes no new writer and does not advance a generation.
func (a *RuntimeSecurityAuthority) VerifyReportedGateway(ctx context.Context, expected runtimeauth.Identity, runtimeType, workspace string, port int) error {
	snapshot, err := a.validatedSnapshot(ctx, expected.InstanceID)
	if err != nil || snapshot.identity != expected || snapshot.gatewayID != fmt.Sprintf("gw-%d-%d", expected.InstanceID, expected.Generation) {
		return errRuntimeSecurity
	}
	kind, supported := NormalizeV2RuntimeType(runtimeType)
	if !supported || kind != runtimeType || snapshot.request.AgentType != runtimeType || workspace == "" || snapshot.request.WorkspacePath != workspace || snapshot.request.GatewayPort != port || port < 1024 || port > 65535-RuntimeGatewayPortBlockSize(runtimeType)+1 || ctx.Err() != nil {
		return errRuntimeSecurity
	}
	return nil
}
