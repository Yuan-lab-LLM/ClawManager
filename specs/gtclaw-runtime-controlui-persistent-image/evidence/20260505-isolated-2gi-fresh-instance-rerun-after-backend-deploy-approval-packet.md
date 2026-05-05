# Isolated 2Gi Fresh Instance Rerun After Backend Deploy Approval Packet

Date: 2026-05-05

Worker: FreshInstanceRuntimeVerificationApprovalPacketWorker

Topology: serial

## Verdict

ISOLATED_2GI_FRESH_INSTANCE_RERUN_AFTER_BACKEND_DEPLOY_APPROVAL_PACKET_DONE

This packet requests user approval for a future rerun gate. It does not create a fresh instance, run browser E2E, build/tag/push/pull, deploy or restart backend, mutate registry/runtime/database state, set `passes:true`, Close, write longterm, or write Mem0.

## Approval Request

Please approve or reject execution of:

`Rerun Isolated 2Gi Fresh Instance After Backend Deploy Gate`

If approved, that future gate may create exactly one new isolated 2Gi OpenClaw fresh instance to prove the backend deploy fixed the runtime listener blockers.

## Dependency Gates

| Gate | Evidence | Status used |
| --- | --- | --- |
| Control Plane Backend Build/Deploy Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-control-plane-backend-build-deploy.md` | `CONTROL_PLANE_BACKEND_BUILD_DEPLOY_DONE`; running backend image `clawmanager:control-plane-backend-gtclaw-20260505183733`; `Deployment/clawmanager-app` Ready `1/1`; `/healthz` returned `200 ok` |
| Backend Runtime Listener Follow-up Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-backend-runtime-listener-followup-implementation.md` | `BACKEND_RUNTIME_LISTENER_FOLLOWUP_IMPLEMENTATION_DONE`; source fixes for Service `18789` exposure tests and OpenClaw short hostname/label behavior are implemented |
| Previous fresh rerun after cluster recovery | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-isolated-2gi-fresh-instance-rerun-after-cluster-recovery.md` | `ISOLATED_2GI_FRESH_INSTANCE_RERUN_BLOCKED`; Service exposed `3001` only, `18789` was unreachable, and OpenClaw exited after a `63 bytes` label assertion |
| Cluster Readiness Recovery Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-cluster-readiness-recovery.md` | `CLUSTER_READINESS_RECOVERY_DONE`; cluster readiness was restored to `1/1` |
| Runtime Startup Artifact Implementation Gate | `specs/gtclaw-runtime-controlui-persistent-image/evidence/20260505-runtime-startup-artifact-implementation.md` | `RUNTIME_STARTUP_ARTIFACT_IMPLEMENTATION_DONE`; startup artifact hashes are available for future verification |

## Current State Summary

- The control-plane backend deploy gate is complete and the running backend image is `clawmanager:control-plane-backend-gtclaw-20260505183733`.
- The backend deploy included the A1 Service `18789` source changes, WS Auth Bridge source changes, and OpenClaw short runtime hostname/label fix.
- The previous fresh instance blocker was observed before this backend deploy: Service exposed `3001` but not `18789`, `18789` was unreachable on PodIP and ServiceIP, and the OpenClaw log showed a generated label longer than `63 bytes`.
- The expected runtime image digest remains `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`.

## Future Gate Allowed Mutation

If the user approves the future gate, the allowed mutation is limited to:

- Create one new isolated 2Gi fresh instance only.
- Use only the approved runtime image tag:
  `k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-persistent-bind-lan-auth-20260505162033`
- Use the approved runtime image digest:
  `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`
- Allow Kubernetes/runtime/database side effects only as the normal consequence of the ClawManager API creating that one new fresh instance.

No other mutation is approved by this packet.

## Future Gate Required Verification

The future gate must verify and record:

- cluster readiness remains `1/1`;
- backend deployment is still Ready and still using `clawmanager:control-plane-backend-gtclaw-20260505183733`;
- the new isolated 2Gi fresh instance pod reaches Ready;
- new pod restart count is `0`;
- no OOM event or OOMKilled container state is present;
- runtime `imageID` matches approved digest `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`;
- the new Service exposes both `3001` and `18789`;
- `18789` is reachable on PodIP and ServiceIP;
- `3001` remains reachable;
- runtime startup artifact hashes match the reviewed artifact, including `defaults/openclaw-agent/config.yaml` sha256 `347af8dcfa73cb0938f00413d28d0fb4a3c409916d794aaf43e47e9a1fafe30e` and `etc/services.d/openclaw-agent/run` sha256 `53d33bd3d3f66be2b9e67346dcd6f45115439c9816a2397d6f55696b3fb9ddda`;
- OpenClaw logs show no hostname or label assertion related to `63 bytes`;
- no additional fresh instance beyond the one approved fresh instance is created.

## Future Gate Forbidden Actions

The future gate remains forbidden from:

- browser E2E;
- Chrome DevTools MCP;
- Playwright;
- build/tag/push/pull;
- backend deploy;
- backend restart;
- runtime image rebuild;
- frontend rebuild or deploy;
- registry mutation;
- database migration;
- manual pod patch;
- manual Service patch;
- `kubectl cp`;
- cleanup/delete instance 9;
- cluster/server/serverlb recovery;
- k3d cluster create/delete/start/stop;
- `passes:true`;
- Close;
- longterm write-back;
- Mem0 write.

## Stop Conditions For Future Gate

The future gate should stop and write a BLOCKED evidence file if:

- cluster readiness is not `1/1`;
- `Deployment/clawmanager-app` is not Ready or is not using `clawmanager:control-plane-backend-gtclaw-20260505183733`;
- the approved runtime image tag or digest cannot be verified before creation;
- creating the fresh instance would require build/tag/push/pull, backend deploy/restart, frontend rebuild, runtime image rebuild, database migration, registry mutation, manual pod patch, or manual Service patch;
- more than one new fresh instance would be required.

## Future Gate Success Criteria

The future gate can report DONE only if the new isolated 2Gi fresh instance proves:

- pod Ready with restart count `0` and no OOM;
- `imageID` matches `sha256:db1cdb2a48fc74440fde12fa0c89bb808bf35bb09c38d0e90cc669d89f78c1f9`;
- Service exposes `3001` and `18789`;
- `18789` is reachable on PodIP and ServiceIP;
- `3001` remains reachable;
- startup artifact hashes match the reviewed runtime startup artifact;
- OpenClaw log has no `63 bytes` hostname/label assertion.

## Follow-up Gate Order

1. If approved: `Rerun Isolated 2Gi Fresh Instance After Backend Deploy Gate`.
2. Listener/hash verification if not fully covered by that fresh rerun gate.
3. Browser E2E approval/gate.
4. Only after fresh E2E plus explicit user approval: `passes:true`, Close, and longterm write-back.

## Recommendation

Approve `Rerun Isolated 2Gi Fresh Instance After Backend Deploy Gate` only if the user is ready to allow creation of exactly one new isolated 2Gi fresh instance using the approved runtime image tag and digest above.

## Explicit Negatives For This Packet

- no fresh instance
- no browser E2E
- no Chrome DevTools MCP
- no Playwright
- no build/tag/push/pull
- no backend deploy
- no backend restart
- no runtime image rebuild
- no frontend rebuild
- no database migration
- no registry mutation
- no K8S write
- no runtime mutation
- no database mutation
- no manual pod patch
- no manual Service patch
- no kubectl cp
- no cleanup/delete instance 9
- no longterm write-back
- no Mem0 write
- no passes:true
- no Close
- no token value, cookie value, credential, secret, or access URL plaintext
