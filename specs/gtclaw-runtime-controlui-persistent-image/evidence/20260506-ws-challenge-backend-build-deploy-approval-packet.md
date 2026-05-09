# WS Challenge Backend Build/Deploy Approval Packet

Date: 2026-05-06
Worker: WSChallengeBackendBuildDeployApprovalPacketWorker
Verdict: WS_CHALLENGE_BACKEND_BUILD_DEPLOY_APPROVAL_PACKET_DONE

## Approval Request

Please approve or reject the next Backend Build/Deploy Gate for the WS challenge bridge source fix.

Approval would authorize a future worker to build and deploy the existing backend/control-plane source fix to the current control-plane backend, then record deploy health evidence. Approval would not authorize browser E2E, fresh instance creation, runtime image rebuild, registry mutation beyond an explicitly approved backend deploy path, database writes, manual pod/service patching, `kubectl cp`, git stage/commit/push, `passes:true`, Close, longterm write-back, or Mem0 write.

Recommended approval options:

- `APPROVE_WS_CHALLENGE_BACKEND_BUILD_DEPLOY_GATE`: run the future Backend Build/Deploy Gate with the minimum scope below.
- `REJECT_OR_BLOCK`: do not deploy; provide the blocking concern or revised scope.

No approval is implied by this packet.

## Dependency Summary

Required dependency gates are present:

- Root Cause Investigation Gate:
  - evidence: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-control-ui-ws-challenge-root-cause.md`
  - verdict: `CONTROL_UI_WS_CHALLENGE_ROOT_CAUSE_DONE`
  - summary: root cause is backend control-ui WebSocket sequencing; upstream `connect.challenge` was not forwarded before browser first connect, so browser connect could carry empty `device.nonce` and produce `invalid connect params`.
- WS Challenge Bridge Implementation Approval Packet:
  - evidence: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation-approval-packet.md`
  - verdict: `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_APPROVAL_PACKET_DONE`
- WS Challenge Bridge Implementation Gate:
  - evidence: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-implementation.md`
  - verdict: `WS_CHALLENGE_BRIDGE_IMPLEMENTATION_DONE`
  - summary: source changes are limited to `backend/internal/services/instance_proxy_service.go` and `backend/internal/services/instance_proxy_service_test.go`.
- WS Challenge Bridge Safety Hardening Gate:
  - evidence: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-safety-hardening.md`
  - verdict: `WS_CHALLENGE_BRIDGE_SAFETY_HARDENING_DONE`
- WS Challenge Bridge Reviewer Gate:
  - evidence: `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-review.md`
  - verdict: `WS_CHALLENGE_BRIDGE_REVIEW_DONE`
  - recommendation: Backend Build/Deploy Approval Packet, then Browser/Manual E2E approval.

Commander-provided review facts:

- focused safety tests passed
- focused WS tests passed
- `go test -count=1 ./internal/services ./internal/handlers` passed
- `git diff --check` passed
- secret-shape matches were classified as fake test fixtures/source identifiers, with no real secret accepted

Live verification is still needed because manual observation can still hit the old deployed backend and show the manual connect form, `invalid connect params`, or `device.nonce` error until the source fix is built and deployed.

## Future Allowed Scope

If the user approves `APPROVE_WS_CHALLENGE_BACKEND_BUILD_DEPLOY_GATE`, the future Backend Build/Deploy Gate may do only the following:

- run `go test -count=1 ./internal/services ./internal/handlers` first
- build only the backend/control-plane image or the repository's existing backend deploy artifact required to update the control-plane backend
- deploy only to existing `Deployment/clawmanager-app`
- wait for and record rollout status for `Deployment/clawmanager-app`
- read workload and pod status needed to prove the rollout result
- run no-auth smoke checks:
  - `/healthz` expected HTTP 200
  - `/api/v1/auth/me` without auth expected HTTP 401
- verify the deployed backend image/tag changed to the new build
- record old backend image and new backend image/tag
- verify no runtime image digest drift
- verify no fresh instance was created
- verify no browser E2E was executed

