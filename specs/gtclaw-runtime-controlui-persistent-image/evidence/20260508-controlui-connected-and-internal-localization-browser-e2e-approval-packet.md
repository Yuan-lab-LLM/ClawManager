# Control UI Connected And Internal Localization Browser E2E Approval Packet

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Worker, serial topology
Gate: CONTROLUI_CONNECTED_AND_INTERNAL_LOCALIZATION_BROWSER_E2E_APPROVAL_PACKET_GATE

## Verdict

CONTROLUI_CONNECTED_AND_INTERNAL_LOCALIZATION_BROWSER_E2E_APPROVAL_PACKET_DONE

This packet requests approval for the next browser/manual E2E gate only. It did not run browser E2E, manual E2E, DevTools, Playwright, image build, deploy, Kubernetes mutation, instance mutation, cleanup, write-back, Close, passes:true, git stage/commit/push, or Mem0 write.

## Requested Approval Token

APPROVE_CONTROLUI_CONNECTED_AND_INTERNAL_LOCALIZATION_BROWSER_E2E_GATE

## Dependency State

```text
runtime_image_delivery_gate=CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_DONE
commander_readonly_review=passed
fresh_runtime_instance=18 / oc2gi-sa-151137
pod=clawmanager-user-1/clawreef-18-oc2gi-sa-151137
service=clawreef-18-oc2gi-sa-151137-svc
cluster_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-sharedauth-20260508151137
image_index_digest=sha256:eb3e6423d73f8097ef8a5c890204d88dfa4a2af67d503b1124d8f93bd0ad38a6
```

Readiness/readback dependency summary:

```text
pod_running=true
pod_ready=true
restart_count=0
oom_killed=false
service_3001_present=true
service_18789_present=true
runtime_18789_status_code=200
running_container_verifier_exit=0
zh_CN_hash_match=true
```

## Proposed Next Gate

CONTROLUI_CONNECTED_AND_INTERNAL_LOCALIZATION_BROWSER_E2E_GATE

Proposed scope after approval:

1. Use browser/manual E2E to open path shape `/api/v1/instances/18/control-ui/chat?session=main`.
2. Verify the user-visible flow no longer shows `device identity required`.
3. Verify the user-visible flow no longer shows `device signature invalid`.
4. Verify Control UI reaches a connected / chat ready state.
5. After connected / chat ready, enter the post-connect mg 内部 surface and verify GTClaw/OpenClaw management content internal localization.
6. Clearly distinguish entrance-form localization from final acceptance: 入口连接表单汉化不等于最终验收; post-connect / mg 内部界面汉化才是本轮核心验收.

## Proposed Non-Scope

- no build/tag/push/pull image.
- no backend deploy.
- no runtime deploy.
- no kubectl/k3d/Helm mutation.
- no create/delete/modify instance.
- no cleanup.
- no storage/cache cleanup.
- no source or artifact modification.
- no old evidence modification.
- no docs or longterm write-back.
- no passes:true.
- no Close.
- no git stage/commit/push.
- no Mem0 write.

## Browser Evidence Rules For Next Gate

Allowed sanitized browser evidence:

- boolean.
- enum.
- path shape.
- visible text category.
- status/code.
- screenshot path, if screenshot evidence is captured.

Forbidden browser evidence:

- plaintext values for TOKEN, PASSWORD, KEY, COOKIE, BEARER, AUTH HEADER, or ACCESS URL.
- full credential-bearing route values.
- browser storage dump.
- network request header dump.
- session material.

The next gate may record that the route shape starts with `/api/v1/instances/18/control-ui`, but it must not record credential-bearing browser state.

## Acceptance Boundary For Next Gate

Required positive observations:

```text
target_instance_id=18
target_instance_name=oc2gi-sa-151137
route_shape=/api/v1/instances/18/control-ui/chat?session=main
device_identity_required_visible=false
device_signature_invalid_visible=false
connected_state=true
chat_ready_state=true
post-connect_surface=mg 内部
内部界面汉化=true
```

Important distinction:

```text
入口连接表单汉化不等于最终验收=true
post-connect_mg_internal_localization_required=true
```

The next gate must not claim success from the entrance connection form alone. Final acceptance requires post-connect / mg 内部 GTClaw/OpenClaw management interface localization after Control UI reaches connected / chat ready.

## Failure Conditions For Next Gate

The next gate should return blocked or failed evidence if any of these are observed:

- `device identity required` is visible.
- `device signature invalid` is visible.
- Control UI does not reach connected / chat ready.
- The route points to any instance other than `18 / oc2gi-sa-151137`.
- Only entrance connection form localization is visible and post-connect mg 内部 localization is not reached.
- Evidence capture would require recording forbidden credential-bearing browser state.

## This Gate Non-Actions

- no browser E2E.
- no manual E2E.
- no DevTools.
- no Playwright.
- no image build/tag/push/pull.
- no backend deploy.
- no runtime deploy.
- no kubectl/k3d/Helm mutation.
- no instance mutation.
- no cleanup.
- no storage/cache cleanup.
- no source modification.
- no artifact modification.
- no old evidence modification.
- no Mem0 write.
- no passes:true.
- no Close.
- no longterm write-back.
- no git stage/commit/push.
