# CONTROLUI_CONNECTED_AND_INTERNAL_LOCALIZATION_BROWSER_E2E

Date/timezone: 2026-05-08, Asia/Shanghai
Role/task: Verifier, serial topology
Gate: CONTROLUI_CONNECTED_AND_INTERNAL_LOCALIZATION_BROWSER_E2E_GATE

## Verdict

CONTROLUI_CONNECTED_AND_INTERNAL_LOCALIZATION_BROWSER_E2E_FAILED: device identity required remained visible before connected/chat ready; post-connect / mg internal localization was not reached.

## Target

```text
approval=APPROVE_CONTROLUI_CONNECTED_AND_INTERNAL_LOCALIZATION_BROWSER_E2E_GATE
dependency_runtime_image_delivery=CONTROLUI_DEVICE_IDENTITY_REQUIRED_RUNTIME_IMAGE_DELIVERY_DONE
instance_id=18
instance_name=oc2gi-sa-151137
pod=clawmanager-user-1/clawreef-18-oc2gi-sa-151137
service=clawreef-18-oc2gi-sa-151137-svc
cluster_image=k3d-clawmanager-registry:5000/clawmanager-openclaw/openclaw:gtclaw-controlui-localization-sharedauth-20260508151137
image_index_digest=sha256:eb3e6423d73f8097ef8a5c890204d88dfa4a2af67d503b1124d8f93bd0ad38a6
```

Read-only target confirmation:

```text
pod_phase=Running
pod_ready=true
service_ports=3001,18789
```

## Browser Execution

Browser method:

```text
browser_engine=Chrome
automation_surface=DevTools Protocol
tool_fallback_reason=browser plugin Node REPL was unavailable through tool discovery
```

Opened path shape:

```text
route_path_shape=/api/v1/instances/18/control-ui/chat
route_query_shape=?session=...
target_instance_route_match=true
```

Pre-navigation status:

```text
browser_login_status_code=200
control_ui_scope_request_status_code=200
control_ui_scope_mode=control-ui
control_ui_scope_target_port=18789
credential_material_recorded=false
```

## Observed Browser State

Page state:

```text
page_title=GTClaw 控制台
document_ready_state=complete
websocket_handshake_status_code=101
visible_text_category=entrance_connection_form_zh_CN_with_error_banner
screenshot_relative_path=specs/gtclaw-runtime-controlui-persistent-image/evidence/20260508-controlui-connected-and-internal-localization-browser-e2e-post-connect.png
```

Required negative checks:

```text
device_identity_required_visible=true
device signature invalid_visible=false
```

Connected/chat ready:

```text
connected_state=false
chat_ready_state=false
chat_ready_blocker=device identity required visible
```

Post-connect / mg internal localization:

```text
post-connect_reached=false
mg 内部_reached=false
内部界面汉化=false
post-connect_blocker=Control UI did not reach connected/chat ready
```

Required success markers not claimed:

```text
required_success_marker=chat_ready_state=true not_reached
required_success_marker=内部界面汉化=true not_reached
```

## Acceptance Boundary

```text
入口连接表单汉化不等于最终验收=true
最终验收只看 post-connect / mg 内部界面汉化=true
```

The visible entrance connection form was localized, but that is not final acceptance. Final acceptance requires connected/chat ready followed by post-connect / mg 内部 GTClaw/OpenClaw management interface localization. This run did not reach that state.

## Sanitized Evidence Notes

Recorded evidence is limited to boolean, enum, path shape, visible text category, status/code, digest, and screenshot relative path. No browser storage dump, request header dump, or network header dump was recorded.

## Non-Actions

- no build/tag/push/pull image.
- no backend deploy.
- no runtime deploy.
- no frontend deploy.
- no kubectl/k3d/Helm mutation.
- no create/delete/modify instance.
- no database mutation.
- no direct SQL.
- no cleanup.
- no source modification.
- no old evidence modification.
- no old artifact modification.
- no insecure auth.
- no global bypass.
- no direct browser device-less allow.
- no Mem0 write.
- no passes:true.
- no Close.
- no longterm write-back.
- no git stage/commit/push.
