# Control UI Device Identity Diagnostic Capacity Recovery And Live Vector Resume Approval Packet

packet_verdict: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_APPROVAL_PACKET_DONE
date: 2026-05-08
task_type: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_APPROVAL_PACKET_GATE

## Dependency State

- dependency_gate: CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_VERIFIER_BLOCKER_UNBLOCK_AND_LIVE_VECTOR_FAILED
- runtime diagnostic image delivered: k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
- runtime diagnostic image index digest: sha256:e520d5ce5359cf2b532043a099ba8e637c6b8611c8e7fd81fd659ff5de755afe
- runtime diagnostic image linux_arm64 manifest digest: sha256:0942d003c9d4339431823829c97412da243910386d8c6bb50050c41d0f888c06
- backend diagnostic image rollout delivered: k3d-clawmanager-registry:5000/clawmanager/clawmanager:gtclaw-diagnostic-backend-20260508174053
- backend diagnostic image index digest: sha256:63084136990d16d1536006260bfe09a910231aef69d5102948d186bc80a3369b
- backend diagnostic env delivered: CONTROLUI_PROXY_AUTH_DIAGNOSTICS=1
- verifier blocker status: unblocked
- packaged verifier status: passed in image build and diagnostic readback

## Current Blocker

- failed diagnostic instance: 19 / oc2gi-diag-175648
- failed diagnostic pod: clawmanager-user-1/clawreef-19-oc2gi-diag-175648
- pod phase: Pending
- pod scheduled: false
- scheduler reason: Unschedulable
- scheduler message shape: Insufficient memory
- endpointslice endpoints: none
- live vector status: not collected
- browser/manual E2E status: not run
- runtime diagnostic logs: not available because the pod is not scheduled

## Read-Only Capacity Diagnostics

- node_count: 1
- node_ready: true
- node_memory_pressure: false
- node_allocatable_memory_shape: about 7.65Gi
- running OpenClaw diagnostic-relevant pods before recovery request: 3
- running OpenClaw memory requests: 2Gi each
- pending diagnostic pod memory request: 2Gi
- node_allocated_memory_requests_shape: about 80 percent before scheduling instance 19
- metrics_available: true
- kubectl_top_node_memory_shape: about 71 percent in use
- scheduler_decision: failed on requested memory, not current measured memory
- read_only_sources: kubectl get/describe nodes, kubectl get/describe pods, kubectl get events, kubectl get endpointslices, kubectl top nodes/pods, existing evidence
- pod describe environment values: not recorded

## Capacity Recovery Options By Risk

### Option 1 - Preferred: Minimize Failed Diagnostic Instance 19, Then Resume Exactly One Fresh Diagnostic Instance

- requested mutation class: delete or stop only instance 19 / oc2gi-diag-175648 and its failed Pending pod/PVC as explicitly approved capacity recovery for this failed diagnostic attempt
- reason: instance 19 has no runtime logs, no browser session, no live vector, and no scheduled workload; removing only this failed diagnostic attempt frees its requested 2Gi scheduling reservation path without touching older runtime evidence/assets
- replacement action: create exactly one replacement fresh diagnostic runtime instance using k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
- expected live vector gate shape: run one browser/manual E2E and collect sanitized backend/runtime/browser live vector
- old evidence/assets cleanup: not allowed; prior evidence files and old runtime images remain untouched
- rollback boundary: if deletion/stop of instance 19 fails or creates unexpected resource impact, stop before replacement create and record BLOCKED with status-only evidence

### Option 2 - Moderate Risk: Temporarily Stop One Explicitly Approved Older Running Runtime Instance, Then Resume

- requested mutation class: stop only one explicitly approved older OpenClaw instance if deleting/stopping instance 19 alone is not enough
- reason: current node has three running OpenClaw pods with 2Gi requests; freeing one running 2Gi request gives the replacement diagnostic pod room to schedule
- required approval detail: identify the exact instance id/name to stop before any action
- rollback boundary: start the stopped instance back only if explicitly approved in the same next gate or in a follow-up gate
- evidence/assets cleanup: not allowed; stopping an instance is not approval to delete old evidence/assets/session artifacts

### Option 3 - Higher Risk: Expand K3D Node Capacity

- requested mutation class: explicit K3D or Docker Desktop resource expansion outside the app workload
- reason: avoids deleting or stopping existing instances, but changes cluster capacity and may require node or cluster-level operations
- concrete K8S mutation: none inside workload manifests unless separately approved
- external mutation shape: increase local Docker/K3D memory capacity, then wait for node allocatable memory to update
- rollback boundary: restore prior local capacity settings only after explicit approval; do not modify deployments/docs/longterm in this gate

### Option 4 - Higher Risk: Adjust Runtime Resource Request For Replacement Diagnostic Instance

- requested mutation class: create replacement diagnostic instance with a smaller memory request only if the product/API supports this as normal create-instance input and the Commander approves the exact value
- reason: may schedule without stopping older instances, but changes diagnostic representativeness from the prior 2Gi path
- concrete K8S mutation: backend-created Pod with lower memory request for the replacement instance only
- rollback boundary: do not alter global runtime image settings, deployments, runtime image, or existing running pods; if the lower-request pod fails live vector collection, record FAILED with sanitized vector

## Requested Approval Phrase For Next Gate

APPROVE_CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_GATE

## Draft Next Gate Allowed Actions

- handle only failed diagnostic instance 19 / oc2gi-diag-175648 or the explicitly approved capacity action
- create exactly one replacement fresh diagnostic runtime instance
- use runtime diagnostic image k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-diagnostic-runtime-20260508175648
- keep backend diagnostic image/env as already rolled out unless the next approval explicitly says otherwise
- run one browser/manual E2E attempt after the replacement runtime is Ready
- collect sanitized backend/runtime/browser live vector only
- record only boolean, enum, path shape, status/code/error code, digest, and sanitized condition names
- if capacity recovery cannot be completed safely, return CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_BLOCKED with reason
- if capacity recovery works but live vector still fails, return CONTROLUI_DEVICE_IDENTITY_DIAGNOSTIC_CAPACITY_RECOVERY_AND_LIVE_VECTOR_RESUME_FAILED with sanitized live vector

## Draft Next Gate Forbidden Items

- no insecure auth
- no global bypass
- no direct browser device-less allow
- no broad origin workaround
- no trustedProxy JSON marker trust
- no secret/header/access URL logging
- no old asset cleanup
- no old session cleanup
- no old evidence cleanup
- no unapproved instance cleanup
- no deployment manifest edit
- no runtime image rebuild
- no backend image rebuild
- no database mutation
- no direct SQL
- no passes:true
- no Close
- no longterm write-back
- no Mem0 write
- no git stage/commit/push

## Current Gate Non Actions

- no instance delete
- no instance stop
- no instance create
- no pod delete
- no PVC delete
- no session cleanup
- no asset cleanup
- no rollout
- no patch
- no scale
- no image build/tag/push/pull
- no browser/manual E2E
- no Playwright
- no DevTools
- no database mutation
- no code change
- no frontend change
- no deployments change
- no docs change
- no longterm change
- no AgentTeam change
- no UnifiedFramework change
- no Mem0 write
- no passes:true
- no Close
- no git stage/commit/push