Registry/image caveat:

- Runtime image build/tag/push/pull is not allowed.
- If the approved backend deploy path strictly requires local backend image load/tag/import for the control-plane image, the future worker must keep it backend-only and document exactly why it is required.
- Any registry mutation beyond the exact backend deploy path requires separate user approval.

## Future Required Checks

The future Backend Build/Deploy Gate must record:

- command and result for `go test -count=1 ./internal/services ./internal/handlers`
- old backend image
- new backend image/tag
- rollout result for `Deployment/clawmanager-app`
- pod Ready state
- pod restart count
- `/healthz` HTTP 200
- `/api/v1/auth/me` no-auth HTTP 401
- no runtime image digest drift
- no fresh instance created
- no browser E2E executed
- no frontend/runtime/deployments/docs/longterm changes
- no database migration/write
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no git stage/commit/push
- no `passes:true`
- no Close
- no Mem0 write

The future gate should also record whether the deployment target stayed on the existing namespace/workload/container and whether any local backend image import/tag/load action was used.

## Future Forbidden Actions

This approval packet does not authorize the future gate to do any of the following:

- browser E2E, Chrome DevTools MCP, Playwright, or other browser automation
- fresh instance creation, deletion, or modification
- runtime image rebuild
- runtime registry mutation
- database migration/write
- manual pod patch
- manual Service patch
- `kubectl cp`
- frontend changes
- runtime source/artifact changes
- deployment manifest changes
- docs changes
- longterm changes
- AgentTeam changes
- spec/plan/tasks changes
- existing evidence modification
- git stage/commit/push
- `passes:true`
- Close
- Mem0 write
- secret, token, cookie, credential, or access URL plaintext output

## Live Verification Follow-up Gate

After an approved and successful Backend Build/Deploy Gate, the next gate should be a Browser/Manual E2E Approval Packet.

That later approval packet should request authorization for a Browser/Manual E2E rerun that checks:

- instance 10 control-ui does not show the manual connect form
- no `invalid connect params`
- no `device.nonce` error
- WebSocket/session reaches authenticated-ready or usable chat/control-ui state
- desktop `/proxy` regression still passes
- no upstream secret/token/cookie/access URL plaintext exposure in evidence

The Browser/Manual E2E rerun must be separately approved. It is not authorized by this packet.

## Recommended Next Gate

If approved: WS Challenge Backend Build/Deploy Gate.

After that gate records a successful backend deployment: Browser/Manual E2E Approval Packet.

## Verification Results

Checks required for this approval packet:

- `git diff --check -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-backend-build-deploy-approval-packet.md`
- `rg -n "WS_CHALLENGE_BACKEND_BUILD_DEPLOY_APPROVAL_PACKET_DONE|WS_CHALLENGE_BACKEND_BUILD_DEPLOY_APPROVAL_PACKET_BLOCKED|Backend Build/Deploy Gate|Deployment/clawmanager-app|rollout|healthz|auth/me|device.nonce|invalid connect params|manual connect form|no browser E2E|no fresh instance|no passes:true|no Close" specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-backend-build-deploy-approval-packet.md`
- secret-shape scan on this new evidence, with matched values suppressed
- `git status --short -- specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-backend-build-deploy-approval-packet.md specs/gtclaw-runtime-controlui-persistent-image/evidence/20260506-ws-challenge-bridge-review.md`

## Explicit Negatives

This packet performed:

- no build/deploy
- no backend deploy/restart
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no fresh instance
- no K8S write
- no runtime mutation
- no database write
- no registry mutation
- no image build/tag/push/pull
- no manual pod patch
- no manual Service patch
- no `kubectl cp`
- no backend/frontend/runtime/deployments/docs/longterm/AgentTeam/spec/plan/tasks/existing evidence modification
- no git stage/commit/push
- no secrets/token/cookie/access URL plaintext output
- no Mem0
- no longterm write-back
- no `passes:true`
- no Close
